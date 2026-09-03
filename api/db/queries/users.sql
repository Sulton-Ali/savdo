-- name: GetUserByUsername :one
-- username is citext: this comparison is case-insensitive by column type,
-- no lower() needed.
SELECT * FROM users
WHERE shop_id = $1 AND username = $2;

-- name: GetUserByID :one
SELECT * FROM users
WHERE shop_id = $1 AND id = $2;

-- name: GetOwner :one
-- The shop's owner account, used by `savdo reset-owner-password` (D-28).
-- MVP has exactly one owner per shop; ORDER BY + LIMIT is defensive in case
-- that ever changes.
SELECT * FROM users
WHERE shop_id = $1 AND role = 'owner'
ORDER BY created_at ASC
LIMIT 1;

-- name: ListUsers :many
-- Keyset pagination on (created_at, id), newest first. Pass NULL for both
-- cursor_created_at and cursor_id to get the first page; for the next page,
-- pass the created_at/id of the last row returned.
SELECT * FROM users
WHERE shop_id = sqlc.arg('shop_id')
    AND (
        sqlc.narg('cursor_created_at')::timestamptz IS NULL
        OR (created_at, id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::uuid)
    )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('limit');

-- name: CreateUser :one
INSERT INTO users (id, shop_id, username, password_hash, full_name, phone, role, locale)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: UpdateUser :one
-- Patch: full_name/phone/role/locale/is_active are optional. Password and
-- last_login_at go through their own single-purpose statements below so a
-- generic profile edit can never accidentally touch either.
UPDATE users
SET
    full_name = COALESCE(sqlc.narg('full_name'), full_name),
    phone = COALESCE(sqlc.narg('phone'), phone),
    role = COALESCE(sqlc.narg('role'), role),
    locale = COALESCE(sqlc.narg('locale'), locale),
    is_active = COALESCE(sqlc.narg('is_active'), is_active),
    updated_at = now()
WHERE shop_id = sqlc.arg('shop_id') AND id = sqlc.arg('id')
RETURNING *;

-- name: SetUserPassword :exec
UPDATE users
SET password_hash = $3, updated_at = now()
WHERE shop_id = $1 AND id = $2;

-- name: SetUserLastLogin :exec
UPDATE users
SET last_login_at = now()
WHERE shop_id = $1 AND id = $2;
