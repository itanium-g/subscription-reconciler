# Database Schema Documentation

## Overview

The schema is built on a **hybrid event-sourcing model**:
- **Immutable History**: `store_events`, `marketplace_revocations`, `processed_events`, `audit_logs`
- **Mutable Projection**: `user_entitlements` (canonical current state)
- **Operational Tables**: `notifications` (scheduled notifications)

---

## Core Tables

### `user_entitlements`
**Purpose**: Represents the canonical current state per `(user_id, source)` pair.

**Key Design**:
- **Composite PK**: `(user_id, source)` — Permits tracking of multiple simultaneous sources natively.
- **Fields**:
  - `active (BOOLEAN)`: Indicates if this source currently grants premium access.
  - `expires_at (TIMESTAMPTZ)`: Denotes when this source's grant expires (nullable).
  - `last_event_time (BIGINT)`: Timestamp of the last event that modified this row (critical for state ordering).
  - `updated_at (TIMESTAMPTZ)`: Record modification timestamp.
  - `carrier_polled_at (TIMESTAMPTZ)`: Tracks the last carrier API poll (specific to `source = 'CARRIER'`).

**Constraints**:
- `source IN ('STORE', 'CARRIER', 'MARKETPLACE')`
- `expires_at > updated_at` (Logical integrity)

**Indexes**:
- `idx_user_entitlements_user_id` — Optimizes reads by user.
- `idx_user_entitlements_expires_at` — Optimizes locating expiring entitlements for notifications.
- `idx_user_entitlements_carrier_polled` — Optimizes background worker polling queries.

**Example Data**:

| user_id | source      | active | expires_at            | reason  | updated_at           |
|---------|-------------|--------|-----------------------|---------|----------------------|
| `u_42`  | STORE       | `true` | `2026-06-10T00:00:00Z`| RENEWAL | `2026-05-20T11:23:00Z`|
| `u_42`  | CARRIER     | `false`| `NULL`                | `NULL`  | `2026-05-19T09:00:00Z`|
| `u_42`  | MARKETPLACE | `false`| `NULL`                | `NULL`  | `2026-05-18T15:30:00Z`|

---

### `store_events`
**Purpose**: An immutable append-only ledger of all store webhook payloads.

**Key Design**:
- `id (BIGSERIAL)`: Highly efficient append-only surrogate key.
- `event_id (TEXT UNIQUE)`: Guarantees no two webhooks share an identifier.
- `event_time_ms (BIGINT)`: **The single source of truth for ordering**, extracted directly from the webhook payload.

**Example Data**:

| id  | event_id       | user_id | type             | event_time_ms | created_at           |
|-----|----------------|---------|------------------|---------------|----------------------|
| `1` | `evt_purchase1`| `u_42`  | INITIAL_PURCHASE | `1716000000000`| `2026-05-18T10:00:00Z`|
| `2` | `evt_renewal1` | `u_42`  | RENEWAL          | `1716600000000`| `2026-05-20T11:23:00Z`|

> [!TIP]
> **Processing Workflow**:
> 1. Webhook arrives.
> 2. Idempotency Check (via `processed_events`).
> 3. Insert into `store_events` (Unique constraint enforces uniqueness).
> 4. If `payload.event_time_ms >= current.last_event_time`, update `user_entitlements`.
> 5. Mark as processed.

---

### `marketplace_revocations`
**Purpose**: An immutable audit trail of marketplace bulk revocation events.

**Key Design**:
- Functions similarly to `store_events`, structurally isolating marketplace webhook processing logic.

---

### `processed_events`
**Purpose**: The central idempotency authority for external events.

**Key Design**:
- **Composite PK**: `(event_id, source)` — Ensures that identical `event_id`s from differing sources do not collide.
- Evaluated prior to processing any webhook to guarantee strict at-most-once operational semantics.

---

### `notifications`
**Purpose**: Stores scheduled background tasks (e.g., "premium expires in 24h").

**Key Design & Deduplication Guarantee**:
- Features a sophisticated `UNIQUE (user_id, type, ((scheduled_for AT TIME ZONE 'UTC')::date))` constraint.
- Ensures absolute atomic deduplication at the database layer.

> [!NOTE]
> **Deduplication Logic**:
> ```sql
> INSERT INTO notifications (user_id, type, scheduled_for) 
> VALUES ($1, $2, $3) ON CONFLICT DO NOTHING;
> ```
> If a notification of identical `type` for the same `user_id` on the same `date` exists, the insert is gracefully swallowed.

---

### `audit_logs` (Stretch Feature)
**Purpose**: Comprehensive, traversable history of every entitlement state transition.

**Key Design**:
- Captures `previous_active` / `next_active` and `previous_expires_at` / `next_expires_at`.
- Interwoven seamlessly into the same PostgreSQL transaction as `user_entitlements` updates.

---

## Migration Strategy

Migrations are executed via `migrate/migrate` automatically during initialization.

1. **`001_initial_schema.up.sql`**: Initializes core tables, immutable constraint indexes, and idempotency checks.
2. **`002_add_indexes.up.sql`**: Injects optimal lookup paths for high-frequency queries (e.g., locating expired users).
3. **`003_add_carrier_polling.up.sql`**: Modifies the entitlement projection for carrier polling metadata.

---

## Concurrency & Ordering Guarantees

### Webhook Event Ordering
- **Event Time Superiority**: Arrival time is explicitly ignored. `event_time_ms` drives state.
- **Late Arrivals**: An event arriving days late is logged but rejected by the projection logic if the user possesses a newer active state.

### Carrier Polling Concurrency
- **Zero-Collision Polling**: Employs `FOR UPDATE SKIP LOCKED`. Worker instances immediately lock a batch of rows; neighboring concurrent workers dynamically skip these rows and poll the next available batch.

---

## Performance Characteristics

| Query Objective | Utilized Index | Complexity |
|-----------------|----------------|------------|
| Fetch user entitlements | `idx_user_entitlements_user_id` | `O(log n)` |
| Locate expiring users | `idx_user_entitlements_expires_at` | `O(log n)` |
| Identify carrier poll targets | `idx_user_entitlements_carrier_polled` | `O(log n)` |
| Retrieve due notifications | `idx_notifications_scheduled_for` | `O(log n)` |
| Construct user audit trail | `idx_audit_logs_user_id` | `O(log n)` |
