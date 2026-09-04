-- name: ListVariantsForStaff :many
SELECT * FROM product_variants
WHERE shop_id = $1 AND product_id = $2 AND deleted_at IS NULL
ORDER BY created_at;

-- name: ListVariantsForCashier :many
-- No cost_override (§ 04-DATA-MODEL.md rule 8, ADR-010).
SELECT
    id, shop_id, product_id, sku, barcode, attributes,
    price_override, is_active, deleted_at, created_at, updated_at
FROM product_variants
WHERE shop_id = $1 AND product_id = $2 AND deleted_at IS NULL
ORDER BY created_at;

-- name: GetVariantForStaff :one
SELECT * FROM product_variants
WHERE shop_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: GetVariantForCashier :one
SELECT
    id, shop_id, product_id, sku, barcode, attributes,
    price_override, is_active, deleted_at, created_at, updated_at
FROM product_variants
WHERE shop_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: CreateVariant :one
INSERT INTO product_variants (
    id, shop_id, product_id, sku, barcode, attributes,
    price_override, cost_override, is_active
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: UpdateVariant :one
-- Patch with explicit clear flags for sku/barcode/price_override/cost_override
-- (COALESCE cannot express "set to NULL"). attributes is intentionally not
-- patchable here: it participates in the (product_id, attributes) unique
-- constraint and changing it is a delete+recreate at the service layer, not
-- an in-place edit.
UPDATE product_variants
SET
    sku = CASE WHEN sqlc.arg('clear_sku')::bool THEN NULL ELSE COALESCE(sqlc.narg('sku'), sku) END,
    barcode = CASE WHEN sqlc.arg('clear_barcode')::bool THEN NULL ELSE COALESCE(sqlc.narg('barcode'), barcode) END,
    price_override = CASE WHEN sqlc.arg('clear_price_override')::bool THEN NULL ELSE COALESCE(sqlc.narg('price_override'), price_override) END,
    cost_override = CASE WHEN sqlc.arg('clear_cost_override')::bool THEN NULL ELSE COALESCE(sqlc.narg('cost_override'), cost_override) END,
    is_active = COALESCE(sqlc.narg('is_active'), is_active),
    updated_at = now()
WHERE shop_id = sqlc.arg('shop_id') AND id = sqlc.arg('id') AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteVariant :exec
UPDATE product_variants
SET deleted_at = now(), updated_at = now()
WHERE shop_id = $1 AND id = $2;

-- name: CountActiveVariants :one
-- Used by the service to keep "every product has at least one variant"
-- (§ 04-DATA-MODEL.md) — refuse deleting the last active variant of a
-- product, and to decide when to flip products.has_variants.
SELECT count(*) FROM product_variants
WHERE shop_id = $1 AND product_id = $2 AND deleted_at IS NULL AND is_active;
