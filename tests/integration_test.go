package tests

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/example/adora/internal/application"
	"github.com/example/adora/internal/domain"
	"github.com/example/adora/internal/infrastructure/postgres"
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
			"POSTGRES_DB":       "adora_test",
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

	dsn := fmt.Sprintf("postgres://postgres:postgres@%s:%s/adora_test?sslmode=disable", host, port.Port())

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
	err = testDB.UpsertEntitlement(ctx, userID, "MARKETPLACE", true, nil, nil, now, nil)
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
}

func (m *mockCarrierClient) GetPlanStatus(userID string) (domain.CarrierPlanStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.polled == nil {
		m.polled = make(map[string]int)
	}
	m.polled[userID]++

	if err, exists := m.errMap[userID]; exists {
		return domain.CarrierStatusAPIError, err
	}
	if status, exists := m.statusMap[userID]; exists {
		return status, nil
	}
	return domain.CarrierStatusActive, nil
}

func (m *mockCarrierClient) getPolledCount(userID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.polled[userID]
}

// TestCarrierInactiveHandling tests carrier inactive status.
func TestCarrierInactiveHandling(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	userID := "user_carrier_inactive"
	now := time.Now().UnixMilli()

	// 1. Setup active carrier entitlement
	err := testDB.UpsertEntitlement(ctx, userID, "CARRIER", true, nil, nil, now, nil)
	require.NoError(t, err)

	// 2. Setup mock client to return inactive
	mockClient := &mockCarrierClient{
		statusMap: map[string]domain.CarrierPlanStatus{
			userID: domain.CarrierStatusInactive,
		},
	}

	service := application.NewCarrierPollingService(testDB, mockClient)

	// 3. Poll
	processed, err := service.PollCarriersForUsers(ctx, 10)
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
	err := testDB.UpsertEntitlement(ctx, userID, "CARRIER", true, nil, nil, now, nil)
	require.NoError(t, err)

	// 2. Setup mock client to return error
	mockClient := &mockCarrierClient{
		errMap: map[string]error{
			userID: fmt.Errorf("carrier api down"),
		},
	}

	service := application.NewCarrierPollingService(testDB, mockClient)

	// 3. Poll (it should not process it as change, but skip updates on API error)
	processed, err := service.PollCarriersForUsers(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, int32(0), processed)

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

// TestConcurrentCarrierWorkers tests multiple workers don't double-poll users.
func TestConcurrentCarrierWorkers(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	// Create 15 carrier users
	userIDs := []string{
		"cc_user_1", "cc_user_2", "cc_user_3", "cc_user_4", "cc_user_5",
		"cc_user_6", "cc_user_7", "cc_user_8", "cc_user_9", "cc_user_10",
		"cc_user_11", "cc_user_12", "cc_user_13", "cc_user_14", "cc_user_15",
	}
	for _, u := range userIDs {
		err := testDB.UpsertEntitlement(ctx, u, "CARRIER", true, nil, nil, now, nil)
		require.NoError(t, err)
	}

	mockClient := &mockCarrierClient{}
	service := application.NewCarrierPollingService(testDB, mockClient)

	// Run multiple polling cycles in concurrent goroutines (each fetches 5 users)
	const numWorkers = 3
	errChan := make(chan error, numWorkers)

	for i := 0; i < numWorkers; i++ {
		go func() {
			_, err := service.PollCarriersForUsers(ctx, 5)
			errChan <- err
		}()
	}

	for i := 0; i < numWorkers; i++ {
		err := <-errChan
		require.NoError(t, err)
	}

	// Verify each user was polled exactly once across all workers
	for _, u := range userIDs {
		require.Equal(t, 1, mockClient.getPolledCount(u), "User %s should have been polled exactly once", u)
	}
}

// Helper function for creating test data
func createTestEntitlement(t *testing.T, db postgres.Database, ctx context.Context, userID string, source string, active bool) {
	t.Helper()
	err := db.UpsertEntitlement(ctx, userID, source, active, nil, nil, time.Now().UnixMilli(), nil)
	require.NoError(t, err)
}
