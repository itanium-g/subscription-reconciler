package postgres

import "context"

// Repository defines database operations for entitlements.
type Repository interface {
	// Entitlement operations
	GetEntitlementByUserAndSource(ctx context.Context, userID string, source string) (interface{}, error)
	UpsertEntitlement(ctx context.Context, userID string, source string, active bool, expiresAt interface{}, reason interface{}) error

	// Event operations
	InsertStoreEvent(ctx context.Context, eventID string, userID string, eventType string, eventTimeMs int64) error
	GetLastEventTime(ctx context.Context, userID string) (int64, error)

	// Deduplication
	IsProcessed(ctx context.Context, eventID string, source string) (bool, error)
	MarkProcessed(ctx context.Context, eventID string, source string) error

	// Notifications
	ScheduleNotification(ctx context.Context, userID string, notificationType string, scheduledFor interface{}) error
	GetDueNotifications(ctx context.Context) (interface{}, error)
	MarkNotificationSent(ctx context.Context, userID string, notificationType string, scheduledFor interface{}) error
}
