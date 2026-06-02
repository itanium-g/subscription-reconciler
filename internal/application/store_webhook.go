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

	// Step 4: Check last event time from STORE to determine if we should update state
	lastEventTime, err := s.db.GetLastEventTimeFromStore(ctx, payload.UserID)
	if err != nil {
		return nil, err
	}

	// Only update if this event is newer (or no prior event)
	shouldUpdateState := payload.EventTimeMs >= lastEventTime

	if shouldUpdateState {
		// Step 5: Calculate new entitlement state based on event type
		active, expiresAt, reason := s.computeStateTransition(payload.Type)

		// Step 6: Update entitlement state for STORE source
		if err := s.db.UpsertEntitlement(ctx, payload.UserID, "STORE", active, expiresAt, &reason, payload.EventTimeMs); err != nil {
			return nil, err
		}

		// Step 7: If expiring soon, schedule notification
		if active && expiresAt != nil && timeUntilExpiry(*expiresAt) <= 24*time.Hour {
			_ = s.db.ScheduleNotification(ctx, payload.UserID, "PREMIUM_EXPIRES_SOON", *expiresAt)
		}
	}

	// Step 8: Mark event as processed (idempotency)
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
func (s *storeWebhookService) computeStateTransition(eventType string) (active bool, expiresAt *time.Time, reason string) {
	now := time.Now()

	switch eventType {
	case "INITIAL_PURCHASE", "RENEWAL", "UN_CANCELLATION":
		// User gains premium access for 30 days
		active = true
		expiry := now.AddDate(0, 1, 0) // 30 days from now
		expiresAt = &expiry
		reason = eventType

	case "CANCELLATION":
		// User loses premium access immediately
		active = false
		expiresAt = nil
		reason = eventType

	case "BILLING_ISSUE":
		// Temporary suspension, but don't revoke completely
		active = false
		expiresAt = nil
		reason = eventType

	case "EXPIRATION":
		// Premium grant expired
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
