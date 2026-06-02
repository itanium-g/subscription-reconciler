-- name: IsEventProcessed :one
SELECT EXISTS(
  SELECT 1 FROM processed_events
  WHERE event_id = $1 AND source = $2
);

-- name: MarkEventProcessed :exec
INSERT INTO processed_events (event_id, source, processed_at)
VALUES ($1, $2, NOW())
ON CONFLICT DO NOTHING;

-- name: GetProcessedEvent :one
SELECT event_id, source, processed_at
FROM processed_events
WHERE event_id = $1 AND source = $2;
