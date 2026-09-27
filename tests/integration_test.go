package tests

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/example/subscription-reconciler/internal/application"
	"github.com/example/subscription-reconciler/internal/domain"
	"github.com/example/subscription-reconciler/internal/infrastructure/postgres"
	workerinfra "github.com/example/subscription-reconciler/internal/infrastructure/worker"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// PostgresContainer manages a Testcontainers PostgreSQL instance.
type PostgresContainer struct {
	container testcontainers.Container
	dsn       string
}

// SetupPostgres starts a PostgreSQL container for testing.
func SetupPostgres(ctx context.Context) (*PostgresContainer, error) {
	req := testcontainers.ContainerRequest{
		Image:        "postgres:18.4-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "postgres",
			"POSTGRES_PASSWORD": "postgres",
			"POSTGRES_DB":       "subscription_reconciler_test",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(30 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, err
	}

	host, err := container.Host(ctx)
	if err != nil {
		container.Terminate(ctx)
		return nil, err
	}

	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		container.Terminate(ctx)
		return nil, err
	}

	dsn := fmt.Sprintf("postgres://postgres:postgres@%s:%s/subscription_reconciler_test?sslmode=disable", host, port.Port())

	return &PostgresContainer{
		container: container,
		dsn:       dsn,
	}, nil
}

// Connect connects to the PostgreSQL container and returns a database client.
func (pc *PostgresContainer) Connect(ctx context.Context) (*postgres.Client, error) {
	return postgres.Connect(ctx, pc.dsn)
}

// Terminate stops the PostgreSQL container.
func (pc *PostgresContainer) Terminate(ctx context.Context) error {
	if pc.container != nil {
		return pc.container.Terminate(ctx)
	}
	return nil
}

var (
	testDB      *postgres.Client
	pgContainer *PostgresContainer
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	pgContainer, err = SetupPostgres(ctx)
	if err != nil {
		panic(fmt.Sprintf("Failed to setup postgres container: %v", err))
	}

	testDB, err = pgContainer.Connect(ctx)
	if err != nil {
		pgContainer.Terminate(ctx)
		panic(fmt.Sprintf("Failed to connect to test db: %v", err))
	}

	// Run migrations
	dbConn, err := sql.Open("pgx", pgContainer.dsn)
	if err != nil {
		pgContainer.Terminate(ctx)
		panic(fmt.Sprintf("Failed to open raw connection for migrations: %v", err))
	}
	defer dbConn.Close()

	migrationFiles := []string{
		"../migrations/001_initial_schema.up.sql",
		"../migrations/002_add_indexes.up.sql",
		"../migrations/003_add_carrier_polling.up.sql",
		"../migrations/004_fix_expires_at_constraint.up.sql",
		"../migrations/005_fix_user_entitlements_check_constraint.up.sql",
	}

	for _, file := range migrationFiles {
		content, err := os.ReadFile(file)
		if err != nil {
			pgContainer.Terminate(ctx)
			panic(fmt.Sprintf("Failed to read migration file %s: %v", file, err))
		}
		_, err = dbConn.Exec(string(content))
		if err != nil {
			pgContainer.Terminate(ctx)
			panic(fmt.Sprintf("Failed to execute migration %s: %v", file, err))
		}
	}

	code := m.Run()

	testDB.Close()
	pgContainer.Terminate(ctx)
	os.Exit(code)
}

// cleanDB truncates all database tables to isolate test scenarios.
func cleanDB(t *testing.T) {
	t.Helper()
	dbConn, err := sql.Open("pgx", pgContainer.dsn)
	require.NoError(t, err)
	defer dbConn.Close()

	_, err = dbConn.Exec("TRUNCATE TABLE processed_events, store_events, marketplace_revocations, user_entitlements, notifications, audit_logs CASCADE")
	require.NoError(t, err)
}

// TestDuplicateStoreEvents tests idempotency of store webhook processing.
func TestDuplicateStoreEvents(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	service := application.NewStoreWebhookService(testDB)

	payload := &domain.StoreWebhookPayload{
		EventID:     "event_dup_1",
		UserID:      "user_dup_1",
		Type:        "INITIAL_PURCHASE",
		EventTimeMs: time.Now().UnixMilli(),
		ProductID:   "premium_1_month",
	}

	// First time: accept and process
	resp, err := service.ProcessStoreWebhook(ctx, payload)
	require.NoError(t, err)
	require.True(t, resp.Accepted)
	require.False(t, resp.IsDuplicate)

	// Second time: duplicate
	resp2, err := service.ProcessStoreWebhook(ctx, payload)
	require.NoError(t, err)
	require.True(t, resp2.Accepted)
	require.True(t, resp2.IsDuplicate)
}

