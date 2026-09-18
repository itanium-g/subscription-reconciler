package application

import (
	"context"
	"time"

	"github.com/example/subscription-reconciler/internal/domain"
	"github.com/example/subscription-reconciler/internal/infrastructure/postgres"
)

const defaultExpirationBatchSize int32 = 1000

// ExpirationService reconciles entitlements whose persisted expiry has
// elapsed.
type ExpirationService interface {
	ReconcileExpiredEntitlements(ctx context.Context, batchSize int32) (int32, error)
}

// NewExpirationService creates a new expiration reconciliation service.
func NewExpirationService(db postgres.Database) ExpirationService {
	return &expirationService{db: db}
}

type expirationService struct {
	db postgres.Database
}

// ReconcileExpiredEntitlements expires one batch of candidates. The concrete
// PostgreSQL repository performs the claim, projection update, and audit write
// in one transaction. The repository fallback keeps this service usable with
// smaller test doubles while retaining the same temporal ordering rule.
func (s *expirationService) ReconcileExpiredEntitlements(ctx context.Context, batchSize int32) (int32, error) {
	if batchSize <= 0 {
		batchSize = defaultExpirationBatchSize
	}

	if reconciler, ok := s.db.(postgres.ExpirationReconciler); ok {
		return reconciler.ReconcileExpiredEntitlements(ctx, batchSize)
	}

	entitlements, err := s.db.GetExpiredEntitlementsForReconciliation(ctx, batchSize)
	if err != nil {
		return 0, err
	}

	now := time.Now()
	reason := string(domain.EventTypeExpiration)
	var processed int32
	for _, entitlement := range entitlements {
		current := domain.Entitlement{
			Active:    entitlement.Active,
			ExpiresAt: entitlement.ExpiresAt,
		}
		if current.IsActive(now) || !entitlement.Active || entitlement.ExpiresAt == nil {
			continue
		}

		changed, err := s.db.UpsertEntitlement(
			ctx,
			entitlement.UserID,
			entitlement.Source,
			false,
			nil,
			&reason,
			entitlement.ExpiresAt.UnixMilli(),
			nil,
		)
		if err != nil {
			return processed, err
		}
		if changed {
			processed++
		}
	}

	return processed, nil
}
