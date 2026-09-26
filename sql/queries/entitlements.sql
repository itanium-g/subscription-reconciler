-- name: GetEntitlementByUserAndSource :one
SELECT user_id, source, active, expires_at, reason, updated_at, last_event_time, carrier_polled_at
FROM user_entitlements
WHERE user_id = $1 AND source = $2;

-- name: GetEntitlementsByUser :many
SELECT user_id, source, active, expires_at, reason, updated_at, last_event_time, carrier_polled_at
FROM user_entitlements
WHERE user_id = $1
ORDER BY source;

-- name: LockEntitlementUser :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(user_id)::text, 0));

-- name: UpsertEntitlement :execrows
INSERT INTO user_entitlements (user_id, source, active, expires_at, reason, updated_at, last_event_time)
VALUES ($1, $2, $3, $4, $5, NOW(), $6)
ON CONFLICT (user_id, source) DO UPDATE SET
  active = EXCLUDED.active,
  expires_at = EXCLUDED.expires_at,
  reason = EXCLUDED.reason,
  updated_at = NOW(),
  last_event_time = EXCLUDED.last_event_time
WHERE user_entitlements.last_event_time < EXCLUDED.last_event_time;

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

-- name: GetExpiredEntitlementsForReconciliation :many
SELECT user_id, source, active, expires_at, reason, updated_at, last_event_time, carrier_polled_at
FROM user_entitlements
WHERE active = TRUE
  AND expires_at IS NOT NULL
  AND expires_at <= NOW()
ORDER BY expires_at ASC
LIMIT $1
FOR UPDATE SKIP LOCKED;

-- name: ListExpiredEntitlementsForReconciliation :many
SELECT user_id, source, active, expires_at, reason, updated_at, last_event_time, carrier_polled_at
FROM user_entitlements
WHERE active = TRUE
  AND expires_at IS NOT NULL
  AND expires_at <= NOW()
ORDER BY expires_at ASC
LIMIT $1;

-- name: ExpireEntitlement :execrows
UPDATE user_entitlements
SET active = FALSE,
    expires_at = NULL,
    reason = $3,
    updated_at = NOW(),
    last_event_time = $4
WHERE user_id = $1
  AND source = $2
  AND active = TRUE
  AND expires_at IS NOT NULL
  AND expires_at <= NOW();

-- name: ClaimDueCarrierEntitlements :many
WITH due AS (
  SELECT candidate.user_id
  FROM user_entitlements AS candidate
  WHERE candidate.source = 'CARRIER'
    AND candidate.active = TRUE
    AND (candidate.carrier_polled_at IS NULL OR candidate.carrier_polled_at <= sqlc.arg(due_before))
  ORDER BY candidate.carrier_polled_at ASC NULLS FIRST, candidate.user_id ASC
  LIMIT sqlc.arg(batch_limit)
  FOR UPDATE SKIP LOCKED
)
UPDATE user_entitlements AS entitlement
SET carrier_polled_at = NOW()
FROM due
WHERE entitlement.user_id = due.user_id
  AND entitlement.source = 'CARRIER'
RETURNING entitlement.user_id, entitlement.source, entitlement.active,
          entitlement.expires_at, entitlement.reason, entitlement.updated_at,
          entitlement.last_event_time, entitlement.carrier_polled_at;
