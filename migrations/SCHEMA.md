# Database Schema Documentation

## Overview

The schema implements a hybrid event-sourcing model:
- **Immutable history**: `store_events`, `marketplace_revocations`, `processed_events`, `audit_logs`
- **Mutable projection**: `user_entitlements` (canonical current state)
- **Operational tables**: `notifications` (scheduled notifications)

## Core Tables

### user_entitlements
**Purpose**: Canonical current state per (user_id, source) pair

**Key Design**:
- Composite PK: `(user_id, source)` — allows multiple simultaneous sources
- `active BOOLEAN` — whether this source currently grants premium
- `expires_at TIMESTAMPTZ` — when this source's grant expires (nullable)
- `last_event_time BIGINT` — timestamp of last event that updated this row (for ordering)
- `updated_at TIMESTAMPTZ` — when this row was last modified
- `carrier_polled_at TIMESTAMPTZ` — last time we polled carrier for this user (if source=CARRIER)

**Constraints**:
- `source IN ('STORE', 'CARRIER', 'MARKETPLACE')`
- `expires_at > updated_at` (logical consistency)

**Indexes**:
- `idx_user_entitlements_user_id` — queries by user_id
- `idx_user_entitlements_expires_at` — find expiring entitlements
- `idx_user_entitlements_carrier_polled` — find users needing carrier polling

**Example Data**:
```
user_id | source    | active | expires_at           | reason     | updated_at
--------|-----------|--------|---------------------|------------|---------------------
u_42    | STORE     | true   | 2026-06-10T00:00:00Z| RENEWAL    | 2026-05-20T11:23:00Z
u_42    | CARRIER   | false  | NULL                | NULL       | 2026-05-19T09:00:00Z
u_42    | MARKETPLACE| false  | NULL                | NULL       | 2026-05-18T15:30:00Z
```

---

### store_events
**Purpose**: Immutable history of all store webhook events

**Key Design**:
- Primary key: `id` (BIGSERIAL for efficient inserts)
- `event_id TEXT UNIQUE` — webhook's unique event identifier
- `event_time_ms BIGINT` — **ordering source** (from webhook payload, not arrival time)
- Immutable: never updated, only inserted

**Constraints**:
- `event_id UNIQUE` — duplicates detected via unique constraint
- `type IN (...)` — validate event types
- `event_time_ms > 0`

**Indexes**:
- `idx_store_events_user_id` — query events for a user
- `idx_store_events_event_time` — ordered by event time for reconciliation

**Example Data**:
```
id | event_id      | user_id | type            | event_time_ms | created_at
---|---------------|---------|-----------------|---------------|---------------------
1  | evt_purchase1 | u_42    | INITIAL_PURCHASE| 1716000000000 | 2026-05-18T10:00:00Z
2  | evt_renewal1  | u_42    | RENEWAL         | 1716600000000 | 2026-05-20T11:23:00Z
```

**Processing Logic**:
1. Webhook arrives with `event_id`, `event_time_ms`
2. Check `processed_events`: if already processed, skip (idempotent)
3. Insert into `store_events` (unique constraint catches duplicates)
4. Check `user_entitlements`: if `event_time_ms >= last_event_time`, apply state change
5. Update `user_entitlements` and mark processed in `processed_events`

---

### marketplace_revocations
**Purpose**: Immutable history of all marketplace revoke operations

**Key Design**:
- Similar to `store_events` but for bulk revoke
- `event_id UNIQUE` — each revoke batch gets unique ID
- Immutable: audit trail of revokes

**Indexes**:
- `idx_marketplace_revocations_user_id` — query revokes for a user

**Processing Logic**:
1. Revoke request arrives with list of user IDs and bulk event_id
2. Check `processed_events`: if already processed, skip (idempotent)
3. For each user_id: update `user_entitlements` WHERE source='MARKETPLACE' SET active=FALSE
4. Insert into `marketplace_revocations` for audit
5. Mark processed in `processed_events`

---

### processed_events
**Purpose**: Idempotency check for all external events

**Key Design**:
- Composite PK: `(event_id, source)` — same event_id can come from different sources
- `processed_at TIMESTAMPTZ` — when this event was processed
- Immutable: never updated, only inserted

**Query**:
```sql
SELECT 1 FROM processed_events 
WHERE event_id = $1 AND source = $2
```

If found: event already processed, skip (idempotent).
If not found: process event, then insert into `processed_events`.

---

### notifications
**Purpose**: Scheduled notifications (e.g., "premium expires in 24h")

**Key Design**:
- `user_id TEXT NOT NULL`
- `type TEXT` — 'PREMIUM_EXPIRES_SOON'
- `scheduled_for TIMESTAMPTZ` — when to send this notification
- `sent_at TIMESTAMPTZ` — when it was sent (NULL = not yet sent)
- `UNIQUE (user_id, type, DATE(scheduled_for))` — **deduplication guarantee**

