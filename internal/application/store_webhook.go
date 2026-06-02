package application

import (
	"context"
	"time"

	"github.com/example/adora/internal/domain"
	"github.com/example/adora/internal/infrastructure/postgres"
)

// StoreWebhookService handles store webhook ingestion and reconciliation.
type StoreWebhookService interface {
	ProcessStoreWebhook(ctx context.Context, payload *domain.StoreWebhookPayload) (*domain.StoreWebhookResponse, error)
}

// NewStoreWebhookService creates a new store webhook service.
func NewStoreWebhookService(db postgres.Database) StoreWebhookService {
	return &storeWebhookService{db: db}
}

type storeWebhookService struct {
	db postgres.Database
}

// ProcessStoreWebhook handles incoming store webhook events.
// It implements idempotency, event ordering, and state reconciliation.
func (s *storeWebhookService) ProcessStoreWebhook(ctx context.Context, payload *domain.StoreWebhookPayload) (*domain.StoreWebhookResponse, error) {
	// Step 1: Validate input
	if err := payload.Validate(); err != nil {
		return &domain.StoreWebhookResponse{
			EventID:  payload.EventID,
			UserID:   payload.UserID,
			Accepted: false,
			Message:  err.Error(),
		}, err
	}

	// Step 2: Check if already processed (idempotency)
	isProcessed, err := s.db.IsEventProcessed(ctx, payload.EventID, "STORE")
	if err != nil {
		return nil, err
	}

	if isProcessed {
		// Already processed, return success with isDuplicate flag
		return &domain.StoreWebhookResponse{
			EventID:     payload.EventID,
			UserID:      payload.UserID,
			Accepted:    true,
			IsDuplicate: true,
			Message:     "Duplicate event, previously processed",
		}, nil
	}

	// Step 3: Store the event (immutable history)
	if err := s.db.InsertStoreEvent(ctx, payload.EventID, payload.UserID, payload.Type, payload.EventTimeMs, &payload.ProductID); err != nil {
		return nil, err
	}

	// Step 4: Calculate new entitlement state based on event type
	active, expiresAt, reason := s.computeStateTransition(payload.Type, payload.EventTimeMs)

	// Step 5: Upsert entitlement state for STORE source.
	// The DB enforces ordering atomically: ON CONFLICT DO UPDATE WHERE
	// last_event_time < EXCLUDED.last_event_time. Late-arriving events are
	// stored in store_events but their state change is silently ignored by
	// the DB if a newer event already owns the projection.
	if err := s.db.UpsertEntitlement(ctx, payload.UserID, "STORE", active, expiresAt, &reason, payload.EventTimeMs); err != nil {
		return nil, err
	}

	// Step 6: If expiring soon, schedule notification
	if active && expiresAt != nil && timeUntilExpiry(*expiresAt) <= 24*time.Hour {
		_ = s.db.ScheduleNotification(ctx, payload.UserID, "PREMIUM_EXPIRES_SOON", *expiresAt)
	}

	// Step 7: Mark event as processed (idempotency)
	if err := s.db.MarkEventProcessed(ctx, payload.EventID, "STORE"); err != nil {
		return nil, err
	}

	return &domain.StoreWebhookResponse{
		EventID:     payload.EventID,
		UserID:      payload.UserID,
		Accepted:    true,
		IsDuplicate: false,
		Message:     "Event accepted and processed",
	}, nil
}

// computeStateTransition determines the new entitlement state based on event type.
// expiresAt is anchored to eventTimeMs, not to the current wall clock, so that
// late-arriving events do not grant a future window they are not entitled to.
func (s *storeWebhookService) computeStateTransition(eventType string, eventTimeMs int64) (active bool, expiresAt *time.Time, reason string) {
	eventTime := time.UnixMilli(eventTimeMs)

	switch eventType {
	case "INITIAL_PURCHASE", "RENEWAL", "UN_CANCELLATION":
		// Premium access granted for one month from the event time.
		active = true
		expiry := eventTime.AddDate(0, 1, 0)
		expiresAt = &expiry
		reason = eventType

	case "CANCELLATION":
		// User loses premium access immediately.
		active = false
		expiresAt = nil
		reason = eventType

	case "BILLING_ISSUE":
		// Temporary suspension — access revoked, no expiry date set.
		active = false
		expiresAt = nil
		reason = eventType

	case "EXPIRATION":
		// Premium grant has elapsed.
		active = false
		expiresAt = nil
		reason = eventType
	}

	return active, expiresAt, reason
}

// timeUntilExpiry calculates time until expiration.
func timeUntilExpiry(expiresAt time.Time) time.Duration {
	return time.Until(expiresAt)
}
