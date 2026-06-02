package tests

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/example/adora/internal/infrastructure/postgres"
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

// TestDuplicateStoreEvents tests idempotency of store webhook processing.
func TestDuplicateStoreEvents(t *testing.T) {
	t.Skip("TODO: Implement duplicate event handling test")
}

// TestOutOfOrderStoreEvents tests event ordering by timestamp.
func TestOutOfOrderStoreEvents(t *testing.T) {
	t.Skip("TODO: Implement out-of-order event handling test")
}

// TestLateArrivingStoreEvents tests late arrivals don't overwrite newer state.
func TestLateArrivingStoreEvents(t *testing.T) {
	t.Skip("TODO: Implement late-arriving event handling test")
}

// TestMarketplaceIsolation tests marketplace revoke only affects MARKETPLACE source.
func TestMarketplaceIsolation(t *testing.T) {
	t.Skip("TODO: Implement marketplace isolation test")
}

// TestCarrierInactiveHandling tests carrier inactive status.
func TestCarrierInactiveHandling(t *testing.T) {
	t.Skip("TODO: Implement carrier inactive handling test")
}

// TestCarrierAPIErrorHandling tests carrier API errors don't revoke access.
func TestCarrierAPIErrorHandling(t *testing.T) {
	t.Skip("TODO: Implement carrier API error handling test")
}

// TestNotificationDeduplication tests at most one notification per user per day.
func TestNotificationDeduplication(t *testing.T) {
	t.Skip("TODO: Implement notification deduplication test")
}

// TestConcurrentCarrierWorkers tests multiple workers don't double-poll users.
func TestConcurrentCarrierWorkers(t *testing.T) {
	t.Skip("TODO: Implement concurrent worker test")
}

// Helper function for creating test data
func createTestEntitlement(t *testing.T, db postgres.Database, ctx context.Context, userID string, source string, active bool) {
	t.Helper()
	err := db.UpsertEntitlement(ctx, userID, source, active, nil, nil, time.Now().UnixMilli())
	require.NoError(t, err)
}