**Deduplication Logic**:
```sql
INSERT INTO notifications (user_id, type, scheduled_for) 
VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;
```

This atomically prevents duplicate notifications. If a notification with the same (user_id, type, date) already exists, the insert is silently ignored.

**Workflow**:
1. Every time entitlement is updated: check if `active=true` and `expires_at < now + 24h`
2. If yes: INSERT (using ON CONFLICT DO NOTHING) to schedule notification
3. Worker queries: `SELECT * FROM notifications WHERE scheduled_for <= now AND sent_at IS NULL`
4. Worker updates: `UPDATE notifications SET sent_at = now WHERE id = $1`

**Example**:
```
user_id | type                  | scheduled_for        | sent_at
--------|----------------------|---------------------|---------------------
u_42    | PREMIUM_EXPIRES_SOON | 2026-06-09T12:00:00Z| NULL
```

---

### audit_logs (Stretch Feature)
**Purpose**: Complete history of entitlement state transitions

**Key Design**:
- `user_id, source` — which entitlement changed
- `previous_active, next_active` — before/after state
- `previous_expires_at, next_expires_at` — before/after expiration
- `triggering_event_id` — which event caused this (nullable for manual changes)
- `created_at` — when this transition occurred

**Populated On**:
- Every update to `user_entitlements` table (via trigger or application logic)

**Query Example**:
```sql
SELECT * FROM audit_logs 
WHERE user_id = $1 
ORDER BY created_at DESC 
LIMIT 100;
```

Returns complete history of changes for a user.

---

## Migration Strategy

Three migration files, applied in order:

### 001_initial_schema.up.sql
Creates all core tables:
- `user_entitlements` (composite PK)
- `store_events` (immutable)
- `marketplace_revocations` (immutable)
- `processed_events` (dedup check)
- `notifications` (scheduled notifications)
- `audit_logs` (stretch feature)

### 002_add_indexes.up.sql
Creates performance indexes:
- `idx_user_entitlements_user_id` — common query pattern
- `idx_user_entitlements_expires_at` — expiring entitlements
- `idx_store_events_user_id` — event lookup
- `idx_store_events_event_time` — event ordering
- `idx_marketplace_revocations_user_id` — revoke history
- `idx_notifications_scheduled_for` — due notifications
- `idx_notifications_user_type` — notification deduplication
- `idx_audit_logs_user_id` — timeline queries

### 003_add_carrier_polling.up.sql
Adds carrier polling support:
- `carrier_polled_at TIMESTAMPTZ` column on `user_entitlements`
- Index for finding users needing polling

---

## Concurrency & Ordering Guarantees

### Store Webhook Processing
1. **Event Ordering**: Use `event_time_ms` (not arrival time)
2. **Late Arrivals**: Store all events, but only apply if `event_time_ms >= last_event_time`
3. **Out-of-Order**: Automatically handled by timestamp comparison

### Carrier Polling Concurrency
1. **Worker Coordination**: `FOR UPDATE SKIP LOCKED` prevents duplicate work
2. **Lock Duration**: Held only during poll, released immediately after
3. **Multiple Workers**: Each claims a batch of 100 users, skips claimed rows

### Notification Deduplication
1. **Database Constraint**: `UNIQUE (user_id, type, DATE(scheduled_for))`
2. **Atomicity**: `INSERT ... ON CONFLICT DO NOTHING` is atomic
3. **No Race Conditions**: Database enforces uniqueness, not application code

---

## Constraints & Validation

### Data Integrity
- `source IN ('STORE', 'CARRIER', 'MARKETPLACE')` — enforce valid sources
- `type IN (...)` — enforce valid event types
- `expires_at > updated_at` — logical consistency
- `sent_at >= scheduled_for` — notifications sent after scheduled time

### Foreign Keys
- No explicit foreign keys (normalized design with immutable history)
- Logical relationship: `user_entitlements` + `store_events` share user_id

---

## Performance Characteristics

| Query | Index | Time |
|-------|-------|------|
| Get current entitlements for user | `idx_user_entitlements_user_id` | O(log n) |
| Find expiring entitlements | `idx_user_entitlements_expires_at` | O(log n) |
| Find users for carrier polling | `idx_user_entitlements_carrier_polled` | O(log n) |
| Get due notifications | `idx_notifications_scheduled_for` | O(log n) |
| Audit trail for user | `idx_audit_logs_user_id` | O(log n) |

All operations are indexed for fast queries on large datasets.

---

## Key Design Decisions

1. **Composite PK on `user_entitlements`**: Allows multiple sources per user (e.g., STORE active + MARKETPLACE inactive)
2. **Immutable event tables**: Complete audit trail, no data loss, replaying possible
3. **Database-enforced deduplication**: `UNIQUE` constraints + `ON CONFLICT DO NOTHING` prevent application bugs
4. **Event time as ordering source**: `event_time_ms` from webhook, not arrival time, handles late arrivals correctly
5. **Separate audit table**: Optional (stretch feature) for complete audit trail without bloating main tables
