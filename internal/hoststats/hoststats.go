// Package hoststats samples host-level CPU/RAM/disk usage for the admin panel.
package hoststats

import (
	"context"
	"sync"
	"time"
)

// Snapshot is the last successfully sampled host-resource reading. Ready is
// false until the first sample completes, so callers can distinguish "0% CPU"
// from "no data yet".
type Snapshot struct {
	CPUPercent     float64
	MemUsedBytes   int64
	MemTotalBytes  int64
	DiskFreeBytes  int64
	DiskTotalBytes int64
	Ready          bool
}

// Poller periodically samples host stats and caches the last snapshot for cheap
// HTTP reads.
type Poller struct {
	diskPath string

	mu   sync.RWMutex
	snap Snapshot

	cpu cpuSampler
}

// NewPoller creates a poller that reports free/total disk space for the
// filesystem containing diskPath.
func NewPoller(diskPath string) *Poller {
	if diskPath == "" {
		diskPath = "."
	}
	return &Poller{diskPath: diskPath}
}

// Snapshot returns the last sample. Safe to call concurrently with Run.
func (p *Poller) Snapshot() Snapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.snap
}

// Run samples immediately, then on every tick of interval, until ctx is
// canceled. CPU usage is a delta between successive samples, so the first sample
// after startup reports 0% while memory/disk are already meaningful.
func (p *Poller) Run(ctx context.Context, interval time.Duration) {
	p.sampleOnce()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.sampleOnce()
		}
	}
}

func (p *Poller) sampleOnce() {
	var snap Snapshot
	snap.CPUPercent = p.cpu.sample()
	if used, total, err := memStats(); err == nil {
		snap.MemUsedBytes, snap.MemTotalBytes = used, total
	}
	if free, total, err := diskFreeBytes(p.diskPath); err == nil {
		snap.DiskFreeBytes, snap.DiskTotalBytes = free, total
	}
	snap.Ready = true

	p.mu.Lock()
	p.snap = snap
	p.mu.Unlock()
}
