-- name: GetIdempotencyKey :one
-- Looked up on every request carrying an `Idempotency-Key` header
-- (docs/05-API.md § Conventions) before the operation runs; a hit returns
-- the stored response instead of repeating the write.
SELECT * FROM idempotency_keys
WHERE shop_id = $1 AND key = $2;

-- name: InsertIdempotencyKey :one
-- Written once, after the operation completes, with its result already
-- known — there is no separate "in flight" state to update later.
INSERT INTO idempotency_keys (shop_id, key, request_hash, response_status, response_body)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;