// TestOutOfOrderStoreEvents tests event ordering by timestamp.
func TestOutOfOrderStoreEvents(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	service := application.NewStoreWebhookService(testDB)
	queryService := application.NewEntitlementQueryService(testDB)

	userID := "user_ooo_1"
	now := time.Now().UnixMilli()

	// 1. Initial purchase at t=now-20s
	_, err := service.ProcessStoreWebhook(ctx, &domain.StoreWebhookPayload{
		EventID:     "event_ooo_1",
		UserID:      userID,
		Type:        "INITIAL_PURCHASE",
		EventTimeMs: now - 20000,
		ProductID:   "premium_1_month",
	})
	require.NoError(t, err)

	// 2. Renewal at t=now-10s
	_, err = service.ProcessStoreWebhook(ctx, &domain.StoreWebhookPayload{
		EventID:     "event_ooo_2",
		UserID:      userID,
		Type:        "RENEWAL",
		EventTimeMs: now - 10000,
		ProductID:   "premium_1_month",
	})
	require.NoError(t, err)

	// 3. Billing issue at t=now-15s (out of order, older than RENEWAL)
	_, err = service.ProcessStoreWebhook(ctx, &domain.StoreWebhookPayload{
		EventID:     "event_ooo_3",
		UserID:      userID,
		Type:        "BILLING_ISSUE",
		EventTimeMs: now - 15000,
		ProductID:   "premium_1_month",
	})
	require.NoError(t, err)

	// Verify entitlement is still active (from RENEWAL at now-10s), because BILLING_ISSUE at now-15s is older and ignored.
	ent, err := queryService.GetCanonicalEntitlement(ctx, userID)
	require.NoError(t, err)
	require.NotNil(t, ent)
	require.True(t, ent.Active)
	require.Equal(t, "STORE", ent.Source)
}

// TestLateArrivingStoreEvents tests late arrivals don't overwrite newer state.
func TestLateArrivingStoreEvents(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	service := application.NewStoreWebhookService(testDB)
	queryService := application.NewEntitlementQueryService(testDB)

	userID := "user_late_1"
	now := time.Now().UnixMilli()

	// 1. Cancel at t=now-10s
	_, err := service.ProcessStoreWebhook(ctx, &domain.StoreWebhookPayload{
		EventID:     "event_late_1",
		UserID:      userID,
		Type:        "CANCELLATION",
		EventTimeMs: now - 10000,
		ProductID:   "premium_1_month",
	})
	require.NoError(t, err)

	// Verify inactive
	ent, err := queryService.GetCanonicalEntitlement(ctx, userID)
	require.NoError(t, err)
	require.False(t, ent.Active)

	// 2. Late initial purchase at t=now-20s arrives
	_, err = service.ProcessStoreWebhook(ctx, &domain.StoreWebhookPayload{
		EventID:     "event_late_2",
		UserID:      userID,
		Type:        "INITIAL_PURCHASE",
		EventTimeMs: now - 20000,
		ProductID:   "premium_1_month",
	})
	require.NoError(t, err)

	// Verify it remains inactive
	ent, err = queryService.GetCanonicalEntitlement(ctx, userID)
	require.NoError(t, err)
	require.False(t, ent.Active)
}

