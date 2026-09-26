package application

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/example/subscription-reconciler/internal/domain"
	"github.com/example/subscription-reconciler/internal/infrastructure/postgres"
	"github.com/stretchr/testify/require"
)

func TestComputeStateTransitionExpiredGrant(t *testing.T) {
	eventTimeMs := time.Now().AddDate(0, -2, 0).UnixMilli()

	active, expiresAt, reason := (&storeWebhookService{}).computeStateTransition("INITIAL_PURCHASE", eventTimeMs)

	require.False(t, active)
	require.NotNil(t, expiresAt)
	require.True(t, expiresAt.Before(time.Now()))
	require.Equal(t, "EXPIRATION", reason)
}

func TestProcessStoreWebhookRollsBackAndRetriesAfterFailure(t *testing.T) {
	for _, failAt := range []string{"upsert", "schedule", "mark_processed"} {
		t.Run(failAt, func(t *testing.T) {
			db := newFakeStoreWebhookDatabase()
			db.failAt = failAt
			service := NewStoreWebhookService(db)
			payload := validStoreWebhook("retry_"+failAt, "retry_user", "INITIAL_PURCHASE", time.Now().UnixMilli())

			response, err := service.ProcessStoreWebhook(context.Background(), payload)
			require.Error(t, err)
			require.Nil(t, response)
			state := db.snapshot()
			require.Empty(t, state.events)
			require.Empty(t, state.processed)
			require.False(t, state.hasEntitlement)
			require.Empty(t, state.notifications)

			db.failAt = ""
			response, err = service.ProcessStoreWebhook(context.Background(), payload)
			require.NoError(t, err)
			require.True(t, response.Accepted)
			require.False(t, response.IsDuplicate)
			state = db.snapshot()
			require.Contains(t, state.events, payload.EventID)
			require.Contains(t, state.processed, payload.EventID)
			require.True(t, state.hasEntitlement)
			require.True(t, state.active)
			require.Len(t, state.notifications, 1)
		})
	}
}

func TestProcessStoreWebhookLateArrivalDoesNotScheduleNotification(t *testing.T) {
	db := newFakeStoreWebhookDatabase()
	service := NewStoreWebhookService(db)
	now := time.Now().UnixMilli()

	_, err := service.ProcessStoreWebhook(context.Background(), validStoreWebhook("new_cancel", "ordered_user", "CANCELLATION", now-60_000))
	require.NoError(t, err)
	_, err = service.ProcessStoreWebhook(context.Background(), validStoreWebhook("late_purchase", "ordered_user", "INITIAL_PURCHASE", now-120_000))
	require.NoError(t, err)

	state := db.snapshot()
	require.False(t, state.active)
	require.Equal(t, now-60_000, state.lastEventTime)
	require.Empty(t, state.notifications)
	require.Contains(t, state.processed, "late_purchase")
}

func TestProcessStoreWebhookSchedulesOnlyForActiveStateTransitions(t *testing.T) {
	db := newFakeStoreWebhookDatabase()
	service := NewStoreWebhookService(db)
	now := time.Now().UnixMilli()

	_, err := service.ProcessStoreWebhook(context.Background(), validStoreWebhook("purchase", "notify_user", "INITIAL_PURCHASE", now))
	require.NoError(t, err)
	_, err = service.ProcessStoreWebhook(context.Background(), validStoreWebhook("cancel", "notify_user", "CANCELLATION", now+1_000))
	require.NoError(t, err)
	_, err = service.ProcessStoreWebhook(context.Background(), validStoreWebhook("renewal", "notify_user", "RENEWAL", now+2_000))
	require.NoError(t, err)

	state := db.snapshot()
	require.True(t, state.active)
	require.Len(t, state.notifications, 2)
}

func validStoreWebhook(eventID string, userID string, eventType string, eventTimeMs int64) *domain.StoreWebhookPayload {
	return &domain.StoreWebhookPayload{
		EventID:     eventID,
		UserID:      userID,
		Type:        eventType,
		EventTimeMs: eventTimeMs,
		ProductID:   "premium_1_month",
	}
}

