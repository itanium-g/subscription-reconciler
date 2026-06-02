-- name: InsertStoreEvent :exec
INSERT INTO store_events (event_id, user_id, type, event_time_ms, product_id)
VALUES ($1, $2, $3, $4, $5);

-- name: GetStoreEventsByUser :many
SELECT id, event_id, user_id, type, event_time_ms, product_id, created_at
FROM store_events
WHERE user_id = $1
ORDER BY event_time_ms DESC;

-- name: GetStoreEventByID :one
SELECT id, event_id, user_id, type, event_time_ms, product_id, created_at
FROM store_events
WHERE event_id = $1;