func TestConcurrentStoreWebhooksForSameUser(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	service := application.NewStoreWebhookService(testDB)
	const userID = "user_concurrent_store_webhooks"
	now := time.Now().UnixMilli()
	events := make([]*domain.StoreWebhookPayload, 8)
	for i := range events {
		eventType := "RENEWAL"
		if i%2 == 0 {
			eventType = "CANCELLATION"
		}
		events[i] = &domain.StoreWebhookPayload{
			EventID:     fmt.Sprintf("event_concurrent_%02d", i),
			UserID:      userID,
			Type:        eventType,
			EventTimeMs: now - int64(len(events)-i)*60_000,
			ProductID:   "premium_1_month",
		}
	}
	newest := events[len(events)-1]
	start := make(chan struct{})
	type result struct {
		response *domain.StoreWebhookResponse
		err      error
	}
	results := make(chan result, len(events))
	for _, event := range events {
		go func(payload *domain.StoreWebhookPayload) {
			<-start
			response, err := service.ProcessStoreWebhook(ctx, payload)
			results <- result{response: response, err: err}
		}(event)
	}
	close(start)
	for range events {
		outcome := <-results
		require.NoError(t, outcome.err)
		require.NotNil(t, outcome.response)
		require.True(t, outcome.response.Accepted)
		require.False(t, outcome.response.IsDuplicate)
	}

	entitlement, err := testDB.GetEntitlementByUserAndSource(ctx, userID, "STORE")
	require.NoError(t, err)
	require.NotNil(t, entitlement)
	require.True(t, entitlement.Active)
	require.Equal(t, newest.EventTimeMs, entitlement.LastEventTime)
	expectedExpiry := time.UnixMilli(newest.EventTimeMs).AddDate(0, 1, 0)
	require.True(t, entitlement.ExpiresAt.Equal(expectedExpiry))

	storeEvents, err := testDB.GetStoreEventsByUser(ctx, userID)
	require.NoError(t, err)
	require.Len(t, storeEvents, len(events))

	auditLogs, err := testDB.GetAuditLogsByUser(ctx, userID, int32(len(events)+1), 0)
	require.NoError(t, err)
	require.NotEmpty(t, auditLogs)
	require.LessOrEqual(t, len(auditLogs), len(events))
	sort.Slice(auditLogs, func(i, j int) bool { return auditLogs[i].ID < auditLogs[j].ID })
	eventTimes := make(map[string]int64, len(events))
	for _, event := range events {
		eventTimes[event.EventID] = event.EventTimeMs
	}
	var previousEventTime int64
	for i, audit := range auditLogs {
		require.NotNil(t, audit.TriggeringEventID)
		eventTime, exists := eventTimes[*audit.TriggeringEventID]
		require.True(t, exists, "unexpected audit event %q", *audit.TriggeringEventID)
		if i > 0 {
			require.Greater(t, eventTime, previousEventTime, "audit transitions must follow increasing event time")
		}
		previousEventTime = eventTime
	}
	require.Equal(t, newest.EventID, *auditLogs[len(auditLogs)-1].TriggeringEventID)
}

func TestStoreWebhookAtomicityAndRetry(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	const eventID = "event_atomic_retry"
	const userID = "user_atomic_retry"
	dbConn, err := sql.Open("pgx", pgContainer.dsn)
	require.NoError(t, err)
	defer dbConn.Close()
	defer func() {
		_, _ = dbConn.Exec("DROP TRIGGER IF EXISTS fail_store_webhook_processed_test ON processed_events")
		_, _ = dbConn.Exec("DROP FUNCTION IF EXISTS fail_store_webhook_processed_test()")
	}()

	_, err = dbConn.Exec(`
		CREATE OR REPLACE FUNCTION fail_store_webhook_processed_test() RETURNS trigger AS $$
		BEGIN
			IF NEW.event_id = 'event_atomic_retry' THEN
				RAISE EXCEPTION 'injected processed-event failure';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql
	`)
	require.NoError(t, err)
	_, err = dbConn.Exec(`
		CREATE TRIGGER fail_store_webhook_processed_test
		BEFORE INSERT ON processed_events
		FOR EACH ROW EXECUTE FUNCTION fail_store_webhook_processed_test()
	`)
	require.NoError(t, err)

	payload := &domain.StoreWebhookPayload{
		EventID:     eventID,
		UserID:      userID,
		Type:        "INITIAL_PURCHASE",
		EventTimeMs: time.Now().Add(-time.Hour).UnixMilli(),
		ProductID:   "premium_1_month",
	}
	service := application.NewStoreWebhookService(testDB)
	response, err := service.ProcessStoreWebhook(ctx, payload)
	require.Error(t, err)
	require.Nil(t, response)

	storeEvents, err := testDB.GetStoreEventsByUser(ctx, userID)
	require.NoError(t, err)
	require.Empty(t, storeEvents)
	_, err = testDB.GetEntitlementByUserAndSource(ctx, userID, "STORE")
	require.ErrorIs(t, err, sql.ErrNoRows)
	processed, err := testDB.IsEventProcessed(ctx, eventID, "STORE")
	require.NoError(t, err)
	require.False(t, processed)
	auditCount, err := testDB.CountAuditLogsByUser(ctx, userID)
	require.NoError(t, err)
	require.Zero(t, auditCount)
	var notificationCount int
	err = dbConn.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE user_id = $1`, userID).Scan(&notificationCount)
	require.NoError(t, err)
	require.Zero(t, notificationCount)

	_, err = dbConn.Exec("DROP TRIGGER fail_store_webhook_processed_test ON processed_events")
	require.NoError(t, err)
	_, err = dbConn.Exec("DROP FUNCTION fail_store_webhook_processed_test()")
	require.NoError(t, err)

	response, err = service.ProcessStoreWebhook(ctx, payload)
	require.NoError(t, err)
	require.True(t, response.Accepted)
	require.False(t, response.IsDuplicate)
	storeEvents, err = testDB.GetStoreEventsByUser(ctx, userID)
	require.NoError(t, err)
	require.Len(t, storeEvents, 1)
	entitlement, err := testDB.GetEntitlementByUserAndSource(ctx, userID, "STORE")
	require.NoError(t, err)
	require.True(t, entitlement.Active)
	processed, err = testDB.IsEventProcessed(ctx, eventID, "STORE")
	require.NoError(t, err)
	require.True(t, processed)
	auditCount, err = testDB.CountAuditLogsByUser(ctx, userID)
	require.NoError(t, err)
	require.EqualValues(t, 1, auditCount)
	err = dbConn.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE user_id = $1`, userID).Scan(&notificationCount)
	require.NoError(t, err)
	require.Equal(t, 1, notificationCount)
}

