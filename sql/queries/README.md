# SQL Queries for sqlc

This directory contains SQL queries that are compiled by `sqlc` into type-safe Go code.

## Query Syntax

Each query file follows sqlc conventions:

```sql
-- name: FunctionName :one
SELECT ... WHERE id = $1;
```

Query modifiers:
- `:one` — returns single row or error (query must use `LIMIT 1`)
- `:many` — returns slice of rows or error
- `:exec` — executes without returning rows (INSERT, UPDATE, DELETE)

## Files

### entitlements.sql
Queries for user entitlements (canonical state per source):
- `GetEntitlementByUserAndSource` — fetch single entitlement
- `GetEntitlementsByUser` — fetch all sources for a user
- `UpsertEntitlement` — insert or update entitlement state
- `UpdateEntitlementCarrierPolledAt` — update carrier polling timestamp
- `GetLastEventTimeFromStore` — get ordering timestamp for store events
- `GetEntitlementsExpiringWithin24h` — find expiring entitlements for notifications
- `GetCarrierEntitlementsForPolling` — fetch carrier users with `FOR UPDATE SKIP LOCKED`

### store_events.sql
Queries for store webhook history:
- `InsertStoreEvent` — persist webhook event (immutable)
- `GetStoreEventsByUser` — fetch event history for a user
- `GetStoreEventByID` — fetch event by ID (for debugging)

### marketplace_revocations.sql
Queries for marketplace revoke history:
- `InsertMarketplaceRevocation` — persist revoke operation (immutable)
- `GetMarketplaceRevocationByEventID` — fetch revoke record by ID

### processed_events.sql
Queries for idempotency check:
- `IsEventProcessed` — check if event already processed
- `MarkEventProcessed` — mark event as processed
- `GetProcessedEvent` — fetch processed event record

### notifications.sql
Queries for scheduled notifications:
- `ScheduleNotification` — schedule a notification (deduped by unique constraint)
- `GetDueNotifications` — fetch notifications ready to send
- `MarkNotificationSent` — mark notification as sent
- `GetNotificationByUserTypeAndDate` — check if notification exists

### audit_logs.sql
Queries for audit trail (stretch feature):
- `InsertAuditLog` — record entitlement state change
- `GetAuditLogsByUser` — fetch timeline for a user (paginated)
- `CountAuditLogsByUser` — count total audit entries for a user

## Generating Code

To generate Go code from these queries:

```bash
sqlc generate
```

This creates Go types and methods in `internal/infrastructure/postgres/gen/`.

## Type Mapping

sqlc automatically maps SQL types to Go types:

| SQL Type | Go Type |
|----------|---------|
| TEXT | string |
| BOOLEAN | bool |
| BIGINT | int64 |
| BIGSERIAL | int64 |
| TIMESTAMPTZ | time.Time |
| TIMESTAMP | time.Time |
| NULL | pointer type (e.g., *string, *time.Time) |

## Key Patterns

### Idempotency
```sql
INSERT INTO processed_events (event_id, source)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;
```

Atomic deduplication. Second insert is silently ignored.

### FOR UPDATE SKIP LOCKED
```sql
SELECT ... FROM user_entitlements
WHERE source = 'CARRIER'
FOR UPDATE SKIP LOCKED
LIMIT 100;
```

Concurrent worker coordination. Each worker claims batch of users, others skip locked rows.

### Ordering by Event Time
```sql
SELECT * FROM store_events
WHERE user_id = $1
ORDER BY event_time_ms DESC;
```

Sorted by `event_time_ms` (from webhook), not arrival time. Handles late arrivals correctly.

### COALESCE for Defaults
```sql
SELECT COALESCE(MAX(last_event_time), 0) FROM user_entitlements;
```

Returns 0 if no rows (safer than NULL).

## Testing

Queries are tested in integration tests using Testcontainers PostgreSQL. See `tests/integration_test.go`.

## References

- [sqlc Documentation](https://docs.sqlc.dev/)
- [PostgreSQL Documentation](https://www.postgresql.org/docs/)
