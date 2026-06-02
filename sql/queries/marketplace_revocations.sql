-- name: InsertMarketplaceRevocation :execresult
INSERT INTO marketplace_revocations (event_id, user_id)
VALUES ($1, $2)
ON CONFLICT (event_id) DO NOTHING;

-- name: GetMarketplaceRevocationByEventID :one
SELECT id, event_id, user_id, created_at
FROM marketplace_revocations
WHERE event_id = $1;