// TestEntitlementExpirationFallback verifies that an expired high-priority
// source does not hide a currently active lower-priority source.
func TestEntitlementExpirationFallback(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	queryService := application.NewEntitlementQueryService(testDB)
	userID := "user_expiration_fallback"

	expiredAt := time.Now().Add(-time.Hour)
	reason := string(domain.EventTypeRenewal)
	_, err := testDB.UpsertEntitlement(ctx, userID, "STORE", true, &expiredAt, &reason, expiredAt.Add(-time.Hour).UnixMilli(), nil)
	require.NoError(t, err)

	_, err = testDB.UpsertEntitlement(ctx, userID, "CARRIER", true, nil, nil, time.Now().UnixMilli(), nil)
	require.NoError(t, err)

	entitlement, err := queryService.GetCanonicalEntitlement(ctx, userID)
	require.NoError(t, err)
	require.True(t, entitlement.Active)
	require.Equal(t, "CARRIER", entitlement.Source)
}

// TestExpirationWorkerReconciliation verifies expiration projection updates,
// audit details, and renewal ordering after the worker has run.
func TestExpirationWorkerReconciliation(t *testing.T) {
	cleanDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	userID := "user_expiration_worker"

	expiredAt := time.Now().Add(-time.Hour).Truncate(time.Millisecond)
	reason := string(domain.EventTypeRenewal)
	_, err := testDB.UpsertEntitlement(ctx, userID, "STORE", true, &expiredAt, &reason, expiredAt.Add(-time.Hour).UnixMilli(), nil)
	require.NoError(t, err)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	expirationWorker := workerinfra.NewExpirationWorker(testDB, logger)
	done := make(chan struct{})
	go func() {
		expirationWorker.StartExpirationReconciliation(ctx)
		close(done)
	}()

	require.Eventually(t, func() bool {
		entitlement, err := testDB.GetEntitlementByUserAndSource(ctx, userID, "STORE")
		return err == nil && entitlement != nil && !entitlement.Active
	}, 5*time.Second, 10*time.Millisecond)

	entitlement, err := testDB.GetEntitlementByUserAndSource(ctx, userID, "STORE")
	require.NoError(t, err)
	require.Equal(t, expiredAt.UnixMilli(), entitlement.LastEventTime)

	renewalEventTime := expiredAt.Add(time.Minute)
	_, err = application.NewStoreWebhookService(testDB).ProcessStoreWebhook(ctx, &domain.StoreWebhookPayload{
		EventID:     "event_expiration_renewal",
		UserID:      userID,
		Type:        string(domain.EventTypeRenewal),
		EventTimeMs: renewalEventTime.UnixMilli(),
		ProductID:   "premium_1_month",
	})
	require.NoError(t, err)

	entitlement, err = testDB.GetEntitlementByUserAndSource(ctx, userID, "STORE")
	require.NoError(t, err)
	require.True(t, entitlement.Active)
	require.Equal(t, renewalEventTime.UnixMilli(), entitlement.LastEventTime)

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("expiration worker did not stop after context cancellation")
	}

	auditLogs, err := testDB.GetAuditLogsByUser(context.Background(), userID, 100, 0)
	require.NoError(t, err)
	var expirationLog *postgres.AuditLog
	for i := range auditLogs {
		if auditLogs[i].Reason != nil && *auditLogs[i].Reason == "EXPIRATION" {
			expirationLog = &auditLogs[i]
			break
		}
	}
	require.NotNil(t, expirationLog)
	require.NotNil(t, expirationLog.PreviousActive)
	require.True(t, *expirationLog.PreviousActive)
	require.False(t, expirationLog.NextActive)
}

