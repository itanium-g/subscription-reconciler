-- Create user_entitlements table
-- Stores current entitlement state per (user_id, source) pair
-- Supports multiple simultaneous sources: a user can be active via STORE and inactive via MARKETPLACE
CREATE TABLE user_entitlements (
    user_id TEXT NOT NULL,
    source TEXT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at TIMESTAMPTZ,
    reason TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_event_time BIGINT NOT NULL DEFAULT 0,
    
    PRIMARY KEY (user_id, source),
    CHECK (source IN ('STORE', 'CARRIER', 'MARKETPLACE')),
    CHECK (expires_at IS NULL OR expires_at > updated_at)
);

-- Create store_events table
-- Immutable history of all store webhook events
CREATE TABLE store_events (
    id BIGSERIAL PRIMARY KEY,
    event_id TEXT NOT NULL UNIQUE,
    user_id TEXT NOT NULL,
    type TEXT NOT NULL,
    event_time_ms BIGINT NOT NULL,
    product_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CHECK (type IN ('INITIAL_PURCHASE', 'RENEWAL', 'CANCELLATION', 'BILLING_ISSUE', 'EXPIRATION', 'UN_CANCELLATION')),
    CHECK (event_time_ms > 0)
);

-- Create marketplace_revocations table
-- Immutable history of all marketplace revoke operations
CREATE TABLE marketplace_revocations (
    id BIGSERIAL PRIMARY KEY,
    event_id TEXT NOT NULL UNIQUE,
    user_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Create processed_events table
-- Idempotency check: all external events deduplicated here
CREATE TABLE processed_events (
    event_id TEXT NOT NULL,
    source TEXT NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    PRIMARY KEY (event_id, source),
    CHECK (source IN ('STORE', 'MARKETPLACE'))
);

-- Create notifications table
-- Scheduled notifications (e.g., "premium expires soon")
CREATE TABLE notifications (
    id BIGSERIAL PRIMARY KEY,
    user_id TEXT NOT NULL,
    type TEXT NOT NULL,
    scheduled_for TIMESTAMPTZ NOT NULL,
    sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CHECK (type IN ('PREMIUM_EXPIRES_SOON')),
    CHECK (sent_at IS NULL OR sent_at >= scheduled_for)
);

-- One notification per (user_id, type, calendar day) — deduplication guarantee.
-- Expressed as a unique index so sqlc can parse it (inline DATE() in UNIQUE is not supported by sqlc).
CREATE UNIQUE INDEX uq_notifications_user_type_day
    ON notifications (user_id, type, ((scheduled_for AT TIME ZONE 'UTC')::date));

-- Create audit_logs table (stretch feature)
-- Complete history of entitlement state transitions
CREATE TABLE audit_logs (
    id BIGSERIAL PRIMARY KEY,
    user_id TEXT NOT NULL,
    source TEXT NOT NULL,
    previous_active BOOLEAN,
    next_active BOOLEAN NOT NULL,
    previous_expires_at TIMESTAMPTZ,
    next_expires_at TIMESTAMPTZ,
    triggering_event_id TEXT,
    reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CHECK (source IN ('STORE', 'CARRIER', 'MARKETPLACE'))
);
