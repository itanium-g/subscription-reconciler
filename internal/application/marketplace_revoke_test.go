package application

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/example/subscription-reconciler/internal/domain"
	"github.com/example/subscription-reconciler/internal/infrastructure/postgres"
	"github.com/stretchr/testify/require"
)

func TestRevokeMarketplaceAccessValidatesRequest(t *testing.T) {
	service := NewMarketplaceRevokeService(newFakeMarketplaceRevocationDatabase())

	for _, testCase := range []struct {
		name    string
		request *domain.MarketplaceRevokeRequest
		wantErr domain.DomainError
	}{
		{name: "nil request", request: nil, wantErr: domain.ErrEmptyUserIDList},
		{name: "empty user list", request: &domain.MarketplaceRevokeRequest{}, wantErr: domain.ErrEmptyUserIDList},
		{name: "too many users", request: &domain.MarketplaceRevokeRequest{UserIDs: make([]string, 10001)}, wantErr: domain.ErrTooManyUserIDs},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response, err := service.RevokeMarketplaceAccess(context.Background(), testCase.request)

			var domainErr domain.DomainError
			require.ErrorAs(t, err, &domainErr)
			require.Equal(t, testCase.wantErr.Code, domainErr.Code)
			require.NotNil(t, response)
			require.False(t, response.Accepted)
			require.Zero(t, response.Count)
		})
	}
}

func TestRevokeMarketplaceAccessCommitsEventEntitlementAndAuditTogether(t *testing.T) {
	db := newFakeMarketplaceRevocationDatabase()
	service := NewMarketplaceRevokeService(db)
	ctx := context.Background()
	userID := "marketplace_user"

	response, err := service.RevokeMarketplaceAccess(ctx, &domain.MarketplaceRevokeRequest{UserIDs: []string{userID}})

	require.NoError(t, err)
	require.True(t, response.Accepted)
	require.Equal(t, int32(1), response.Count)
	state := db.snapshot()
	eventID := marketplaceRevocationEventID(userID, time.Now())
	require.Equal(t, userID, state.events[eventID])
	require.Equal(t, "MARKETPLACE", state.processed[eventID])
	require.False(t, state.entitlements[userID].active)
	require.Len(t, state.audits, 1)
	require.Equal(t, eventID, state.audits[0].triggeringEventID)
	require.Equal(t, "MARKETPLACE_REVOKE", state.audits[0].reason)
}

func TestRevokeMarketplaceAccessRollsBackAndReturnsActionableErrors(t *testing.T) {
	for _, failAt := range []string{"insert", "upsert", "mark_processed"} {
		t.Run(failAt, func(t *testing.T) {
			db := newFakeMarketplaceRevocationDatabase()
			db.failAt = failAt
			service := NewMarketplaceRevokeService(db)
			userID := "retry_user"

			response, err := service.RevokeMarketplaceAccess(context.Background(), &domain.MarketplaceRevokeRequest{UserIDs: []string{userID}})

			require.Error(t, err)
			require.Nil(t, response)
			require.Contains(t, err.Error(), userID)
			require.Contains(t, err.Error(), "marketplace_revoke_")
			state := db.snapshot()
			require.Empty(t, state.events)
			require.Empty(t, state.processed)
			require.Empty(t, state.entitlements)
			require.Empty(t, state.audits)

			db.failAt = ""
			response, err = service.RevokeMarketplaceAccess(context.Background(), &domain.MarketplaceRevokeRequest{UserIDs: []string{userID}})
			require.NoError(t, err)
			require.Equal(t, int32(1), response.Count)
			require.Len(t, db.snapshot().events, 1)
		})
	}
}

func TestRevokeMarketplaceAccessSkipsDuplicateAndAlreadyRevokedAudit(t *testing.T) {
	t.Run("duplicate monthly request", func(t *testing.T) {
		db := newFakeMarketplaceRevocationDatabase()
		service := NewMarketplaceRevokeService(db)
		request := &domain.MarketplaceRevokeRequest{UserIDs: []string{"duplicate_user"}}

		first, err := service.RevokeMarketplaceAccess(context.Background(), request)
		require.NoError(t, err)
		require.Equal(t, int32(1), first.Count)

		second, err := service.RevokeMarketplaceAccess(context.Background(), request)
		require.NoError(t, err)
		require.Equal(t, int32(0), second.Count)
		require.Len(t, db.snapshot().audits, 1)
	})

	t.Run("already inactive entitlement", func(t *testing.T) {
		db := newFakeMarketplaceRevocationDatabase()
		db.state.entitlements["inactive_user"] = fakeMarketplaceEntitlement{active: false, lastEventTime: 1}
		service := NewMarketplaceRevokeService(db)

		response, err := service.RevokeMarketplaceAccess(context.Background(), &domain.MarketplaceRevokeRequest{UserIDs: []string{"inactive_user"}})

		require.NoError(t, err)
		require.Equal(t, int32(1), response.Count)
		state := db.snapshot()
		require.Len(t, state.events, 1)
		require.Len(t, state.processed, 1)
		require.Empty(t, state.audits)
	})
}

func TestRevokeMarketplaceAccessStopsPromptlyOnContextCancellation(t *testing.T) {
	db := newFakeMarketplaceRevocationDatabase()
	db.cancelAt = "upsert"
	service := NewMarketplaceRevokeService(db)
	ctx, cancel := context.WithCancel(context.Background())
	db.cancel = cancel

	response, err := service.RevokeMarketplaceAccess(ctx, &domain.MarketplaceRevokeRequest{UserIDs: []string{"cancel_user"}})

	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, response)
	require.Empty(t, db.snapshot().events)
	require.Empty(t, db.snapshot().processed)
	require.Empty(t, db.snapshot().entitlements)
	require.Empty(t, db.snapshot().audits)
}

