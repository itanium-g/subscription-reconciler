package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/example/subscription-reconciler/internal/application"
	"github.com/example/subscription-reconciler/internal/infrastructure/postgres"
)

const (
	notificationSendingInterval       = time.Minute
	notificationBatchSize       int32 = 1000
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
	ticker := time.NewTicker(notificationSendingInterval)
	defer ticker.Stop()

	w.logger.InfoContext(ctx, "notification sender worker started", "interval", notificationSendingInterval.String(), "batch_size", notificationBatchSize)

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

// sendOnce drains the due notification backlog in bounded batches.
func (w *NotificationWorker) sendOnce(ctx context.Context) {
	startTime := time.Now()
	var claimed int32

	for {
		if err := ctx.Err(); err != nil {
			return
		}

		batchClaimed, err := w.service.SendDueNotifications(ctx, notificationBatchSize)
		if err != nil {
			if ctx.Err() == nil {
				w.logger.ErrorContext(ctx, "notification sending error", "err", err, "count", claimed, "duration", time.Since(startTime))
			}
			return
		}

		claimed += batchClaimed
		if batchClaimed < notificationBatchSize {
			break
		}
	}

	w.logger.InfoContext(ctx, "notification sending cycle completed", "count", claimed, "duration", time.Since(startTime))
}
