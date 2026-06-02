package application

import (
	"context"
	"time"

	"github.com/example/subscription-reconciler/internal/domain"
	"github.com/example/subscription-reconciler/internal/infrastructure/postgres"
)

// CarrierPollingService polls the carrier API and updates entitlements.
type CarrierPollingService interface {
	PollCarriersForUsers(ctx context.Context, batchSize int32) (int32, error)
}

// NewCarrierPollingService creates a new carrier polling service.
// client is the domain.CarrierClient used to query carrier plan status —
// in production this is an HTTP client; in tests it can be a stub.
func NewCarrierPollingService(db postgres.Database, client domain.CarrierClient) CarrierPollingService {
	return &carrierPollingService{
		db:     db,
		client: client,
	}
}

type carrierPollingService struct {
	db     postgres.Database
	client domain.CarrierClient
}

// PollCarriersForUsers polls carrier status for a batch of users.
// Uses FOR UPDATE SKIP LOCKED to coordinate multiple concurrent workers.
//
// On api_error the entitlement projection is left unchanged; only the
// carrier_polled_at timestamp is advanced so the user re-enters the queue.
func (s *carrierPollingService) PollCarriersForUsers(ctx context.Context, batchSize int32) (int32, error) {
	entitlements, err := s.db.GetCarrierEntitlementsForPolling(ctx, batchSize)
	if err != nil {
		return 0, err
	}

	var processed int32
	for _, ent := range entitlements {
		status, err := s.client.GetPlanStatus(ent.UserID)

		// Network error from GetPlanStatus is returned as (api_error, err).
		// Treat any error the same as api_error: preserve existing state.
		if err != nil || status == domain.CarrierStatusAPIError {
			continue
		}

		active := status == domain.CarrierStatusActive
		reason := "CARRIER_POLL"

		if err := s.db.UpsertEntitlement(ctx, ent.UserID, "CARRIER", active, nil, &reason, time.Now().UnixMilli(), nil); err != nil {
			continue
		}

		processed++
	}

	return processed, nil
}