// TestConcurrentExpirationWorkers verifies that concurrent workers claim each
// expired row once and do not duplicate its audit transition.
func TestConcurrentExpirationWorkers(t *testing.T) {
	cleanDB(t)
	setupCtx := context.Background()
	const userCount = 20
	for i := range userCount {
		userID := fmt.Sprintf("user_expiration_concurrent_%d", i)
		expiredAt := time.Now().Add(-time.Hour).Truncate(time.Millisecond)
		reason := string(domain.EventTypeRenewal)
		_, err := testDB.UpsertEntitlement(setupCtx, userID, "STORE", true, &expiredAt, &reason, expiredAt.Add(-time.Hour).UnixMilli(), nil)
		require.NoError(t, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	workers := []*workerinfra.ExpirationWorker{
		workerinfra.NewExpirationWorker(testDB, logger),
		workerinfra.NewExpirationWorker(testDB, logger),
	}
	done := make(chan struct{}, len(workers))
	for _, expirationWorker := range workers {
		go func() {
			expirationWorker.StartExpirationReconciliation(ctx)
			done <- struct{}{}
		}()
	}

	require.Eventually(t, func() bool {
		for i := range userCount {
			userID := fmt.Sprintf("user_expiration_concurrent_%d", i)
			entitlement, err := testDB.GetEntitlementByUserAndSource(ctx, userID, "STORE")
			if err != nil || entitlement == nil || entitlement.Active {
				return false
			}
		}
		return true
	}, 5*time.Second, 10*time.Millisecond)

	cancel()
	for range workers {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("expiration worker did not stop after context cancellation")
		}
	}

	for i := range userCount {
		userID := fmt.Sprintf("user_expiration_concurrent_%d", i)
		auditLogs, err := testDB.GetAuditLogsByUser(context.Background(), userID, 100, 0)
		require.NoError(t, err)
		expirationCount := 0
		for _, auditLog := range auditLogs {
			if auditLog.Reason != nil && *auditLog.Reason == "EXPIRATION" {
				expirationCount++
				require.NotNil(t, auditLog.PreviousActive)
				require.True(t, *auditLog.PreviousActive)
				require.False(t, auditLog.NextActive)
			}
		}
		require.Equal(t, 1, expirationCount, userID)
	}
}

// TestMarketplaceIsolation tests marketplace revoke only affects MARKETPLACE source.
func TestMarketplaceIsolation(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	storeService := application.NewStoreWebhookService(testDB)
	marketplaceService := application.NewMarketplaceRevokeService(testDB)
	queryService := application.NewEntitlementQueryService(testDB)

	userID := "user_iso_1"
	now := time.Now().UnixMilli()

	// 1. Grant STORE entitlement (active)
	_, err := storeService.ProcessStoreWebhook(ctx, &domain.StoreWebhookPayload{
		EventID:     "event_iso_store",
		UserID:      userID,
		Type:        "INITIAL_PURCHASE",
		EventTimeMs: now,
		ProductID:   "premium_1_month",
	})
	require.NoError(t, err)

	// 2. Grant MARKETPLACE entitlement (active)
	_, err = testDB.UpsertEntitlement(ctx, userID, "MARKETPLACE", true, nil, nil, now, nil)
	require.NoError(t, err)

	// 3. Revoke MARKETPLACE entitlement
	_, err = marketplaceService.RevokeMarketplaceAccess(ctx, &domain.MarketplaceRevokeRequest{
		UserIDs: []string{userID},
	})
	require.NoError(t, err)

	// Verify user still active (STORE is active)
	ent, err := queryService.GetCanonicalEntitlement(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, "STORE", ent.Source)
	require.True(t, ent.Active)

	// Verify MARKETPLACE itself is inactive
	marketplaceEnt, err := testDB.GetEntitlementByUserAndSource(ctx, userID, "MARKETPLACE")
	require.NoError(t, err)
	require.NotNil(t, marketplaceEnt)
	require.False(t, marketplaceEnt.Active)
}

type mockCarrierClient struct {
	mu        sync.Mutex
	statusMap map[string]domain.CarrierPlanStatus
	errMap    map[string]error
	polled    map[string]int
	delay     time.Duration
}

func (m *mockCarrierClient) GetPlanStatus(ctx context.Context, userID string) (domain.CarrierPlanStatus, error) {
	m.mu.Lock()
	if m.polled == nil {
		m.polled = make(map[string]int)
	}
	m.polled[userID]++
	err := m.errMap[userID]
	status, hasStatus := m.statusMap[userID]
	delay := m.delay
	m.mu.Unlock()

	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return domain.CarrierStatusAPIError, ctx.Err()
		case <-timer.C:
		}
	}
	if err != nil {
		return domain.CarrierStatusAPIError, err
	}
	if hasStatus {
		return status, nil
	}
	return domain.CarrierStatusActive, nil
}

