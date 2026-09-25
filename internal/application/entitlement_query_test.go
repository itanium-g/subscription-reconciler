package application

import (
	"context"
	"testing"
	"time"

	"github.com/example/subscription-reconciler/internal/infrastructure/postgres"
	"github.com/stretchr/testify/require"
)

type entitlementQueryDatabase struct {
	postgres.Database
	entitlements []postgres.Entitlement
}

func (d *entitlementQueryDatabase) GetEntitlementsByUser(_ context.Context, _ string) ([]postgres.Entitlement, error) {
	return d.entitlements, nil
}

func TestGetCanonicalEntitlementExpiredSourceFallsBack(t *testing.T) {
	expiredAt := time.Now().Add(-time.Minute)
	futureAt := time.Now().Add(time.Hour)

	db := &entitlementQueryDatabase{
		entitlements: []postgres.Entitlement{
			{
				UserID:    "user_expiry_fallback",
				Source:    "STORE",
				Active:    true,
				ExpiresAt: &expiredAt,
				UpdatedAt: time.Now().Add(-time.Hour),
			},
			{
				UserID:    "user_expiry_fallback",
				Source:    "CARRIER",
				Active:    true,
				ExpiresAt: &futureAt,
				UpdatedAt: time.Now().Add(-2 * time.Hour),
			},
		},
	}

	response, err := NewEntitlementQueryService(db).GetCanonicalEntitlement(context.Background(), "user_expiry_fallback")

	require.NoError(t, err)
	require.True(t, response.Active)
	require.Equal(t, "CARRIER", response.Source)
}

func TestGetCanonicalEntitlementReturnsNoneWhenAllSourcesExpired(t *testing.T) {
	expiredAt := time.Now().Add(-time.Minute)

	db := &entitlementQueryDatabase{
		entitlements: []postgres.Entitlement{
			{
				UserID:    "user_all_expired",
				Source:    "STORE",
				Active:    true,
				ExpiresAt: &expiredAt,
				UpdatedAt: time.Now(),
			},
		},
	}

	response, err := NewEntitlementQueryService(db).GetCanonicalEntitlement(context.Background(), "user_all_expired")

	require.NoError(t, err)
	require.False(t, response.Active)
	require.Equal(t, "NONE", response.Source)
}
