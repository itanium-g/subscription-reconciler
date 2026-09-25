package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/subscription-reconciler/internal/domain"
	"github.com/example/subscription-reconciler/internal/infrastructure/postgres"
	"github.com/stretchr/testify/require"
)

type carrierPollingDatabase struct {
	claimed       []postgres.Entitlement
	claimErr      error
	claimedLimit  int32
	claimedBefore time.Time
	claimCalls    int
	upserts       []carrierUpsert
	upsertErr     error
}

type carrierUpsert struct {
	userID string
	active bool
}

func (d *carrierPollingDatabase) ClaimDueCarrierEntitlements(_ context.Context, limit int32, dueBefore time.Time) ([]postgres.Entitlement, error) {
	d.claimCalls++
	d.claimedLimit = limit
	d.claimedBefore = dueBefore
	return d.claimed, d.claimErr
}

func (d *carrierPollingDatabase) UpsertEntitlement(_ context.Context, userID string, _ string, active bool, _ *time.Time, _ *string, _ int64, _ *string) (bool, error) {
	d.upserts = append(d.upserts, carrierUpsert{userID: userID, active: active})
	return true, d.upsertErr
}

type carrierPollingClient struct {
	statuses map[string]domain.CarrierPlanStatus
	errors   map[string]error
	calls    []string
	cancel   context.CancelFunc
}

func (c *carrierPollingClient) GetPlanStatus(_ context.Context, userID string) (domain.CarrierPlanStatus, error) {
	c.calls = append(c.calls, userID)
	if c.cancel != nil {
		c.cancel()
	}
	if err := c.errors[userID]; err != nil {
		return domain.CarrierStatusAPIError, err
	}
	return c.statuses[userID], nil
}

func TestPollCarriersForUsersEmptySet(t *testing.T) {
	dueBefore := time.Now().Add(-5 * time.Minute)
	db := &carrierPollingDatabase{}
	client := &carrierPollingClient{}

	processed, err := NewCarrierPollingService(db, client).PollCarriersForUsers(context.Background(), 25, dueBefore)

	require.NoError(t, err)
	require.Zero(t, processed)
	require.Equal(t, 1, db.claimCalls)
	require.Equal(t, int32(25), db.claimedLimit)
	require.Equal(t, dueBefore, db.claimedBefore)
	require.Empty(t, client.calls)
	require.Empty(t, db.upserts)
}

func TestPollCarriersForUsersAppliesOnlyStatusTransitions(t *testing.T) {
	db := &carrierPollingDatabase{claimed: []postgres.Entitlement{
		{UserID: "active_to_inactive", Source: "CARRIER", Active: true},
		{UserID: "inactive_to_active", Source: "CARRIER", Active: false},
	}}
	client := &carrierPollingClient{statuses: map[string]domain.CarrierPlanStatus{
		"active_to_inactive": domain.CarrierStatusInactive,
		"inactive_to_active": domain.CarrierStatusActive,
	}}

	processed, err := NewCarrierPollingService(db, client).PollCarriersForUsers(context.Background(), 2, time.Now())

	require.NoError(t, err)
	require.Equal(t, int32(2), processed)
	require.Equal(t, []carrierUpsert{
		{userID: "active_to_inactive", active: false},
		{userID: "inactive_to_active", active: true},
	}, db.upserts)
}

func TestPollCarriersForUsersSkipsUnchangedStatuses(t *testing.T) {
	db := &carrierPollingDatabase{claimed: []postgres.Entitlement{
		{UserID: "still_active", Source: "CARRIER", Active: true},
		{UserID: "still_inactive", Source: "CARRIER", Active: false},
	}}
	client := &carrierPollingClient{statuses: map[string]domain.CarrierPlanStatus{
		"still_active":   domain.CarrierStatusActive,
		"still_inactive": domain.CarrierStatusInactive,
	}}

	processed, err := NewCarrierPollingService(db, client).PollCarriersForUsers(context.Background(), 2, time.Now())

	require.NoError(t, err)
	require.Equal(t, int32(2), processed)
	require.Empty(t, db.upserts)
}

func TestPollCarriersForUsersAPIErrorPreservesEntitlements(t *testing.T) {
	apiErr := errors.New("carrier unavailable")
	db := &carrierPollingDatabase{claimed: []postgres.Entitlement{
		{UserID: "api_status_error", Source: "CARRIER", Active: true},
		{UserID: "network_error", Source: "CARRIER", Active: true},
	}}
	client := &carrierPollingClient{
		statuses: map[string]domain.CarrierPlanStatus{"api_status_error": domain.CarrierStatusAPIError},
		errors:   map[string]error{"network_error": apiErr},
	}

	processed, err := NewCarrierPollingService(db, client).PollCarriersForUsers(context.Background(), 2, time.Now())

	require.NoError(t, err)
	require.Equal(t, int32(2), processed)
	require.Empty(t, db.upserts)
}

func TestPollCarriersForUsersStopsOnContextCancellation(t *testing.T) {
	db := &carrierPollingDatabase{claimed: []postgres.Entitlement{
		{UserID: "first", Source: "CARRIER", Active: true},
		{UserID: "second", Source: "CARRIER", Active: true},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	client := &carrierPollingClient{
		statuses: map[string]domain.CarrierPlanStatus{"first": domain.CarrierStatusInactive},
		cancel:   cancel,
	}

	processed, err := NewCarrierPollingService(db, client).PollCarriersForUsers(ctx, 2, time.Now())

	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, processed)
	require.Equal(t, []string{"first"}, client.calls)
	require.Empty(t, db.upserts)
}
