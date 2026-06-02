package application

import (
	"context"
	"time"

	"github.com/example/subscription-reconciler/internal/domain"
	"github.com/example/subscription-reconciler/internal/infrastructure/postgres"
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
//
// Idempotency is enforced at the store_events level: InsertStoreEvent uses
// ON CONFLICT (event_id) DO NOTHING, so concurrent duplicate deliveries are
// safe — the second caller sees RowsAffected=0 and returns a duplicate
// response without touching entitlement state.
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

	// Step 2: Atomically insert event (idempotency gate).
	// ON CONFLICT (event_id) DO NOTHING ensures concurrent duplicates are safe.
	inserted, err := s.db.InsertStoreEvent(ctx, payload.EventID, payload.UserID, payload.Type, payload.EventTimeMs, &payload.ProductID)
	if err != nil {
		return nil, err
	}

	if !inserted {
		// Duplicate event — already in store_events.
		return &domain.StoreWebhookResponse{
			EventID:     payload.EventID,
			UserID:      payload.UserID,
			Accepted:    true,
			IsDuplicate: true,
			Message:     "Duplicate event, previously processed",
		}, nil
	}

	// Step 3: Calculate new entitlement state based on event type
	active, expiresAt, reason := s.computeStateTransition(payload.Type, payload.EventTimeMs)

	// Step 4: Upsert entitlement state for STORE source.
	// Returns false if a newer event already owns the projection (late arrival).
	stateChanged, err := s.db.UpsertEntitlement(ctx, payload.UserID, "STORE", active, expiresAt, &reason, payload.EventTimeMs, &payload.EventID)
	if err != nil {
		return nil, err
	}

	// Step 5: Schedule expiration notification only when state actually changed
	// to an active grant with an expiry date. Prevents scheduling spurious
	// notifications for late-arriving events whose upsert was skipped.
	if stateChanged && active && expiresAt != nil {
		notifyAt := expiresAt.Add(-24 * time.Hour)
		_ = s.db.ScheduleNotification(ctx, payload.UserID, "PREMIUM_EXPIRES_SOON", notifyAt)
	}

	// Step 6: Mark event as processed (belt-and-suspenders idempotency)
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
