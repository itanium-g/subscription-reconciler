package application

import (
	"context"
	"time"

	"github.com/example/subscription-reconciler/internal/domain"
	"github.com/example/subscription-reconciler/internal/infrastructure/postgres"
)

// CarrierPollingService polls the carrier API and updates entitlements.
type CarrierPollingService interface {
	PollCarriersForUsers(ctx context.Context, batchSize int32, dueBefore time.Time) (int32, error)
}

// NewCarrierPollingService creates a new carrier polling service.
// client is the domain.CarrierClient used to query carrier plan status —
// in production this is an HTTP client; in tests it can be a stub.
func NewCarrierPollingService(db postgres.CarrierPollingRepository, client domain.CarrierClient) CarrierPollingService {
	return &carrierPollingService{
		db:     db,
		client: client,
	}
}

type carrierPollingService struct {
	db     postgres.CarrierPollingRepository
	client domain.CarrierClient
}

// PollCarriersForUsers polls carrier status for a batch of users.
// Uses FOR UPDATE SKIP LOCKED to coordinate multiple concurrent workers.
//
// API errors and unchanged statuses leave the projection and audit log alone.
// The claim itself advances carrier_polled_at so failed calls become due again
// after the configured staleness window.
func (s *carrierPollingService) PollCarriersForUsers(ctx context.Context, batchSize int32, dueBefore time.Time) (int32, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	entitlements, err := s.db.ClaimDueCarrierEntitlements(ctx, batchSize, dueBefore)
	if err != nil {
		return 0, err
	}

	var processed int32
	for _, ent := range entitlements {
		if err := ctx.Err(); err != nil {
			return processed, err
		}

		status, err := s.client.GetPlanStatus(ctx, ent.UserID)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return processed, ctxErr
		}

		// A failed request was still an attempted poll; the claim timestamp
		// ensures it will be retried only after it becomes stale again.
		if err != nil || status == domain.CarrierStatusAPIError {
			processed++
			continue
		}

		var active bool
		switch status {
		case domain.CarrierStatusActive:
			active = true
		case domain.CarrierStatusInactive:
			active = false
		default:
			processed++
			continue
		}
		if active == ent.Active {
			processed++
			continue
		}

		reason := "CARRIER_POLL"
		if _, err := s.db.UpsertEntitlement(ctx, ent.UserID, "CARRIER", active, nil, &reason, time.Now().UnixMilli(), nil); err != nil {
			return processed, err
		}

		processed++
	}

	return processed, nil
}