func (m *mockCarrierClient) getPolledCount(userID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.polled[userID]
}

func (m *mockCarrierClient) totalPolled() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := 0
	for _, count := range m.polled {
		total += count
	}
	return total
}

// TestCarrierInactiveHandling tests carrier inactive status.
func TestCarrierInactiveHandling(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	userID := "user_carrier_inactive"
	now := time.Now().UnixMilli()

	// 1. Setup active carrier entitlement
	_, err := testDB.UpsertEntitlement(ctx, userID, "CARRIER", true, nil, nil, now, nil)
	require.NoError(t, err)

	// 2. Setup mock client to return inactive
	mockClient := &mockCarrierClient{
		statusMap: map[string]domain.CarrierPlanStatus{
			userID: domain.CarrierStatusInactive,
		},
	}

	service := application.NewCarrierPollingService(testDB, mockClient)

	// 3. Poll
	processed, err := service.PollCarriersForUsers(ctx, 10, time.Now().Add(-5*time.Minute))
	require.NoError(t, err)
	require.Equal(t, int32(1), processed)

	// 4. Verify entitlement is inactive
	ent, err := testDB.GetEntitlementByUserAndSource(ctx, userID, "CARRIER")
	require.NoError(t, err)
	require.NotNil(t, ent)
	require.False(t, ent.Active)
}

// TestCarrierAPIErrorHandling tests carrier API errors don't revoke access.
func TestCarrierAPIErrorHandling(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	userID := "user_carrier_err"
	now := time.Now().UnixMilli()

	// 1. Setup active carrier entitlement
	_, err := testDB.UpsertEntitlement(ctx, userID, "CARRIER", true, nil, nil, now, nil)
	require.NoError(t, err)

	// 2. Setup mock client to return error
	mockClient := &mockCarrierClient{
		errMap: map[string]error{
			userID: fmt.Errorf("carrier api down"),
		},
	}

	service := application.NewCarrierPollingService(testDB, mockClient)

	// 3. Poll; failed requests count as attempts but skip entitlement updates.
	processed, err := service.PollCarriersForUsers(ctx, 10, time.Now().Add(-5*time.Minute))
	require.NoError(t, err)
	require.Equal(t, int32(1), processed)

	// 4. Verify entitlement remains active
	ent, err := testDB.GetEntitlementByUserAndSource(ctx, userID, "CARRIER")
	require.NoError(t, err)
	require.NotNil(t, ent)
	require.True(t, ent.Active)
}

// TestNotificationDeduplication tests at most one notification per user per day.
func TestNotificationDeduplication(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	userID := "user_notify_dedup"
	now := time.Now()

	// 1. Schedule first notification
	err := testDB.ScheduleNotification(ctx, userID, "PREMIUM_EXPIRES_SOON", now)
	require.NoError(t, err)

	// 2. Schedule second notification on same day (should trigger duplicate key/do nothing)
	err = testDB.ScheduleNotification(ctx, userID, "PREMIUM_EXPIRES_SOON", now.Add(1*time.Hour))
	require.NoError(t, err)

	// Verify only 1 notification is in the database for that user
	notifications, err := testDB.GetDueNotifications(ctx, 10)
	require.NoError(t, err)

	count := 0
	for _, n := range notifications {
		if n.UserID == userID {
			count++
		}
	}
	require.Equal(t, 1, count)
}

