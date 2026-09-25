-- name: ScheduleNotification :exec
INSERT INTO notifications (user_id, type, scheduled_for)
VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;

-- name: GetDueNotifications :many
SELECT id, user_id, type, scheduled_for, sent_at, created_at
FROM notifications
WHERE scheduled_for <= NOW() AND sent_at IS NULL
ORDER BY scheduled_for ASC
LIMIT $1
FOR UPDATE SKIP LOCKED;

-- name: ClaimDueNotifications :many
WITH due AS (
    SELECT id
    FROM notifications
    WHERE scheduled_for <= NOW() AND sent_at IS NULL
    ORDER BY scheduled_for ASC, id ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
UPDATE notifications AS n
SET sent_at = NOW()
FROM due
WHERE n.id = due.id
  AND n.sent_at IS NULL
RETURNING n.id, n.user_id, n.type, n.scheduled_for, n.sent_at, n.created_at;

-- name: MarkNotificationSent :exec
UPDATE notifications
SET sent_at = NOW()
WHERE id = $1;

-- name: GetNotificationByUserTypeAndDate :one
SELECT id, user_id, type, scheduled_for, sent_at, created_at
FROM notifications
WHERE user_id = $1 AND type = $2 AND ((scheduled_for AT TIME ZONE 'UTC')::date) = ((sqlc.arg(date) AT TIME ZONE 'UTC')::date);
