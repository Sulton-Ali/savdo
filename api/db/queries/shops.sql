-- name: GetShop :one
SELECT * FROM shops
WHERE id = $1;

-- name: LockShop :one
-- Per-tenant serialization point for default-location changes: acquire this
-- lock first, then decide/update location defaults within the same
-- transaction.
SELECT id FROM shops
WHERE id = $1
FOR UPDATE;

-- name: GetShopBySlug :one
SELECT * FROM shops
WHERE slug = $1;

-- name: CreateShop :one
-- For seeding: the MVP has exactly one shop (D-30). Columns with defaults
-- (currency, timezone, default_locale, next_sale_number, ...) are left to
-- the database; use UpdateShop to change them afterwards.
INSERT INTO shops (id, slug, name)
VALUES ($1, $2, $3)
RETURNING *;

-- name: UpdateShop :one
-- Patch: every settings field is optional via sqlc.narg + COALESCE, so a
-- caller only supplies the fields it wants to change. next_sale_number is
-- deliberately not here: it is only ever advanced under FOR UPDATE by the
-- sales flow (Phase 4), never by a settings update.
UPDATE shops
SET
    name = COALESCE(sqlc.narg('name'), name),
    currency = COALESCE(sqlc.narg('currency'), currency),
    timezone = COALESCE(sqlc.narg('timezone'), timezone),
    default_locale = COALESCE(sqlc.narg('default_locale'), default_locale),
    allow_negative_stock = COALESCE(sqlc.narg('allow_negative_stock'), allow_negative_stock),
    update_cost_on_purchase = COALESCE(sqlc.narg('update_cost_on_purchase'), update_cost_on_purchase),
    ai_daily_token_budget = COALESCE(sqlc.narg('ai_daily_token_budget'), ai_daily_token_budget),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;
