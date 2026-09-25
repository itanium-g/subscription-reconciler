package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/example/subscription-reconciler/internal/application"
	"github.com/example/subscription-reconciler/internal/domain"
	"github.com/example/subscription-reconciler/internal/infrastructure/postgres"
)

const (
	carrierPollingInterval        = 5 * time.Minute
	carrierPollingStaleness       = 5 * time.Minute
	carrierPollingBatchSize int32 = 100
)

// PollingWorker runs background polling jobs.
type PollingWorker struct {
	service application.CarrierPollingService
	logger  *slog.Logger
}

// NewPollingWorker creates a new polling worker.
func NewPollingWorker(db postgres.Database, client domain.CarrierClient, logger *slog.Logger) *PollingWorker {
	service := application.NewCarrierPollingService(db, client)
	return &PollingWorker{
		service: service,
		logger:  logger,
	}
}

// StartCarrierPolling starts the carrier polling job (every 5 minutes).
func (w *PollingWorker) StartCarrierPolling(ctx context.Context) {
	ticker := time.NewTicker(carrierPollingInterval)
	defer ticker.Stop()

	w.logger.InfoContext(ctx, "carrier polling worker started", "interval", carrierPollingInterval.String(), "staleness", carrierPollingStaleness.String(), "batch_size", carrierPollingBatchSize)

	// Run immediately on startup
	w.pollOnce(ctx)

	// Then run on schedule
	for {
		select {
		case <-ctx.Done():
			w.logger.InfoContext(ctx, "carrier polling worker stopped")
			return
		case <-ticker.C:
			w.pollOnce(ctx)
		}
	}
}

// pollOnce runs a single polling cycle.
func (w *PollingWorker) pollOnce(ctx context.Context) {
	startTime := time.Now()
	var processed int32

	for {
		if err := ctx.Err(); err != nil {
			return
		}

		batchProcessed, err := w.service.PollCarriersForUsers(ctx, carrierPollingBatchSize, time.Now().Add(-carrierPollingStaleness))
		if err != nil {
			if ctx.Err() == nil {
				w.logger.ErrorContext(ctx, "carrier polling error", "err", err, "processed", processed, "duration", time.Since(startTime))
			}
			return
		}

		processed += batchProcessed
		if batchProcessed < carrierPollingBatchSize {
			break
		}
	}

	w.logger.InfoContext(ctx, "carrier polling cycle completed", "processed", processed, "duration", time.Since(startTime))
}
