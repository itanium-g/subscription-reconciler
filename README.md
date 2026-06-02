# Premium Entitlement Reconciler

A backend service that reconciles premium subscriptions from three independent sales channels (in-app store, mobile carrier, third-party marketplace) into a single canonical entitlement state.

## Problem Statement

Premium access can be granted or revoked by any of three channels:
1. **In-app store**: Pushes webhooks (at-least-once, unordered, may arrive late)
2. **Mobile carrier**: No webhooks; must poll API on schedule
3. **Third-party marketplace**: Pushes bulk revoke requests once per month

The service maintains the canonical truth: "Is this user premium right now, and why?"

## Architecture

### Design Principles

- **Hybrid event model**: Immutable event history + mutable canonical projection
  - `store_events`, `marketplace_revocations`, `processed_events`: Never modified
  - `user_entitlements`: Current state per source (fast reads)
- **Multiple simultaneous sources**: A user can be active via STORE and inactive via MARKETPLACE simultaneously
- **Source isolation**: Marketplace revoke only affects MARKETPLACE source; carrier polling only affects CARRIER
- **Deterministic priority**: API returns single source using priority: STORE > CARRIER > MARKETPLACE > NONE

### Key Features

- **Idempotency**: All external events deduplicated via `processed_events` table
- **Timestamp-based ordering**: STORE events ordered by `event_time_ms` (webhook payload), not arrival time
- **Late-arriving events**: Persisted but don't overwrite newer state
- **Worker concurrency**: Carrier polling uses `FOR UPDATE SKIP LOCKED` for safe concurrent workers
- **Notification deduplication**: Database constraints guarantee at most one expiration notification per user per day

## Quick Start

### Prerequisites

- Docker Engine 29.5.2+
- Docker Compose 2.36.1+

### Run Everything

```bash
docker compose up
```

