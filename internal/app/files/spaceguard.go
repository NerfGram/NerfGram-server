package files

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"telesrv/internal/domain"

	"go.uber.org/zap"
)

// SpaceGuard provides a cached, read-only capacity decision for a storage
// target. Refresh is deliberately explicit: startup performs one successful
// refresh before any writer is exposed, and the worker only updates that
// already-valid snapshot afterwards.
type SpaceGuard interface {
	Allow(additional int64) (bool, error)
	Usage() (used, total int64, ok bool)
	Refresh(context.Context) error
}

type NoopSpaceGuard struct{}

func (NoopSpaceGuard) Allow(int64) (bool, error)     { return true, nil }
func (NoopSpaceGuard) Usage() (int64, int64, bool)   { return 0, 0, false }
func (NoopSpaceGuard) Refresh(context.Context) error { return nil }

// LocalDiskSpaceGuard protects the filesystem containing root. free is the
// amount available to the current process, not free blocks reserved for root.
type LocalDiskSpaceGuard struct {
	root         string
	minFreeBytes int64
	free         atomic.Int64
	total        atomic.Int64
	ready        atomic.Bool
}

func NewLocalDiskSpaceGuard(root string, minFreeBytes int64) *LocalDiskSpaceGuard {
	return &LocalDiskSpaceGuard{root: root, minFreeBytes: minFreeBytes}
}

func (g *LocalDiskSpaceGuard) Allow(additional int64) (bool, error) {
	if additional < 0 {
		return false, fmt.Errorf("additional storage bytes must be non-negative")
	}
	if g.minFreeBytes <= 0 {
		return true, nil
	}
	if !g.ready.Load() {
		return false, fmt.Errorf("local storage capacity snapshot is not ready")
	}
	return additional <= g.free.Load()-g.minFreeBytes, nil
}

func (g *LocalDiskSpaceGuard) Usage() (used, total int64, ok bool) {
	if !g.ready.Load() {
		return 0, 0, false
	}
	total = g.total.Load()
	used = total - g.free.Load()
	if used < 0 {
		used = 0
	}
	return used, total, true
}

func (g *LocalDiskSpaceGuard) Refresh(context.Context) error {
	free, total, err := localDiskFreeBytes(g.root)
	if err != nil {
		return fmt.Errorf("read free space for %q: %w", g.root, err)
	}
	if free < 0 || total <= 0 || free > total {
		return fmt.Errorf("invalid free-space snapshot for %q: free=%d total=%d", g.root, free, total)
	}
	g.free.Store(free)
	g.total.Store(total)
	g.ready.Store(true)
	return nil
}

// UniqueBlobBytesStore returns physical bytes tracked for one permanent
// backend, counting a content-addressed object key exactly once.
type UniqueBlobBytesStore interface {
	UniqueFileBlobBytes(context.Context, domain.MediaBackend) (int64, error)
}

type S3BudgetSpaceGuard struct {
	store         UniqueBlobBytesStore
	backend       domain.MediaBackend
	maxTotalBytes int64
	used          atomic.Int64
	ready         atomic.Bool
}

func NewS3BudgetSpaceGuard(store UniqueBlobBytesStore, maxTotalBytes int64) *S3BudgetSpaceGuard {
	return &S3BudgetSpaceGuard{store: store, backend: domain.MediaBackendS3, maxTotalBytes: maxTotalBytes}
}

func (g *S3BudgetSpaceGuard) Allow(additional int64) (bool, error) {
	if additional < 0 {
		return false, fmt.Errorf("additional storage bytes must be non-negative")
	}
	if g.maxTotalBytes <= 0 {
		return true, nil
	}
	if !g.ready.Load() {
		return false, fmt.Errorf("s3 capacity snapshot is not ready")
	}
	used := g.used.Load()
	return used <= g.maxTotalBytes && additional <= g.maxTotalBytes-used, nil
}

func (g *S3BudgetSpaceGuard) Usage() (used, total int64, ok bool) {
	if !g.ready.Load() {
		return 0, 0, false
	}
	return g.used.Load(), g.maxTotalBytes, true
}

func (g *S3BudgetSpaceGuard) Refresh(ctx context.Context) error {
	if g.store == nil {
		return fmt.Errorf("s3 capacity store is nil")
	}
	used, err := g.store.UniqueFileBlobBytes(ctx, g.backend)
	if err != nil {
		return fmt.Errorf("read unique s3 blob bytes: %w", err)
	}
	if used < 0 {
		return fmt.Errorf("invalid unique s3 blob bytes: %d", used)
	}
	g.used.Store(used)
	g.ready.Store(true)
	return nil
}

// MultiSpaceGuard requires every target to have capacity. S3 writes use this
// for both the local spool filesystem and the permanent S3 budget.
type MultiSpaceGuard []SpaceGuard

func (g MultiSpaceGuard) Allow(additional int64) (bool, error) {
	for _, item := range g {
		if item == nil {
			continue
		}
		ok, err := item.Allow(additional)
		if err != nil || !ok {
			return ok, err
		}
	}
	return true, nil
}

func (g MultiSpaceGuard) Usage() (int64, int64, bool)   { return 0, 0, false }
func (g MultiSpaceGuard) Refresh(context.Context) error { return nil }

// DiskUsageWorker refreshes a previously initialized capacity snapshot. A
// failed periodic refresh keeps the last valid snapshot and is observable.
type DiskUsageWorker struct {
	guard    SpaceGuard
	interval time.Duration
	log      *zap.Logger
}

func NewDiskUsageWorker(guard SpaceGuard, interval time.Duration, log *zap.Logger) *DiskUsageWorker {
	if interval <= 0 {
		interval = time.Minute
	}
	if log == nil {
		log = zap.NewNop()
	}
	return &DiskUsageWorker{guard: guard, interval: interval, log: log}
}

func (w *DiskUsageWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.guard.Refresh(ctx); err != nil && ctx.Err() == nil {
				w.log.Warn("refresh storage capacity snapshot failed", zap.Error(err))
			}
		}
	}
}

func requireSpace(guard SpaceGuard, additional int64) error {
	if guard == nil {
		return nil
	}
	ok, err := guard.Allow(additional)
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrStorageFull
	}
	return nil
}

// capacityReader stops a streaming permanent write before the backend can
// publish an object larger than the current capacity snapshot permits.
type capacityReader struct {
	src   io.Reader
	guard SpaceGuard
	total int64
}

func (r *capacityReader) Read(p []byte) (int, error) {
	n, err := r.src.Read(p)
	if n > 0 {
		if guardErr := requireSpace(r.guard, r.total+int64(n)); guardErr != nil {
			return 0, guardErr
		}
		r.total += int64(n)
	}
	return n, err
}