type fakeStoreWebhookDatabase struct {
	mu     sync.Mutex
	state  fakeStoreWebhookState
	failAt string
}

type fakeStoreWebhookState struct {
	events         map[string]struct{}
	processed      map[string]struct{}
	hasEntitlement bool
	active         bool
	expiresAt      *time.Time
	lastEventTime  int64
	notifications  []time.Time
}

func newFakeStoreWebhookDatabase() *fakeStoreWebhookDatabase {
	return &fakeStoreWebhookDatabase{state: fakeStoreWebhookState{
		events:    make(map[string]struct{}),
		processed: make(map[string]struct{}),
	}}
}

func (db *fakeStoreWebhookDatabase) WithStoreWebhookTransaction(_ context.Context, fn func(postgres.StoreWebhookTransaction) error) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	working := db.state.clone()
	tx := &fakeStoreWebhookTransaction{state: &working, failAt: db.failAt}
	if err := fn(tx); err != nil {
		return err
	}
	db.state = working
	return nil
}

func (db *fakeStoreWebhookDatabase) snapshot() fakeStoreWebhookState {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.state.clone()
}

func (state fakeStoreWebhookState) clone() fakeStoreWebhookState {
	copy := fakeStoreWebhookState{
		events:         make(map[string]struct{}, len(state.events)),
		processed:      make(map[string]struct{}, len(state.processed)),
		hasEntitlement: state.hasEntitlement,
		active:         state.active,
		lastEventTime:  state.lastEventTime,
		notifications:  append([]time.Time(nil), state.notifications...),
	}
	for eventID := range state.events {
		copy.events[eventID] = struct{}{}
	}
	for eventID := range state.processed {
		copy.processed[eventID] = struct{}{}
	}
	if state.expiresAt != nil {
		expiry := *state.expiresAt
		copy.expiresAt = &expiry
	}
	return copy
}

type fakeStoreWebhookTransaction struct {
	state  *fakeStoreWebhookState
	failAt string
}

func (tx *fakeStoreWebhookTransaction) fail(step string) error {
	if tx.failAt == step {
		return fmt.Errorf("injected %s failure", step)
	}
	return nil
}

func (tx *fakeStoreWebhookTransaction) InsertStoreEvent(_ context.Context, eventID string, _ string, _ string, _ int64, _ *string) (bool, error) {
	if err := tx.fail("insert"); err != nil {
		return false, err
	}
	if _, exists := tx.state.events[eventID]; exists {
		return false, nil
	}
	tx.state.events[eventID] = struct{}{}
	return true, nil
}

func (tx *fakeStoreWebhookTransaction) UpsertEntitlement(_ context.Context, _ string, _ string, active bool, expiresAt *time.Time, _ *string, lastEventTime int64, _ *string) (bool, error) {
	if err := tx.fail("upsert"); err != nil {
		return false, err
	}
	if tx.state.hasEntitlement && lastEventTime <= tx.state.lastEventTime {
		return false, nil
	}
	stateChanged := !tx.state.hasEntitlement || tx.state.active != active || !sameTestExpiration(tx.state.expiresAt, expiresAt)
	tx.state.hasEntitlement = true
	tx.state.active = active
	tx.state.lastEventTime = lastEventTime
	tx.state.expiresAt = nil
	if expiresAt != nil {
		expiry := *expiresAt
		tx.state.expiresAt = &expiry
	}
	return stateChanged, nil
}

func (tx *fakeStoreWebhookTransaction) ScheduleNotification(_ context.Context, _ string, _ string, scheduledFor time.Time) error {
	if err := tx.fail("schedule"); err != nil {
		return err
	}
	tx.state.notifications = append(tx.state.notifications, scheduledFor)
	return nil
}

func (tx *fakeStoreWebhookTransaction) MarkEventProcessed(_ context.Context, eventID string, _ string) error {
	if err := tx.fail("mark_processed"); err != nil {
		return err
	}
	tx.state.processed[eventID] = struct{}{}
	return nil
}

func sameTestExpiration(left *time.Time, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}
