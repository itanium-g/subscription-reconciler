package application

import (
	"context"
	"log/slog"

	"github.com/example/adora/internal/infrastructure/postgres"
)

// NotificationService handles notification operations.
type NotificationService interface {
	SendDueNotifications(ctx context.Context) (int32, error)
}

// NewNotificationService creates a new notification service.
func NewNotificationService(db postgres.Database) NotificationService {
	return &notificationService{db: db}
}

type notificationService struct {
	db postgres.Database
}

// SendDueNotifications sends all notifications that are due.
// In this implementation, "sending" means marking as sent in the database.
// A real system would integrate with SMS/email provider here.
func (s *notificationService) SendDueNotifications(ctx context.Context) (int32, error) {
	// Fetch notifications that are due (scheduled_for <= now and sent_at is null)
	notifications, err := s.db.GetDueNotifications(ctx, 1000) // Process up to 1000 per cycle
	if err != nil {
		return 0, err
	}

	if len(notifications) == 0 {
		return 0, nil
	}

	// Mark each notification as sent
	var sent int32
	for _, notif := range notifications {
		if err := s.db.MarkNotificationSent(ctx, notif.ID); err != nil {
			// Log error but continue with other notifications
			slog.Error("failed to mark notification sent", "id", notif.ID, "err", err)
			continue
		}
		sent++

		// In a real system, we would send the notification here:
		// - Send SMS: "Your premium expires soon"
		// - Send email: "Your subscription will expire on ..."
		// - Send push notification, etc.
		// For this assignment, we just mark it sent.
	}

	return sent, nil
}