This starts:
- PostgreSQL database
- API server (http://localhost:8080)
- Background worker (carrier polling)
- Mock carrier API (http://localhost:8081)

### Local Development

```bash
# Install dependencies
go mod download

# Run linter
make lint

# Run tests
make test

# Build binaries
make build

# Run API locally (requires PostgreSQL running)
make run-api

# Run worker locally (requires PostgreSQL running)
make run-worker
```

## API Endpoints

### Store Webhook

```http
POST /webhooks/store
Content-Type: application/json

{
  "eventId": "evt_abc123",
  "userId": "u_42",
  "type": "INITIAL_PURCHASE|RENEWAL|CANCELLATION|BILLING_ISSUE|EXPIRATION|UN_CANCELLATION",
  "eventTimeMs": 1716700000000,
  "productId": "premium_monthly"
}
```

Handles:
- Duplicate events (idempotent)
- Out-of-order delivery
- Late arrivals (only overwrites if timestamp is newer)

### Marketplace Revoke

```http
POST /webhooks/marketplace/revoke
Content-Type: application/json

{
  "userIds": ["u_42", "u_91", "u_133"]
}
```

Only revokes marketplace-granted access; leaves STORE and CARRIER unchanged.

### Get User Entitlement

```http
GET /users/{userId}/entitlement
```

Response:

```json
{
  "active": true,
  "source": "STORE|CARRIER|MARKETPLACE|NONE",
  "expiresAt": "2026-06-10T00:00:00Z",
  "lastChangedAt": "2026-05-20T11:23:00Z",
  "reason": "RENEWAL"
}
```

**Response priority**: Returns the highest-priority active source (STORE > CARRIER > MARKETPLACE). If multiple sources are active, the highest-priority one is returned.

### Timeline (Stretch)

```http
GET /users/{userId}/timeline
```

Returns reconstructed history of entitlement changes from audit log (requires stretch implementation).

## Testing with curl

Once the application is running, you can test its core and stretch functionality using the following `curl` command examples:

### 1. In-App Store Webhooks (`POST /webhooks/store`)

**Initial Purchase (Active)**
Grant premium access to `user_store_1` starting now:
```bash
curl -X POST http://localhost:8080/webhooks/store \
  -H "Content-Type: application/json" \
  -d '{
    "eventId": "evt_store_purchase_001",
    "userId": "user_store_1",
    "type": "INITIAL_PURCHASE",
    "eventTimeMs": '$(date +%s000)',
    "productId": "premium_monthly"
  }'
```

**Duplicate Event Ingestion (Idempotency)**
Send the exact same request again to test idempotency:
```bash
curl -X POST http://localhost:8080/webhooks/store \
  -H "Content-Type: application/json" \
  -d '{
    "eventId": "evt_store_purchase_001",
    "userId": "user_store_1",
    "type": "INITIAL_PURCHASE",
    "eventTimeMs": '$(date +%s000)',
    "productId": "premium_monthly"
  }'
```
*(Should return `isDuplicate: true`)*

**Out-of-Order / Late-Arriving Event**
Simulate a late-arriving event by sending a `BILLING_ISSUE` (which cancels access) timestamped in the past (e.g., 20 seconds ago), after the active purchase:
```bash
curl -X POST http://localhost:8080/webhooks/store \
  -H "Content-Type: application/json" \
  -d '{
    "eventId": "evt_store_late_billing_002",
    "userId": "user_store_1",
    "type": "BILLING_ISSUE",
    "eventTimeMs": '$(( $(date +%s) - 20 ))'000',
    "productId": "premium_monthly"
  }'
```
*(Will be accepted for logging but will not overwrite the active purchase status since it is timestamped earlier)*

### 2. Entitlement Queries (`GET /users/:id/entitlement`)

Retrieve the current canonical entitlement for a user:
```bash
curl http://localhost:8080/users/user_store_1/entitlement
```

### 3. Marketplace Bulk Revoke (`POST /webhooks/marketplace/revoke`)

Revoke marketplace corporate access bulk for a list of users:
```bash
curl -X POST http://localhost:8080/webhooks/marketplace/revoke \
  -H "Content-Type: application/json" \
  -d '{
    "userIds": ["user_market_1", "user_market_2"]
  }'
```

### 4. Carrier Polling Mock (`GET /mock/carrier/plan`)

Query the carrier mock endpoint to check its randomized output behavior:
```bash
curl "http://localhost:8080/mock/carrier/plan?userId=user_carrier_1"
```

### 5. Transition Timeline / Audit Trail (`GET /users/:id/timeline`)

Query the reconstructed state transition history for a user:
```bash
curl http://localhost:8080/users/user_store_1/timeline
```

## Database Schema

### Core Tables

- **user_entitlements**: Current state per source (PK: user_id, source)
- **store_events**: All store webhook events (immutable)
- **marketplace_revocations**: All marketplace revoke events (immutable)
- **processed_events**: Idempotency check (event_id UNIQUE)
- **notifications**: Scheduled notifications (deduped by user_id, type, date)

### Stretch Tables

- **audit_logs**: Complete state transition history

## Design Decisions

### 1. Hybrid Event Model vs. Full Event Sourcing

**Decision**: Hybrid model (immutable history + mutable projection)

**Why**: 
- Immutable history provides auditability and debugging
- Mutable projection in `user_entitlements` enables fast reads
- Avoids overhead of replaying all events on every query
- Pragmatic balance between auditability and performance

### 2. Multiple Simultaneous Sources

**Decision**: `user_entitlements` has composite key `(user_id, source)`

**Why**:
- Allows independent source tracking
- Marketplace revoke naturally only affects MARKETPLACE rows
- Carrier polling naturally only affects CARRIER rows
- Reflects assignment requirement: "only marketplace-granted access should be revoked"

### 3. Single Source in API Response

**Decision**: Deterministic priority (STORE > CARRIER > MARKETPLACE > NONE)

**Why**:
- Assignment requires single source in response
- Priority is deterministic and documented
- Clients have clear, unambiguous contract
- Avoids race conditions in multi-source selection

### 4. Event Ordering (STORE Only)

**Decision**: Use `event_time_ms` from webhook; late events persist but don't overwrite

**Why**:
- STORE webhooks have "no ordering guarantee"
- `event_time_ms` is the system's source of truth for causality
- Late arrivals are persisted for audit
- Newer (by timestamp) state always "wins"
- CARRIER and MARKETPLACE don't need ordering (polling + bulk revoke)

### 5. Worker Concurrency (Carrier Polling)

**Decision**: `FOR UPDATE SKIP LOCKED` for row-level locking

**Why**:
- PostgreSQL native, no external lock service required
- Efficient: workers skip claimed rows, don't block
- Simple: SQL handles all coordination
- Safe: no double-polling of same user

### 6. Notification Deduplication

**Decision**: Database constraints `INSERT ... ON CONFLICT DO NOTHING`

**Why**:
- No race conditions (database enforces atomically)
- Cheaper than application-level logic
- Consistent with PostgreSQL best practices
- Trivial to reason about

## Testing

Integration tests use PostgreSQL Testcontainers for real database behavior.

**Coverage priorities**:
- Duplicate store events
- Out-of-order events
- Late-arriving events
- Marketplace isolation (only revokes MARKETPLACE source)
- Carrier inactive/error handling
- Notification deduplication
- Concurrent worker safety

Run tests:

```bash
make test
```

## Tradeoffs & Production Considerations

### What Would Change with Another Week

1. **Richer audit trail**: Detailed audit logs on every state change
2. **Metrics & observability**: Prometheus metrics (reconciliation latency, error rates)
3. **Webhook retry logic**: Backoff strategy for failed webhooks
4. **Carrier API caching**: Cache recent responses to reduce load
5. **Bulk operations**: Batch notification sends, batch carrier polls
6. **Rate limiting**: Per-user rate limits on webhook ingestion
7. **Request signing**: Verify HMAC signatures on marketplace webhooks

## Project Structure

```
.
├── cmd/
│   ├── api/                   # API server
│   └── worker/                # Background job runner
├── internal/
│   ├── domain/                # Domain types (Entitlement, Event, etc.)
│   ├── application/           # Service layer (reconciliation logic)
│   ├── infrastructure/
│   │   ├── postgres/          # Repository implementations
│   │   ├── http/              # HTTP handlers
│   │   └── worker/            # Job implementations
├── migrations/                # SQL migrations
├── sql/
│   └── queries/               # sqlc query files
├── openapi/                   # OpenAPI 3.1 spec
├── tests/                     # Integration tests
├── Makefile
├── docker-compose.yml
├── Dockerfile
├── go.mod
├── go.sum
└── README.md
```

## Contributing

- Use `make fmt` to format code
- Use `make lint` to check for issues
- Use `make test` to run tests
- Commit frequently with clear messages
- Each phase ends with a reviewable commit
