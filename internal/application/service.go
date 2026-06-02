package application

import "context"

// EntitlementService handles entitlement business logic.
type EntitlementService interface {
	// Reconcile processes an incoming event and updates entitlement state.
	Reconcile(ctx context.Context, eventID string, userID string, eventType string, eventTimeMs int64) error

	// GetCanonicalEntitlement returns the highest-priority active entitlement for a user.
	GetCanonicalEntitlement(ctx context.Context, userID string) (interface{}, error)

	// RevokeMarketplaceAccess revokes marketplace-granted access for users.
	RevokeMarketplaceAccess(ctx context.Context, userIDs []string) error

	// PollCarrier checks carrier status for a user and updates entitlement.
	PollCarrier(ctx context.Context, userID string) error

	// ScheduleExpirationNotifications checks for upcoming expirations and schedules notifications.
	ScheduleExpirationNotifications(ctx context.Context) error

	// SendScheduledNotifications sends notifications that are due.
	SendScheduledNotifications(ctx context.Context) error
}
