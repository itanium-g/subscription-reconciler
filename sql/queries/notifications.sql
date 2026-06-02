-- name: ScheduleNotification :exec
INSERT INTO notifications (user_id, type, scheduled_for)
VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;

-- name: GetDueNotifications :many
SELECT id, user_id, type, scheduled_for, sent_at, created_at
FROM notifications
WHERE scheduled_for <= NOW() AND sent_at IS NULL
ORDER BY scheduled_for ASC
LIMIT $1;

-- name: MarkNotificationSent :exec
UPDATE notifications
SET sent_at = NOW()
WHERE id = $1;

-- name: GetNotificationByUserTypeAndDate :one
SELECT id, user_id, type, scheduled_for, sent_at, created_at
FROM notifications
WHERE user_id = $1 AND type = $2 AND ((scheduled_for AT TIME ZONE 'UTC')::date) = ((sqlc.arg(date) AT TIME ZONE 'UTC')::date);
