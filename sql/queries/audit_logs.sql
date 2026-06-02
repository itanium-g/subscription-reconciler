-- name: InsertAuditLog :exec
INSERT INTO audit_logs (user_id, source, previous_active, next_active, previous_expires_at, next_expires_at, triggering_event_id, reason)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: GetAuditLogsByUser :many
SELECT id, user_id, source, previous_active, next_active, previous_expires_at, next_expires_at, triggering_event_id, reason, created_at
FROM audit_logs
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountAuditLogsByUser :one
SELECT COUNT(*) as count
FROM audit_logs
WHERE user_id = $1;
