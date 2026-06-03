-- name: GetEntitlementByUserAndSource :one
SELECT user_id, source, active, expires_at, reason, updated_at, last_event_time, carrier_polled_at
FROM user_entitlements
WHERE user_id = $1 AND source = $2;

-- name: GetEntitlementsByUser :many
SELECT user_id, source, active, expires_at, reason, updated_at, last_event_time, carrier_polled_at
FROM user_entitlements
WHERE user_id = $1
ORDER BY source;

-- name: UpsertEntitlement :exec
INSERT INTO user_entitlements (user_id, source, active, expires_at, reason, updated_at, last_event_time)
VALUES ($1, $2, $3, $4, $5, NOW(), $6)
ON CONFLICT (user_id, source) DO UPDATE SET
  active = EXCLUDED.active,
  expires_at = EXCLUDED.expires_at,
  reason = EXCLUDED.reason,
  updated_at = NOW(),
  last_event_time = EXCLUDED.last_event_time
WHERE user_entitlements.last_event_time < EXCLUDED.last_event_time;

-- name: UpdateEntitlementCarrierPolledAt :exec
UPDATE user_entitlements
SET carrier_polled_at = NOW()
WHERE user_id = $1 AND source = $2;

-- name: GetLastEventTimeFromStore :one
SELECT COALESCE(MAX(last_event_time), 0)
FROM user_entitlements
WHERE user_id = $1 AND source = 'STORE';

-- name: GetEntitlementsExpiringWithin24h :many
SELECT user_id, source, active, expires_at, reason, updated_at, last_event_time, carrier_polled_at
FROM user_entitlements
WHERE active = TRUE 
  AND expires_at IS NOT NULL 
  AND expires_at <= NOW() + INTERVAL '24 hours'
  AND expires_at > NOW()
ORDER BY expires_at ASC;

-- name: GetCarrierEntitlementsForPolling :many
SELECT user_id, source, active, expires_at, reason, updated_at, last_event_time, carrier_polled_at
FROM user_entitlements
WHERE source = 'CARRIER' AND active = TRUE
ORDER BY carrier_polled_at ASC NULLS FIRST
LIMIT $1
FOR UPDATE SKIP LOCKED;
