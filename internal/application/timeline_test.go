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

type timelineAuditLogRepository struct {
	postgres.AuditLogRepository

	logs       []postgres.AuditLog
	total      int64
	getErr     error
	countErr   error
	onGet      func()
	getCalls   int
	countCalls int
	gotUserID  string
	gotLimit   int32
	gotOffset  int32
}

func (r *timelineAuditLogRepository) GetAuditLogsByUser(_ context.Context, userID string, limit, offset int32) ([]postgres.AuditLog, error) {
	r.getCalls++
	r.gotUserID = userID
	r.gotLimit = limit
	r.gotOffset = offset
	if r.onGet != nil {
		r.onGet()
	}
	return r.logs, r.getErr
}

func (r *timelineAuditLogRepository) CountAuditLogsByUser(_ context.Context, _ string) (int64, error) {
	r.countCalls++
	return r.total, r.countErr
}

func TestGetEntitlementTimelineValidatesRequest(t *testing.T) {
	testCases := []struct {
		name    string
		userID  string
		limit   int32
		offset  int32
		wantErr string
	}{
		{name: "empty user ID", userID: "", limit: 10, offset: 0, wantErr: "MISSING_USER_ID"},
		{name: "invalid user ID prefix", userID: "1user", limit: 10, offset: 0, wantErr: "INVALID_USER_ID"},
		{name: "invalid user ID character", userID: "user-name", limit: 10, offset: 0, wantErr: "INVALID_USER_ID"},
		{name: "zero limit", userID: "user_1", limit: 0, offset: 0, wantErr: "INVALID_PARAMETERS"},
		{name: "limit over maximum", userID: "user_1", limit: 1001, offset: 0, wantErr: "INVALID_PARAMETERS"},
		{name: "negative offset", userID: "user_1", limit: 10, offset: -1, wantErr: "INVALID_PARAMETERS"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			repository := &timelineAuditLogRepository{}
			_, err := NewTimelineService(repository).GetEntitlementTimeline(context.Background(), testCase.userID, testCase.limit, testCase.offset)

			require.Error(t, err)
			var domainErr domain.DomainError
			require.ErrorAs(t, err, &domainErr)
			require.Equal(t, testCase.wantErr, domainErr.Code)
			require.Zero(t, repository.getCalls)
			require.Zero(t, repository.countCalls)
		})
	}
}

func TestGetEntitlementTimelineMapsPaginatedAuditLogs(t *testing.T) {
	createdAt := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	firstEventID := "event_first"
	firstReason := "INITIAL_PURCHASE"
	secondEventID := "event_second"
	secondReason := "RENEWAL"
	secondExpiry := createdAt.AddDate(0, 1, 0)
	repository := &timelineAuditLogRepository{
		logs: []postgres.AuditLog{
			{
				UserID:            "user_timeline_1",
				Source:            "STORE",
				NextActive:        true,
				NextExpiresAt:     &secondExpiry,
				TriggeringEventID: &secondEventID,
				Reason:            &secondReason,
				CreatedAt:         createdAt.Add(time.Hour),
			},
			{
				UserID:            "user_timeline_1",
				Source:            "STORE",
				NextActive:        true,
				TriggeringEventID: &firstEventID,
				Reason:            &firstReason,
				CreatedAt:         createdAt,
			},
		},
		total: 7,
	}

	response, err := NewTimelineService(repository).GetEntitlementTimeline(context.Background(), "user_timeline_1", 2, 3)

	require.NoError(t, err)
	require.Equal(t, "user_timeline_1", response.UserID)
	require.Equal(t, int64(7), response.Total)
	require.Equal(t, int32(2), response.Limit)
	require.Equal(t, int32(3), response.Offset)
	require.Equal(t, "event_second", *response.Entries[0].TriggeringEventID)
	require.Equal(t, "RENEWAL", *response.Entries[0].Reason)
	require.Equal(t, "STORE", response.Entries[0].Source)
	require.True(t, response.Entries[0].Active)
	require.Equal(t, &secondExpiry, response.Entries[0].ExpiresAt)
	require.Equal(t, createdAt.Add(time.Hour), response.Entries[0].Timestamp)
	require.Equal(t, "event_first", *response.Entries[1].TriggeringEventID)
	require.Equal(t, "INITIAL_PURCHASE", *response.Entries[1].Reason)
	require.Equal(t, createdAt, response.Entries[1].Timestamp)
	require.Equal(t, "user_timeline_1", repository.gotUserID)
	require.Equal(t, int32(2), repository.gotLimit)
	require.Equal(t, int32(3), repository.gotOffset)
	require.Equal(t, 1, repository.getCalls)
	require.Equal(t, 1, repository.countCalls)
}

func TestGetEntitlementTimelineReturnsEmptyEntries(t *testing.T) {
	repository := &timelineAuditLogRepository{total: 0}

	response, err := NewTimelineService(repository).GetEntitlementTimeline(context.Background(), "user_without_history", 100, 0)

	require.NoError(t, err)
	require.NotNil(t, response.Entries)
	require.Empty(t, response.Entries)
}

func TestGetEntitlementTimelinePropagatesRepositoryErrors(t *testing.T) {
	getErr := errors.New("audit query failed")
	t.Run("fetch audit logs", func(t *testing.T) {
		repository := &timelineAuditLogRepository{getErr: getErr}

		_, err := NewTimelineService(repository).GetEntitlementTimeline(context.Background(), "user_1", 10, 0)

		require.ErrorIs(t, err, getErr)
		require.Equal(t, 1, repository.getCalls)
		require.Zero(t, repository.countCalls)
	})

	countErr := errors.New("audit count failed")
	t.Run("count audit logs", func(t *testing.T) {
		repository := &timelineAuditLogRepository{countErr: countErr}

		_, err := NewTimelineService(repository).GetEntitlementTimeline(context.Background(), "user_1", 10, 0)

		require.ErrorIs(t, err, countErr)
		require.Equal(t, 1, repository.getCalls)
		require.Equal(t, 1, repository.countCalls)
	})
}

func TestGetEntitlementTimelineHandlesContextCancellation(t *testing.T) {
	t.Run("already canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		repository := &timelineAuditLogRepository{}

		_, err := NewTimelineService(repository).GetEntitlementTimeline(ctx, "user_1", 10, 0)

		require.ErrorIs(t, err, context.Canceled)
		require.Zero(t, repository.getCalls)
		require.Zero(t, repository.countCalls)
	})

	t.Run("canceled between repository queries", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		repository := &timelineAuditLogRepository{onGet: cancel}

		_, err := NewTimelineService(repository).GetEntitlementTimeline(ctx, "user_1", 10, 0)

		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, 1, repository.getCalls)
		require.Zero(t, repository.countCalls)
	})
}
