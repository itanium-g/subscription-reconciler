package application

import (
	"context"
	"time"

	"github.com/example/subscription-reconciler/internal/domain"
	"github.com/example/subscription-reconciler/internal/infrastructure/postgres"
)

// EntitlementQueryService handles entitlement queries.
type EntitlementQueryService interface {
	GetCanonicalEntitlement(ctx context.Context, userID string) (*domain.EntitlementResponse, error)
}

// NewEntitlementQueryService creates a new entitlement query service.
func NewEntitlementQueryService(db postgres.Database) EntitlementQueryService {
	return &entitlementQueryService{db: db}
}

type entitlementQueryService struct {
	db postgres.Database
}

// GetCanonicalEntitlement returns the highest-priority active entitlement for a user.
// Priority: STORE > CARRIER > MARKETPLACE > NONE
func (s *entitlementQueryService) GetCanonicalEntitlement(ctx context.Context, userID string) (*domain.EntitlementResponse, error) {
	// Query all entitlements for this user
	entitlements, err := s.db.GetEntitlementsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	// If no entitlements exist, return NONE
	if len(entitlements) == 0 {
		return &domain.EntitlementResponse{
			Active: false,
			Source: "NONE",
		}, nil
	}

	// Find the highest-priority active entitlement
	var bestEntitlement *postgres.Entitlement
	var bestPriority int
	now := time.Now()

	for i, ent := range entitlements {
		priority := domain.SourcePriority[ent.Source]

		// The expiration worker is eventually consistent. Resolve against the
		// current time as well as the persisted active flag so an expired
		// higher-priority source cannot hide a valid lower-priority source.
		current := domain.Entitlement{
			Active:    ent.Active,
			ExpiresAt: ent.ExpiresAt,
		}
		if current.IsActive(now) && priority > bestPriority {
			bestEntitlement = &entitlements[i]
			bestPriority = priority
		}
	}

	// If we found an active entitlement, return it
	if bestEntitlement != nil {
		return &domain.EntitlementResponse{
			Active:        bestEntitlement.Active,
			Source:        bestEntitlement.Source,
			ExpiresAt:     bestEntitlement.ExpiresAt,
			LastChangedAt: bestEntitlement.UpdatedAt,
			Reason:        bestEntitlement.Reason,
		}, nil
	}

	// No active entitlements found, return NONE
	// But return the most recently updated entitlement for reference
	mostRecent := &entitlements[0]
	for i := 1; i < len(entitlements); i++ {
		if entitlements[i].UpdatedAt.After(mostRecent.UpdatedAt) {
			mostRecent = &entitlements[i]
		}
	}

	return &domain.EntitlementResponse{
		Active:        false,
		Source:        "NONE",
		LastChangedAt: mostRecent.UpdatedAt,
	}, nil
}
