package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/example/adora/internal/application"
	"github.com/example/adora/internal/infrastructure/postgres"
)

// NotificationWorker runs background notification jobs.
type NotificationWorker struct {
	service application.NotificationService
	logger  *slog.Logger
}

// NewNotificationWorker creates a new notification worker.
func NewNotificationWorker(db postgres.Database, logger *slog.Logger) *NotificationWorker {
	service := application.NewNotificationService(db)
	return &NotificationWorker{
		service: service,
		logger:  logger,
	}
}

// StartNotificationSending starts the notification sender job (every 1 minute).
func (w *NotificationWorker) StartNotificationSending(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	w.logger.InfoContext(ctx, "notification sender worker started", "interval", "1 minute")

	// Run immediately on startup
	w.sendOnce(ctx)

	// Then run on schedule
	for {
		select {
		case <-ctx.Done():
			w.logger.InfoContext(ctx, "notification sender worker stopped")
			return
		case <-ticker.C:
			w.sendOnce(ctx)
		}
	}
}

// sendOnce runs a single notification sending cycle.
func (w *NotificationWorker) sendOnce(ctx context.Context) {
	startTime := time.Now()

	// Send due notifications
	sent, err := w.service.SendDueNotifications(ctx)
	if err != nil {
		w.logger.ErrorContext(ctx, "notification sending error", "err", err, "duration", time.Since(startTime))
		return
	}

	if sent > 0 {
		w.logger.InfoContext(ctx, "notifications sent", "count", sent, "duration", time.Since(startTime))
	}
}
