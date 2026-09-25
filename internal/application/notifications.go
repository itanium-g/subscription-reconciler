package application

import (
	"context"

	"github.com/example/subscription-reconciler/internal/infrastructure/postgres"
)

// NotificationService handles notification operations.
type NotificationService interface {
	SendDueNotifications(ctx context.Context, batchSize int32) (int32, error)
}

// NewNotificationService creates a new notification service.
func NewNotificationService(db postgres.Database) NotificationService {
	return &notificationService{db: db}
}

type notificationService struct {
	db postgres.Database
}

// SendDueNotifications atomically claims a batch of due notifications.
// Claiming marks notifications as sent before returning, which prevents
// competing workers from dispatching the same notification twice.
func (s *notificationService) SendDueNotifications(ctx context.Context, batchSize int32) (int32, error) {
	notifications, err := s.db.ClaimDueNotifications(ctx, batchSize)
	if err != nil {
		return 0, err
	}

	return int32(len(notifications)), nil
}