func TestRevokeMarketplaceAccessDoesNotStartTransactionForCanceledContext(t *testing.T) {
	db := newFakeMarketplaceRevocationDatabase()
	service := NewMarketplaceRevokeService(db)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := service.RevokeMarketplaceAccess(ctx, &domain.MarketplaceRevokeRequest{UserIDs: []string{"cancel_user"}})

	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, db.transactionCount)
}

func marketplaceRevocationEventID(userID string, timestamp time.Time) string {
	return fmt.Sprintf("marketplace_revoke_%s_%s", userID, timestamp.Format("2006-01"))
}

type fakeMarketplaceRevocationDatabase struct {
	mu               sync.Mutex
	state            fakeMarketplaceRevocationState
	failAt           string
	cancelAt         string
	cancel           context.CancelFunc
	transactionCount int
}

type fakeMarketplaceRevocationState struct {
	events       map[string]string
	processed    map[string]string
	entitlements map[string]fakeMarketplaceEntitlement
	audits       []fakeMarketplaceAudit
}

type fakeMarketplaceEntitlement struct {
	active        bool
	lastEventTime int64
}

type fakeMarketplaceAudit struct {
	userID            string
	triggeringEventID string
	reason            string
	previousActive    *bool
	nextActive        bool
}

func newFakeMarketplaceRevocationDatabase() *fakeMarketplaceRevocationDatabase {
	return &fakeMarketplaceRevocationDatabase{state: fakeMarketplaceRevocationState{
		events:       make(map[string]string),
		processed:    make(map[string]string),
		entitlements: make(map[string]fakeMarketplaceEntitlement),
	}}
}

func (db *fakeMarketplaceRevocationDatabase) WithMarketplaceRevocationTransaction(ctx context.Context, fn func(postgres.MarketplaceRevocationTransaction) error) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.transactionCount++

	working := db.state.clone()
	tx := &fakeMarketplaceRevocationTransaction{db: db, state: &working}
	if err := fn(tx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	db.state = working
	return nil
}

func (db *fakeMarketplaceRevocationDatabase) GetMarketplaceRevocationByEventID(ctx context.Context, eventID string) (*postgres.MarketplaceRevocation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	state := db.snapshot()
	userID, exists := state.events[eventID]
	if !exists {
		return nil, sql.ErrNoRows
	}
	return &postgres.MarketplaceRevocation{EventID: eventID, UserID: userID}, nil
}

func (db *fakeMarketplaceRevocationDatabase) snapshot() fakeMarketplaceRevocationState {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.state.clone()
}

func (state fakeMarketplaceRevocationState) clone() fakeMarketplaceRevocationState {
	cloned := fakeMarketplaceRevocationState{
		events:       make(map[string]string, len(state.events)),
		processed:    make(map[string]string, len(state.processed)),
		entitlements: make(map[string]fakeMarketplaceEntitlement, len(state.entitlements)),
		audits:       append([]fakeMarketplaceAudit(nil), state.audits...),
	}
	for eventID, userID := range state.events {
		cloned.events[eventID] = userID
	}
	for eventID, source := range state.processed {
		cloned.processed[eventID] = source
	}
	for userID, entitlement := range state.entitlements {
		cloned.entitlements[userID] = entitlement
	}
	return cloned
}

type fakeMarketplaceRevocationTransaction struct {
	db    *fakeMarketplaceRevocationDatabase
	state *fakeMarketplaceRevocationState
}

func (tx *fakeMarketplaceRevocationTransaction) InsertMarketplaceRevocation(ctx context.Context, eventID string, userID string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if tx.db.failAt == "insert" {
		return false, errors.New("injected marketplace event insert failure")
	}
	if _, exists := tx.state.events[eventID]; exists {
		return false, nil
	}
	tx.state.events[eventID] = userID
	return true, nil
}

func (tx *fakeMarketplaceRevocationTransaction) UpsertEntitlement(ctx context.Context, userID string, _ string, active bool, _ *time.Time, reason *string, lastEventTime int64, triggeringEventID *string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if tx.db.failAt == "upsert" {
		return false, errors.New("injected marketplace entitlement upsert failure")
	}
	previous, exists := tx.state.entitlements[userID]
	if exists && lastEventTime <= previous.lastEventTime {
		return false, nil
	}
	stateChanged := !exists || previous.active != active
	tx.state.entitlements[userID] = fakeMarketplaceEntitlement{active: active, lastEventTime: lastEventTime}
	if stateChanged {
		var previousActive *bool
		if exists {
			previousActive = new(bool)
			*previousActive = previous.active
		}
		reasonValue := ""
		if reason != nil {
			reasonValue = *reason
		}
		eventID := ""
		if triggeringEventID != nil {
			eventID = *triggeringEventID
		}
		tx.state.audits = append(tx.state.audits, fakeMarketplaceAudit{
			userID:            userID,
			triggeringEventID: eventID,
			reason:            reasonValue,
			previousActive:    previousActive,
			nextActive:        active,
		})
	}
	if tx.db.cancelAt == "upsert" && tx.db.cancel != nil {
		tx.db.cancel()
	}
	return stateChanged, nil
}

func (tx *fakeMarketplaceRevocationTransaction) MarkEventProcessed(ctx context.Context, eventID string, source string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx.db.failAt == "mark_processed" {
		return errors.New("injected processed event failure")
	}
	tx.state.processed[eventID] = source
	return nil
}
