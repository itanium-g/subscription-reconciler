package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/example/subscription-reconciler/internal/application"
	"github.com/example/subscription-reconciler/internal/domain"
	"github.com/example/subscription-reconciler/internal/infrastructure/postgres"
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
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	w.logger.InfoContext(ctx, "carrier polling worker started", "interval", "5 minutes")

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

	// Poll up to 100 users per cycle
	processed, err := w.service.PollCarriersForUsers(ctx, 100)
	if err != nil {
		w.logger.ErrorContext(ctx, "carrier polling error", "err", err, "duration", time.Since(startTime))
		return
	}

	w.logger.InfoContext(ctx, "carrier polling cycle completed", "processed", processed, "duration", time.Since(startTime))
}