type notificationMetricsHandler struct {
	mu      sync.Mutex
	claimed int32
}

func (h *notificationMetricsHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (h *notificationMetricsHandler) Handle(_ context.Context, record slog.Record) error {
	if record.Message != "notification sending cycle completed" {
		return nil
	}

	var claimed int32
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key == "count" {
			claimed = slogInt32(attr.Value)
		}
		return true
	})

	h.mu.Lock()
	h.claimed += claimed
	h.mu.Unlock()
	return nil
}

func (h *notificationMetricsHandler) WithAttrs([]slog.Attr) slog.Handler {
	return h
}

func (h *notificationMetricsHandler) WithGroup(string) slog.Handler {
	return h
}

func (h *notificationMetricsHandler) totalClaimed() int32 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.claimed
}

func slogInt32(value slog.Value) int32 {
	switch value.Kind() {
	case slog.KindInt64:
		return int32(value.Int64())
	case slog.KindUint64:
		return int32(value.Uint64())
	case slog.KindAny:
		switch number := value.Any().(type) {
		case int:
			return int32(number)
		case int32:
			return number
		case int64:
			return int32(number)
		}
	}
	return 0
}

func insertDueNotifications(t *testing.T, ctx context.Context, count int, userPrefix string) {
	t.Helper()
	dbConn, err := sql.Open("pgx", pgContainer.dsn)
	require.NoError(t, err)
	defer dbConn.Close()

	_, err = dbConn.ExecContext(ctx, `
		INSERT INTO notifications (user_id, type, scheduled_for)
		SELECT $1 || i::text, $2, NOW() - INTERVAL '1 minute'
		FROM generate_series(1, $3::int) AS series(i)
	`, userPrefix, "PREMIUM_EXPIRES_SOON", count)
	require.NoError(t, err)
}

func notificationCounts(ctx context.Context) (total int, sent int, err error) {
	dbConn, err := sql.Open("pgx", pgContainer.dsn)
	if err != nil {
		return 0, 0, err
	}
	defer dbConn.Close()

	err = dbConn.QueryRowContext(ctx, `
		SELECT COUNT(*)::int, COUNT(*) FILTER (WHERE sent_at IS NOT NULL)::int
		FROM notifications
	`).Scan(&total, &sent)
	return total, sent, err
}

// TestConcurrentNotificationWorkers verifies concurrent workers claim each
// due notification once and report exactly the shared backlog size.
func TestConcurrentNotificationWorkers(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	const notificationCount = 64
	const workerCount = 4
	insertDueNotifications(t, ctx, notificationCount, "user_notification_concurrent_")

	metrics := &notificationMetricsHandler{}
	logger := slog.New(metrics)
	workerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{}, workerCount)
	start := make(chan struct{})
	workers := make([]*workerinfra.NotificationWorker, 0, workerCount)
	for range workerCount {
		workers = append(workers, workerinfra.NewNotificationWorker(testDB, logger))
	}
	defer func() {
		cancel()
		for range workerCount {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("notification worker did not stop after context cancellation")
			}
		}
	}()

	for _, notificationWorker := range workers {
		go func(w *workerinfra.NotificationWorker) {
			<-start
			w.StartNotificationSending(workerCtx)
			done <- struct{}{}
		}(notificationWorker)
	}
	close(start)

	require.Eventually(t, func() bool {
		total, sent, err := notificationCounts(ctx)
		return err == nil && total == notificationCount && sent == notificationCount && metrics.totalClaimed() == notificationCount
	}, 10*time.Second, 20*time.Millisecond)
}

// TestNotificationWorkerBacklogDraining verifies a worker drains more than a
// single batch before waiting for its next scheduled interval.
func TestNotificationWorkerBacklogDraining(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	const notificationCount = 2001
	insertDueNotifications(t, ctx, notificationCount, "user_notification_backlog_")

	metrics := &notificationMetricsHandler{}
	logger := slog.New(metrics)
	workerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	worker := workerinfra.NewNotificationWorker(testDB, logger)
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("notification worker did not stop after context cancellation")
		}
	}()

	go func() {
		worker.StartNotificationSending(workerCtx)
		close(done)
	}()

	require.Eventually(t, func() bool {
		total, sent, err := notificationCounts(ctx)
		return err == nil && total == notificationCount && sent == notificationCount && metrics.totalClaimed() == notificationCount
	}, 15*time.Second, 20*time.Millisecond)
}

