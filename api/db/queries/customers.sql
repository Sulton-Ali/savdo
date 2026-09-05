-- name: ListCustomers :many
-- Keyset pagination on (created_at, id), newest first — same convention as
-- ListSuppliers. q matches full_name or phone by substring (ILIKE); the
-- customer directory can grow larger than suppliers, but a leading-'%'
-- ILIKE cannot use an index either way, so no trigram index is added here
-- (same reasoning as ListSuppliers).
SELECT * FROM customers
WHERE shop_id = sqlc.arg('shop_id')
    AND deleted_at IS NULL
    AND (
        sqlc.narg('q')::text IS NULL
        OR full_name ILIKE '%' || sqlc.narg('q') || '%'
        OR phone ILIKE '%' || sqlc.narg('q') || '%'
    )
    AND (
        sqlc.narg('cursor_created_at')::timestamptz IS NULL
        OR (created_at, id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::uuid)
    )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('limit');

-- name: GetCustomer :one
SELECT * FROM customers
WHERE shop_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: CreateCustomer :one
INSERT INTO customers (id, shop_id, full_name, phone, telegram_username, note, tags)
VALUES (
    sqlc.arg('id'), sqlc.arg('shop_id'), sqlc.arg('full_name'),
    sqlc.narg('phone'), sqlc.narg('telegram_username'), sqlc.narg('note'),
    COALESCE(sqlc.narg('tags')::text[], '{}')
)
RETURNING *;

-- name: UpdateCustomer :one
-- Patch with explicit clear flags for the nullable fields (COALESCE cannot
-- express "set to NULL"), same shape as UpdateSupplier. tags is a full
-- replace, not a merge — the service reads-modifies-writes the array if it
-- needs to add/remove one tag.
UPDATE customers
SET
    full_name = COALESCE(sqlc.narg('full_name'), full_name),
    phone = CASE WHEN sqlc.arg('clear_phone')::bool THEN NULL ELSE COALESCE(sqlc.narg('phone'), phone) END,
    telegram_username = CASE WHEN sqlc.arg('clear_telegram_username')::bool THEN NULL ELSE COALESCE(sqlc.narg('telegram_username'), telegram_username) END,
    note = CASE WHEN sqlc.arg('clear_note')::bool THEN NULL ELSE COALESCE(sqlc.narg('note'), note) END,
    tags = COALESCE(sqlc.narg('tags')::text[], tags),
    updated_at = now()
WHERE shop_id = sqlc.arg('shop_id') AND id = sqlc.arg('id') AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteCustomer :exec
UPDATE customers
SET deleted_at = now(), updated_at = now()
WHERE shop_id = $1 AND id = $2 AND deleted_at IS NULL;
