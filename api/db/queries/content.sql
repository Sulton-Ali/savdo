-- name: GetContentBlock :one
-- One (key, locale) pair, exact match only — no fallback here. The Phase 6
-- content service resolves requested -> uz -> any (D-104) in Go from
-- ListContentBlocksByKey's rows; this is the plain single-row read the
-- manager+ editor uses (GET /content/{key} reads one locale at a time).
SELECT * FROM content_blocks
WHERE shop_id = $1 AND key = $2 AND locale = $3;

-- name: ListContentBlocksByKey :many
-- Every locale saved for one key, for the admin editor (shows uz/ru/en side
-- by side) and for the D-104 fallback resolution a single content read
-- needs.
SELECT * FROM content_blocks
WHERE shop_id = $1 AND key = $2
ORDER BY locale;

-- name: ListContentBlocksForShop :many
-- Every key, every locale, for the shop — feeds GET /public/shop, which
-- resolves each key's fallback (requested -> uz -> any, D-104) in Go from
-- this one query rather than one query per key.
SELECT * FROM content_blocks
WHERE shop_id = $1
ORDER BY key, locale;

-- name: UpsertContentBlock :one
INSERT INTO content_blocks (shop_id, key, locale, data, updated_by, updated_at)
VALUES ($1, $2, $3, $4, $5, now())
ON CONFLICT (shop_id, key, locale) DO UPDATE
SET data = EXCLUDED.data, updated_by = EXCLUDED.updated_by, updated_at = now()
RETURNING *;
