# SQL Queries for sqlc

This directory contains raw SQL queries configured for consumption by [`sqlc`](https://docs.sqlc.dev/). `sqlc` compiles these queries into highly-optimized, type-safe Go code.

## Query Syntax

Each file follows strict `sqlc` annotation conventions:

```sql
-- name: FunctionName :one
SELECT * FROM table_name WHERE id = $1;
```

**Query Modifiers**:
- `:one` — Expects exactly one row (or throws an error). Standardize queries with `LIMIT 1`.
- `:many` — Returns a slice of rows.
- `:exec` — Executes without returning rows (e.g., `INSERT`, `UPDATE`, `DELETE`).
- `:execrows` — Executes and returns the number of rows affected.

---

## Files Overview

### `entitlements.sql`
Manages the mutable projection representing a user's canonical state:
- `GetEntitlementByUserAndSource`: Resolves a specific state.
- `GetEntitlementsByUser`: Fetch all active sources for multi-source conflict resolution.
- `UpsertEntitlement`: UPSERT the latest state transition.
- `GetCarrierEntitlementsForPolling`: Thread-safe fetch utilizing `FOR UPDATE SKIP LOCKED`.

### `store_events.sql`
Manages the immutable ledger of App Store payloads:
- `InsertStoreEvent`: Append-only persistence.

### `marketplace_revocations.sql`
Manages the immutable ledger of Marketplace bulk revokes:
- `InsertMarketplaceRevocation`: Append-only persistence.

### `processed_events.sql`
Manages the absolute source of truth for idempotency checks:
- `IsEventProcessed`: Validates incoming webhooks.
- `MarkEventProcessed`: Secures idempotency locks post-processing.

### `notifications.sql`
Manages background job scheduling:
- `ScheduleNotification`: Dedupes natively via `ON CONFLICT DO NOTHING`.
- `GetDueNotifications`: Fetches jobs ready for broadcast.

### `audit_logs.sql`
Manages historical timeline generation (Stretch Feature):
- `InsertAuditLog`: Synchronously captures delta changes.
- `GetAuditLogsByUser`: Generates chronologically reversed user timelines.

---

## Code Generation

To regenerate the Go bindings after modifying any `.sql` file, run:

```bash
make sqlc
# Alternatively: sqlc generate
```

This injects generated types and repository methods into `internal/infrastructure/postgres/gen/`.

---

## Advanced Key Patterns

> [!TIP]
> The database strictly offloads data integrity validation from application code to SQL constraints.

### 1. Atomic Idempotency
```sql
INSERT INTO processed_events (event_id, source)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;
```
Ensures that if two identically-identified webhooks arrive synchronously, the database automatically drops the second attempt.

### 2. Thread-Safe Worker Queues (`FOR UPDATE SKIP LOCKED`)
```sql
SELECT * FROM user_entitlements
WHERE source = 'CARRIER'
FOR UPDATE SKIP LOCKED
LIMIT 100;
```
Enables massive concurrency. Instead of workers blocking each other on locked rows, they dynamically skip claimed tasks, processing the queue perfectly in parallel.

### 3. Chronological Truth via Webhook Payloads
```sql
SELECT * FROM store_events
WHERE user_id = $1
ORDER BY event_time_ms DESC;
```
Ordering operations are strictly tied to `event_time_ms` rather than server arrival time, seamlessly preventing late arrivals from overriding newer logical states.
