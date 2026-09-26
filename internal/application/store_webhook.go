package application

import (
	"context"
	"fmt"
	"time"

	"github.com/example/subscription-reconciler/internal/domain"
	"github.com/example/subscription-reconciler/internal/infrastructure/postgres"
)

// StoreWebhookService handles store webhook ingestion and reconciliation.
type StoreWebhookService interface {
	ProcessStoreWebhook(ctx context.Context, payload *domain.StoreWebhookPayload) (*domain.StoreWebhookResponse, error)
}

// NewStoreWebhookService creates a new store webhook service.
func NewStoreWebhookService(db postgres.StoreWebhookRepository) StoreWebhookService {
	return &storeWebhookService{db: db}
}

type storeWebhookService struct {
	db postgres.StoreWebhookRepository
}

// ProcessStoreWebhook handles incoming store webhook events.
// It implements idempotency, event ordering, and state reconciliation.
//
// Event recording, entitlement reconciliation, notification scheduling, and
// processed marking commit together. If any write fails, the event insert is
// rolled back so the provider can retry it.
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

	active, expiresAt, reason := s.computeStateTransition(payload.Type, payload.EventTimeMs)
	inserted := false

	err := s.db.WithStoreWebhookTransaction(ctx, func(tx postgres.StoreWebhookTransaction) error {
		// ON CONFLICT (event_id) DO NOTHING makes duplicate deliveries safe.
		var err error
		inserted, err = tx.InsertStoreEvent(ctx, payload.EventID, payload.UserID, payload.Type, payload.EventTimeMs, &payload.ProductID)
		if err != nil {
			return fmt.Errorf("insert store event: %w", err)
		}
		if !inserted {
			return nil
		}

		// Returns false when a newer event owns the projection or when the
		// timestamp advanced without changing the entitlement state.
		stateChanged, err := tx.UpsertEntitlement(ctx, payload.UserID, "STORE", active, expiresAt, &reason, payload.EventTimeMs, &payload.EventID)
		if err != nil {
			return fmt.Errorf("upsert store entitlement: %w", err)
		}

		// Schedule only for a real transition to an active, expiring grant.
		if stateChanged && active && expiresAt != nil {
			notifyAt := expiresAt.Add(-24 * time.Hour)
			if err := tx.ScheduleNotification(ctx, payload.UserID, "PREMIUM_EXPIRES_SOON", notifyAt); err != nil {
				return fmt.Errorf("schedule store expiration notification: %w", err)
			}
		}

		if err := tx.MarkEventProcessed(ctx, payload.EventID, "STORE"); err != nil {
			return fmt.Errorf("mark store event processed: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if !inserted {
		return &domain.StoreWebhookResponse{
			EventID:     payload.EventID,
			UserID:      payload.UserID,
			Accepted:    true,
			IsDuplicate: true,
			Message:     "Duplicate event, previously processed",
		}, nil
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
		if !expiry.After(time.Now()) {
			// A late-arriving grant has already elapsed by the time it is
			// received. Preserve its historical expiry, but do not grant
			// access retroactively.
			active = false
			reason = string(domain.EventTypeExpiration)
		}

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
