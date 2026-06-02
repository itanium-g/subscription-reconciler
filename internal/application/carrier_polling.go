package application

import (
	"context"
	"math/rand"
	"time"

	"github.com/example/adora/internal/domain"
	"github.com/example/adora/internal/infrastructure/postgres"
)

// CarrierPollingService handles carrier reconciliation.
type CarrierPollingService interface {
	// PollCarriersForUsers queries and updates carrier status for a batch of users.
	PollCarriersForUsers(ctx context.Context, batchSize int32) (int32, error)

	// MockCarrierStatus simulates a carrier API call.
	MockCarrierStatus(userID string) domain.CarrierPlanStatus
}

// NewCarrierPollingService creates a new carrier polling service.
func NewCarrierPollingService(db postgres.Database) CarrierPollingService {
	return &carrierPollingService{
		db: db,
	}
}

type carrierPollingService struct {
	db postgres.Database
}

// PollCarriersForUsers polls carrier status for a batch of users.
// Uses FOR UPDATE SKIP LOCKED to coordinate multiple workers.
func (s *carrierPollingService) PollCarriersForUsers(ctx context.Context, batchSize int32) (int32, error) {
	// Query users for polling (FOR UPDATE SKIP LOCKED for concurrent safety)
	entitlements, err := s.db.GetCarrierEntitlementsForPolling(ctx, batchSize)
	if err != nil {
		return 0, err
	}

	// Process each user
	var processed int32
	for _, ent := range entitlements {
		// Get current carrier status (mocked)
		status := s.MockCarrierStatus(ent.UserID)

		// Map status to entitlement state
		active := status == domain.CarrierStatusActive
		reason := "CARRIER_POLL"

		// Update entitlement
		if err := s.db.UpsertEntitlement(ctx, ent.UserID, "CARRIER", active, nil, &reason, time.Now().UnixMilli()); err != nil {
			// Log error but continue with other users
			continue
		}

		// Update polling timestamp
		if err := s.db.UpdateEntitlementCarrierPolledAt(ctx, ent.UserID, "CARRIER"); err != nil {
			// Log error but continue
			continue
		}

		processed++
	}

	return processed, nil
}

// MockCarrierStatus simulates a carrier API response.
// Returns: 85% active, 10% inactive, 5% api_error
func (s *carrierPollingService) MockCarrierStatus(userID string) domain.CarrierPlanStatus {
	// Use user ID as seed for deterministic but varying results
	randSource := rand.NewSource(int64(hashUserID(userID)) + time.Now().Unix()/60) // Changes every minute
	r := rand.New(randSource)
	roll := r.Intn(100)

	switch {
	case roll < 85:
		return domain.CarrierStatusActive
	case roll < 95:
		return domain.CarrierStatusInactive
	default:
		return domain.CarrierStatusAPIError
	}
}

// hashUserID creates a hash of the user ID for seeding random.
func hashUserID(userID string) uint64 {
	h := uint64(5381)
	for _, c := range userID {
		h = ((h << 5) + h) + uint64(c)
	}
	return h
}
