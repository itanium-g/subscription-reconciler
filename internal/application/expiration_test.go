package application

import (
	"context"
	"testing"
	"time"

	"github.com/example/subscription-reconciler/internal/infrastructure/postgres"
	"github.com/stretchr/testify/require"
)

type expirationDatabase struct {
	postgres.Database
	expired       []postgres.Entitlement
	lastEventTime int64
	reason        string
}

func (d *expirationDatabase) GetExpiredEntitlementsForReconciliation(_ context.Context, _ int32) ([]postgres.Entitlement, error) {
	return d.expired, nil
}

func (d *expirationDatabase) UpsertEntitlement(_ context.Context, _ string, _ string, _ bool, _ *time.Time, reason *string, lastEventTime int64, _ *string) (bool, error) {
	d.lastEventTime = lastEventTime
	if reason != nil {
		d.reason = *reason
	}
	return true, nil
}

func TestReconcileExpiredEntitlementUsesExpirationTimestampForOrdering(t *testing.T) {
	expiredAt := time.Now().Add(-time.Hour).Truncate(time.Millisecond)
	db := &expirationDatabase{
		expired: []postgres.Entitlement{{
			UserID:    "user_expiration_ordering",
			Source:    "STORE",
			Active:    true,
			ExpiresAt: &expiredAt,
		}},
	}

	processed, err := NewExpirationService(db).ReconcileExpiredEntitlements(context.Background(), 1)

	require.NoError(t, err)
	require.Equal(t, int32(1), processed)
	require.Equal(t, expiredAt.UnixMilli(), db.lastEventTime)
	require.Equal(t, "EXPIRATION", db.reason)
}

func TestReconcileExpiredEntitlementsSkipsActiveFutureCandidates(t *testing.T) {
	futureAt := time.Now().Add(time.Hour)
	db := &expirationDatabase{
		expired: []postgres.Entitlement{{
			UserID:    "user_not_expired",
			Source:    "STORE",
			Active:    true,
			ExpiresAt: &futureAt,
		}},
	}

	processed, err := NewExpirationService(db).ReconcileExpiredEntitlements(context.Background(), 1)

	require.NoError(t, err)
	require.Zero(t, processed)
}
