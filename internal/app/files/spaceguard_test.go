package files

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"telesrv/internal/domain"
)

type staticUniqueBlobBytesStore struct {
	used int64
	err  error
}

func (s staticUniqueBlobBytesStore) UniqueFileBlobBytes(context.Context, domain.MediaBackend) (int64, error) {
	return s.used, s.err
}

func TestS3BudgetSpaceGuardRequiresRefreshAndCountsAdditionalBytes(t *testing.T) {
	guard := NewS3BudgetSpaceGuard(staticUniqueBlobBytesStore{used: 90}, 100)
	if ok, err := guard.Allow(1); err == nil || ok {
		t.Fatalf("uninitialized Allow = %v, %v; want fail closed", ok, err)
	}
	if err := guard.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if ok, err := guard.Allow(10); err != nil || !ok {
		t.Fatalf("Allow at boundary = %v, %v", ok, err)
	}
	if ok, err := guard.Allow(11); err != nil || ok {
		t.Fatalf("Allow over boundary = %v, %v", ok, err)
	}
	if used, total, ok := guard.Usage(); !ok || used != 90 || total != 100 {
		t.Fatalf("Usage = %d/%d ok=%v", used, total, ok)
	}
}

func TestS3BudgetSpaceGuardRefreshErrorKeepsLastSnapshot(t *testing.T) {
	store := &staticUniqueBlobBytesStore{used: 20}
	guard := NewS3BudgetSpaceGuard(store, 100)
	if err := guard.Refresh(context.Background()); err != nil {
		t.Fatalf("initial Refresh: %v", err)
	}
	store.err = errors.New("database unavailable")
	if err := guard.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh accepted store error")
	}
	if ok, err := guard.Allow(80); err != nil || !ok {
		t.Fatalf("last valid snapshot was not retained: ok=%v err=%v", ok, err)
	}
}

func TestGuardedBlobBackendRejectsStreamingWriteBeforePublish(t *testing.T) {
	root := t.TempDir()
	local, err := NewLocalFS(root)
	if err != nil {
		t.Fatalf("NewLocalFS: %v", err)
	}
	guard := NewS3BudgetSpaceGuard(staticUniqueBlobBytesStore{used: 95}, 100)
	if err := guard.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	backend := NewGuardedBlobBackend(local, guard)
	key, _, _, err := backend.PutReader(context.Background(), bytes.NewReader([]byte("123456")))
	if !errors.Is(err, domain.ErrStorageFull) {
		t.Fatalf("PutReader err = %v, want ErrStorageFull", err)
	}
	if key != "" {
		t.Fatalf("rejected PutReader published key %q", key)
	}
}

func TestLocalDiskSpaceGuardRefreshesRealFilesystem(t *testing.T) {
	guard := NewLocalDiskSpaceGuard(t.TempDir(), 1)
	if ok, err := guard.Allow(1); err == nil || ok {
		t.Fatalf("uninitialized Allow = %v, %v; want fail closed", ok, err)
	}
	if err := guard.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	used, total, ok := guard.Usage()
	if !ok || total <= 0 || used < 0 || used > total {
		t.Fatalf("Usage = %d/%d ok=%v", used, total, ok)
	}
}
