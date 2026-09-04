-- name: ListSuppliers :many
-- Keyset pagination on (created_at, id), newest first — same convention as
-- ListUsers/ListLocations. q is a plain ILIKE substring match on name; the
-- suppliers directory is small (O-15-style reasoning), no trigram index
-- needed.
SELECT * FROM suppliers
WHERE shop_id = sqlc.arg('shop_id')
    AND deleted_at IS NULL
    AND (
        sqlc.narg('q')::text IS NULL
        OR name ILIKE '%' || sqlc.narg('q') || '%'
    )
    AND (
        sqlc.narg('cursor_created_at')::timestamptz IS NULL
        OR (created_at, id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::uuid)
    )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('limit');

-- name: GetSupplier :one
SELECT * FROM suppliers
WHERE shop_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: CreateSupplier :one
INSERT INTO suppliers (id, shop_id, name, contact_name, phone, telegram_username, note)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateSupplier :one
-- Patch with explicit clear flags for the nullable fields (COALESCE cannot
-- express "set to NULL"), same shape as UpdateVariant/UpdateProduct.
UPDATE suppliers
SET
    name = COALESCE(sqlc.narg('name'), name),
    contact_name = CASE WHEN sqlc.arg('clear_contact_name')::bool THEN NULL ELSE COALESCE(sqlc.narg('contact_name'), contact_name) END,
    phone = CASE WHEN sqlc.arg('clear_phone')::bool THEN NULL ELSE COALESCE(sqlc.narg('phone'), phone) END,
    telegram_username = CASE WHEN sqlc.arg('clear_telegram_username')::bool THEN NULL ELSE COALESCE(sqlc.narg('telegram_username'), telegram_username) END,
    note = CASE WHEN sqlc.arg('clear_note')::bool THEN NULL ELSE COALESCE(sqlc.narg('note'), note) END,
    updated_at = now()
WHERE shop_id = sqlc.arg('shop_id') AND id = sqlc.arg('id') AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteSupplier :exec
UPDATE suppliers
SET deleted_at = now(), updated_at = now()
WHERE shop_id = $1 AND id = $2;