// TestConcurrentCarrierWorkers verifies carrier worker instances divide a
// shared backlog without polling any user twice.
func TestConcurrentCarrierWorkers(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	const userCount = 205
	const numWorkers = 3
	insertCarrierEntitlements(t, ctx, userCount, "cc_user_")

	mockClient := &mockCarrierClient{delay: time.Millisecond}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	workerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{}, numWorkers)
	start := make(chan struct{})
	workers := make([]*workerinfra.PollingWorker, 0, numWorkers)
	for range numWorkers {
		workers = append(workers, workerinfra.NewPollingWorker(testDB, mockClient, logger))
	}
	defer func() {
		cancel()
		for range numWorkers {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("carrier worker did not stop after context cancellation")
			}
		}
	}()

	for _, pollingWorker := range workers {
		go func(w *workerinfra.PollingWorker) {
			<-start
			w.StartCarrierPolling(workerCtx)
			done <- struct{}{}
		}(pollingWorker)
	}
	close(start)

	require.Eventually(t, func() bool { return mockClient.totalPolled() >= userCount }, 15*time.Second, 20*time.Millisecond)

	for i := range userCount {
		userID := fmt.Sprintf("cc_user_%d", i+1)
		require.Equal(t, 1, mockClient.getPolledCount(userID), "user %s should be polled exactly once", userID)
	}
}

// TestCarrierWorkerBacklogDraining verifies a worker drains multiple complete
// batches and the final partial batch during one startup cycle.
func TestCarrierWorkerBacklogDraining(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	const userCount = 201
	insertCarrierEntitlements(t, ctx, userCount, "carrier_backlog_")

	mockClient := &mockCarrierClient{delay: time.Millisecond}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	workerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	worker := workerinfra.NewPollingWorker(testDB, mockClient, logger)
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("carrier worker did not stop after context cancellation")
		}
	}()

	go func() {
		worker.StartCarrierPolling(workerCtx)
		close(done)
	}()

	require.Eventually(t, func() bool { return mockClient.totalPolled() == userCount }, 15*time.Second, 20*time.Millisecond)
	for i := range userCount {
		userID := fmt.Sprintf("carrier_backlog_%d", i+1)
		require.Equal(t, 1, mockClient.getPolledCount(userID), "user %s should be polled exactly once", userID)
	}
}

func TestCarrierWorkerCancellation(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	const userID = "carrier_cancel_user"
	createTestEntitlement(t, testDB, ctx, userID, "CARRIER", true)
	initialAudits, err := testDB.CountAuditLogsByUser(ctx, userID)
	require.NoError(t, err)

	mockClient := &mockCarrierClient{delay: 30 * time.Second}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	workerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	worker := workerinfra.NewPollingWorker(testDB, mockClient, logger)
	go func() {
		worker.StartCarrierPolling(workerCtx)
		close(done)
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("carrier worker did not stop after context cancellation")
		}
	}()

	require.Eventually(t, func() bool { return mockClient.totalPolled() == 1 }, 5*time.Second, 10*time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("carrier worker did not stop promptly after context cancellation")
	}

	entitlement, err := testDB.GetEntitlementByUserAndSource(ctx, userID, "CARRIER")
	require.NoError(t, err)
	require.NotNil(t, entitlement)
	require.True(t, entitlement.Active)
	finalAudits, err := testDB.CountAuditLogsByUser(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, initialAudits, finalAudits)
}

func insertCarrierEntitlements(t *testing.T, ctx context.Context, count int, userPrefix string) {
	t.Helper()
	dbConn, err := sql.Open("pgx", pgContainer.dsn)
	require.NoError(t, err)
	defer dbConn.Close()

	_, err = dbConn.ExecContext(ctx, `
		INSERT INTO user_entitlements (user_id, source, active)
		SELECT $1 || i::text, 'CARRIER', TRUE
		FROM generate_series(1, $2::int) AS series(i)
	`, userPrefix, count)
	require.NoError(t, err)
}

// Helper function for creating test data
func createTestEntitlement(t *testing.T, db postgres.Database, ctx context.Context, userID string, source string, active bool) {
	t.Helper()
	_, err := db.UpsertEntitlement(ctx, userID, source, active, nil, nil, time.Now().UnixMilli(), nil)
	require.NoError(t, err)
}
