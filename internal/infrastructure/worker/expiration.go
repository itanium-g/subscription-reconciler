package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/example/subscription-reconciler/internal/application"
	"github.com/example/subscription-reconciler/internal/infrastructure/postgres"
)

const (
	expirationReconciliationInterval       = time.Minute
	expirationBatchSize              int32 = 1000
)

// ExpirationWorker runs periodic entitlement expiration reconciliation.
type ExpirationWorker struct {
	service application.ExpirationService
	logger  *slog.Logger
}

// NewExpirationWorker creates a new expiration reconciliation worker.
func NewExpirationWorker(db postgres.Database, logger *slog.Logger) *ExpirationWorker {
	return &ExpirationWorker{
		service: application.NewExpirationService(db),
		logger:  logger,
	}
}

// StartExpirationReconciliation starts the expiration sweep. It runs once on
// startup and then once per minute until the supplied context is cancelled.
func (w *ExpirationWorker) StartExpirationReconciliation(ctx context.Context) {
	ticker := time.NewTicker(expirationReconciliationInterval)
	defer ticker.Stop()

	w.logger.InfoContext(
		ctx,
		"expiration reconciliation worker started",
		"interval",
		expirationReconciliationInterval.String(),
		"batch_size",
		expirationBatchSize,
	)

	// Reconcile immediately so an existing backlog does not wait for the
	// first scheduled tick.
	w.reconcileOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			w.logger.InfoContext(ctx, "expiration reconciliation worker stopped")
			return
		case <-ticker.C:
			w.reconcileOnce(ctx)
		}
	}
}

func (w *ExpirationWorker) reconcileOnce(ctx context.Context) {
	startTime := time.Now()
	var processed int32

	for {
		if err := ctx.Err(); err != nil {
			return
		}

		batchProcessed, err := w.service.ReconcileExpiredEntitlements(ctx, expirationBatchSize)
		if err != nil {
			if ctx.Err() == nil {
				w.logger.ErrorContext(ctx, "expiration reconciliation error", "err", err, "duration", time.Since(startTime))
			}
			return
		}

		processed += batchProcessed
		if batchProcessed < expirationBatchSize {
			break
		}
	}

	if processed > 0 {
		w.logger.InfoContext(
			ctx,
			"expiration reconciliation cycle completed",
			"processed",
			processed,
			"duration",
			time.Since(startTime),
		)
	}
}
