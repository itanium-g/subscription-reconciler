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
func NewMarketplaceRevokeService(db postgres.MarketplaceRevocationRepository) MarketplaceRevokeService {
	return &marketplaceRevokeService{db: db}
}

type marketplaceRevokeService struct {
	db postgres.MarketplaceRevocationRepository
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

	// Process each user independently so one failed user can be retried without
	// undoing successful users from the same bulk request.
	batchTS := time.Now()
	var processed int32
	for _, userID := range request.UserIDs {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("marketplace revoke canceled before processing user %q: %w", userID, err)
		}

		// Use YYYY-MM as the idempotency key for the monthly bulk request
		eventID := fmt.Sprintf("marketplace_revoke_%s_%s", userID, batchTS.Format("2006-01"))
		var inserted bool
		reason := "MARKETPLACE_REVOKE"
		err := s.db.WithMarketplaceRevocationTransaction(ctx, func(tx postgres.MarketplaceRevocationTransaction) error {
			if err := ctx.Err(); err != nil {
				return err
			}

			// The immutable event insert is the monthly idempotency gate. It is
			// inside this transaction so failures below leave it retryable.
			var err error
			inserted, err = tx.InsertMarketplaceRevocation(ctx, eventID, userID)
			if err != nil {
				return fmt.Errorf("record revocation event: %w", err)
			}
			if !inserted {
				return nil
			}

			if err := ctx.Err(); err != nil {
				return err
			}
			if _, err := tx.UpsertEntitlement(ctx, userID, "MARKETPLACE", false, nil, &reason, batchTS.UnixMilli(), &eventID); err != nil {
				return fmt.Errorf("upsert marketplace entitlement: %w", err)
			}

			if err := ctx.Err(); err != nil {
				return err
			}
			if err := tx.MarkEventProcessed(ctx, eventID, "MARKETPLACE"); err != nil {
				return fmt.Errorf("mark revocation event processed: %w", err)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("process marketplace revocation for user %q (event %q): %w", userID, eventID, err)
		}
		if inserted {
			processed++
		}
	}

	return &domain.MarketplaceRevokeResponse{
		Accepted: true,
		Count:    processed,
		Message:  "Marketplace access revoked",
	}, nil
}
