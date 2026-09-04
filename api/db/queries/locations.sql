-- name: ListLocations :many
-- Keyset pagination on (created_at, id), newest first — same convention as
-- ListUsers.
SELECT * FROM locations
WHERE shop_id = sqlc.arg('shop_id')
    AND (
        sqlc.narg('cursor_created_at')::timestamptz IS NULL
        OR (created_at, id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::uuid)
    )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('limit');

-- name: GetLocation :one
SELECT * FROM locations
WHERE shop_id = $1 AND id = $2;

-- name: GetLocationForUpdate :one
SELECT * FROM locations
WHERE shop_id = $1 AND id = $2
FOR UPDATE;

-- name: ShopHasLocations :one
-- Used inside the transaction after LockShop to decide whether a location
-- about to be created is the shop's first (and therefore its default).
SELECT EXISTS (SELECT 1 FROM locations WHERE shop_id = $1);

-- name: CreateLocation :one
INSERT INTO locations (id, shop_id, name, kind, is_default, is_active)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateLocation :one
-- Patch. Setting is_default = true here can violate the "one default per
-- shop" partial unique index unless the caller has already run
-- ClearDefaultLocation in the same transaction.
UPDATE locations
SET
    name = COALESCE(sqlc.narg('name'), name),
    kind = COALESCE(sqlc.narg('kind'), kind),
    is_default = COALESCE(sqlc.narg('is_default'), is_default),
    is_active = COALESCE(sqlc.narg('is_active'), is_active),
    updated_at = now()
WHERE shop_id = sqlc.arg('shop_id') AND id = sqlc.arg('id')
RETURNING *;

-- name: ClearDefaultLocation :exec
-- Run before UpdateLocation(..., is_default = true) in the same transaction
-- so the partial unique index on (shop_id) WHERE is_default is never
-- violated.
UPDATE locations
SET is_default = false, updated_at = now()
WHERE shop_id = $1 AND is_default = true;
