package application

import (
	"context"
	"fmt"
	"time"

	"github.com/example/subscription-reconciler/internal/domain"
	"github.com/example/subscription-reconciler/internal/infrastructure/postgres"
)

// MarketplaceRevokeService handles marketplace revoke operations.
type MarketplaceRevokeService interface {
	RevokeMarketplaceAccess(ctx context.Context, request *domain.MarketplaceRevokeRequest) (*domain.MarketplaceRevokeResponse, error)
}

// NewMarketplaceRevokeService creates a new marketplace revoke service.
func NewMarketplaceRevokeService(db postgres.Database) MarketplaceRevokeService {
	return &marketplaceRevokeService{db: db}
}

type marketplaceRevokeService struct {
	db postgres.Database
}

// RevokeMarketplaceAccess revokes marketplace-granted access for specified users.
// Only affects MARKETPLACE source; leaves STORE and CARRIER unchanged.
func (s *marketplaceRevokeService) RevokeMarketplaceAccess(ctx context.Context, request *domain.MarketplaceRevokeRequest) (*domain.MarketplaceRevokeResponse, error) {
	// Validate input
	if err := request.Validate(); err != nil {
		return &domain.MarketplaceRevokeResponse{
			Accepted: false,
			Count:    0,
			Message:  err.Error(),
		}, err
	}

	// Process each user
	batchTS := time.Now()
	var processed int32
	for _, userID := range request.UserIDs {
		// Use YYYY-MM as the idempotency key for the monthly bulk request
		eventID := fmt.Sprintf("marketplace_revoke_%s_%s", userID, batchTS.Format("2006-01"))

		// Record the revocation in the immutable history table.
		// ON CONFLICT (event_id) DO NOTHING acts as our idempotency gate.
		inserted, err := s.db.InsertMarketplaceRevocation(ctx, eventID, userID)
		if err != nil || !inserted {
			// Skip if error or already processed this month
			continue
		}

		// Only update MARKETPLACE source, set active=false
		reason := "MARKETPLACE_REVOKE"
		if _, err := s.db.UpsertEntitlement(ctx, userID, "MARKETPLACE", false, nil, &reason, batchTS.UnixMilli(), nil); err != nil {
			// Log error but continue with other users
			continue
		}
		processed++
	}

	return &domain.MarketplaceRevokeResponse{
		Accepted: true,
		Count:    processed,
		Message:  "Marketplace access revoked",
	}, nil
}
