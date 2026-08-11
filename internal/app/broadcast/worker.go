package broadcast

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"go.uber.org/zap"
)

type WorkerConfig struct {
	Interval         time.Duration
	Lease            time.Duration
	MaterializeBatch int
	DeliveryBatch    int
}

type Worker struct {
	service *Service
	config  WorkerConfig
	log     *zap.Logger
}

func NewWorker(service *Service, config WorkerConfig, log *zap.Logger) *Worker {
	if config.Interval <= 0 {
		config.Interval = 3 * time.Second
	}
	if config.Lease <= 0 {
		config.Lease = 30 * time.Second
	}
	if config.MaterializeBatch <= 0 || config.MaterializeBatch > 1000 {
		config.MaterializeBatch = 200
	}
	if config.DeliveryBatch <= 0 || config.DeliveryBatch > 500 {
		config.DeliveryBatch = 50
	}
	if log == nil {
		log = zap.NewNop()
	}
	return &Worker{service: service, config: config, log: log}
}

func (w *Worker) Run(ctx context.Context) {
	if w == nil || w.service == nil || !w.service.Ready() {
		return
	}
	w.runOnce(ctx)
	ticker := time.NewTicker(w.config.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.runOnce(ctx)
		}
	}
}

func (w *Worker) runOnce(ctx context.Context) {
	var tokenBytes [16]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		w.log.Error("generate broadcast lease token", zap.Error(err))
		return
	}
	result, err := w.service.RunCycle(ctx, hex.EncodeToString(tokenBytes[:]), w.config.MaterializeBatch, w.config.DeliveryBatch, w.config.Lease)
	if err != nil {
		if ctx.Err() == nil {
			w.log.Warn("broadcast delivery cycle failed", zap.Error(err))
		}
		return
	}
	if result.Materialized > 0 || result.Claimed > 0 {
		w.log.Info("broadcast delivery cycle completed",
			zap.Int("materialized", result.Materialized),
			zap.Int("claimed", result.Claimed),
			zap.Int("sent", result.Sent),
			zap.Int("failed", result.Failed))
	}
}
