-- Three list/get families, one per role, with different column sets rather
-- than one query filtered in Go (§ 04-DATA-MODEL.md rule 8, ADR-010):
-- ...ForStaff selects cost_price, ...ForCashier and ...Public never do.
--
-- Shared filters across all three lists:
--   q                 trigram match (ILIKE substring OR pg_trgm similarity)
--                      against any locale's translated name
--   category_id       optional exact match (sqlc.narg)
--   cursor_created_at/cursor_id  keyset pagination, newest first
-- ForStaff/ForCashier additionally take include_inactive; Public is always
-- active-only and never sees inactive products.

-- name: ListProductsForStaff :many
SELECT
    p.*,
    -- COALESCE to '': a product with zero translations must still list, not
    -- fail to scan (LEFT JOIN LATERAL leaves these NULL and sqlc does not
    -- infer that as nullable).
    COALESCE(t.locale, '') AS locale_used,
    COALESCE(t.name, '') AS name
FROM products p
LEFT JOIN LATERAL (
    SELECT pt.locale, pt.name
    FROM product_translations pt
    WHERE pt.product_id = p.id
    ORDER BY
        CASE
            WHEN pt.locale = sqlc.arg('locale') THEN 0
            WHEN pt.locale = 'uz' THEN 1
            ELSE 2
        END
    LIMIT 1
) t ON true
WHERE p.shop_id = sqlc.arg('shop_id')
    AND p.deleted_at IS NULL
    AND (sqlc.arg('include_inactive')::bool OR p.is_active)
    AND (sqlc.narg('category_id')::uuid IS NULL OR p.category_id = sqlc.narg('category_id'))
    AND (
        sqlc.narg('q')::text IS NULL
        OR EXISTS (
            SELECT 1 FROM product_translations spt
            WHERE spt.product_id = p.id
                AND (spt.name ILIKE '%' || sqlc.narg('q') || '%' OR spt.name % sqlc.narg('q'))
        )
    )
    AND (
        sqlc.narg('cursor_created_at')::timestamptz IS NULL
        OR (p.created_at, p.id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::uuid)
    )
ORDER BY p.created_at DESC, p.id DESC
LIMIT sqlc.arg('limit');

-- name: ListProductsForCashier :many
SELECT
    p.id, p.shop_id, p.category_id, p.unit_id, p.slug, p.sku,
    p.base_price, p.promo_price, p.promo_from, p.promo_to,
    p.is_active, p.is_featured, p.has_variants, p.deleted_at,
    p.created_at, p.updated_at,
    COALESCE(t.locale, '') AS locale_used,
    COALESCE(t.name, '') AS name
FROM products p
LEFT JOIN LATERAL (
    SELECT pt.locale, pt.name
    FROM product_translations pt
    WHERE pt.product_id = p.id
    ORDER BY
        CASE
            WHEN pt.locale = sqlc.arg('locale') THEN 0
            WHEN pt.locale = 'uz' THEN 1
            ELSE 2
        END
    LIMIT 1
) t ON true
WHERE p.shop_id = sqlc.arg('shop_id')
    AND p.deleted_at IS NULL
    AND (sqlc.arg('include_inactive')::bool OR p.is_active)
    AND (sqlc.narg('category_id')::uuid IS NULL OR p.category_id = sqlc.narg('category_id'))
    AND (
        sqlc.narg('q')::text IS NULL
        OR EXISTS (
            SELECT 1 FROM product_translations spt
            WHERE spt.product_id = p.id
                AND (spt.name ILIKE '%' || sqlc.narg('q') || '%' OR spt.name % sqlc.narg('q'))
        )
    )
    AND (
        sqlc.narg('cursor_created_at')::timestamptz IS NULL
        OR (p.created_at, p.id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::uuid)
    )
ORDER BY p.created_at DESC, p.id DESC
LIMIT sqlc.arg('limit');

-- name: ListProductsPublic :many
-- Active only, always; no include_inactive parameter at all.
SELECT
    p.id, p.shop_id, p.category_id, p.unit_id, p.slug, p.sku,
    p.base_price, p.promo_price, p.promo_from, p.promo_to,
    p.is_featured, p.created_at,
    COALESCE(t.locale, '') AS locale_used,
    COALESCE(t.name, '') AS name
FROM products p
LEFT JOIN LATERAL (
    SELECT pt.locale, pt.name
    FROM product_translations pt
    WHERE pt.product_id = p.id
    ORDER BY
        CASE
            WHEN pt.locale = sqlc.arg('locale') THEN 0
            WHEN pt.locale = 'uz' THEN 1
            ELSE 2
        END
    LIMIT 1
) t ON true
WHERE p.shop_id = sqlc.arg('shop_id')
    AND p.deleted_at IS NULL
    AND p.is_active
    AND (sqlc.narg('category_id')::uuid IS NULL OR p.category_id = sqlc.narg('category_id'))
    AND (
        sqlc.narg('q')::text IS NULL
        OR EXISTS (
            SELECT 1 FROM product_translations spt
            WHERE spt.product_id = p.id
                AND (spt.name ILIKE '%' || sqlc.narg('q') || '%' OR spt.name % sqlc.narg('q'))
        )
    )
    AND (
        sqlc.narg('cursor_created_at')::timestamptz IS NULL
        OR (p.created_at, p.id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::uuid)
    )
ORDER BY p.created_at DESC, p.id DESC
LIMIT sqlc.arg('limit');

-- name: GetProductForStaff :one
-- Same locale-fallback + COALESCE pattern as ListProductsForStaff.
SELECT
    p.*,
    COALESCE(t.locale, '') AS locale_used,
    COALESCE(t.name, '') AS name
FROM products p
LEFT JOIN LATERAL (
    SELECT pt.locale, pt.name
    FROM product_translations pt
    WHERE pt.product_id = p.id
    ORDER BY
        CASE
            WHEN pt.locale = sqlc.arg('locale') THEN 0
            WHEN pt.locale = 'uz' THEN 1
            ELSE 2
        END
    LIMIT 1
) t ON true
WHERE p.shop_id = sqlc.arg('shop_id') AND p.id = sqlc.arg('id') AND p.deleted_at IS NULL;

-- name: GetProductForCashier :one
-- Same locale-fallback + COALESCE pattern as ListProductsForCashier; no
-- cost_price (§ 04-DATA-MODEL.md rule 8, ADR-010).
SELECT
    p.id, p.shop_id, p.category_id, p.unit_id, p.slug, p.sku,
    p.base_price, p.promo_price, p.promo_from, p.promo_to,
    p.is_active, p.is_featured, p.has_variants, p.deleted_at,
    p.created_at, p.updated_at,
    COALESCE(t.locale, '') AS locale_used,
    COALESCE(t.name, '') AS name
FROM products p
LEFT JOIN LATERAL (
    SELECT pt.locale, pt.name
    FROM product_translations pt
    WHERE pt.product_id = p.id
    ORDER BY
        CASE
            WHEN pt.locale = sqlc.arg('locale') THEN 0
            WHEN pt.locale = 'uz' THEN 1
            ELSE 2
        END
    LIMIT 1
) t ON true
WHERE p.shop_id = sqlc.arg('shop_id') AND p.id = sqlc.arg('id') AND p.deleted_at IS NULL;

-- name: GetProductPublic :one
-- Same locale-fallback + COALESCE pattern as ListProductsPublic; active
-- only, never cost.
SELECT
    p.id, p.shop_id, p.category_id, p.unit_id, p.slug, p.sku,
    p.base_price, p.promo_price, p.promo_from, p.promo_to,
    p.is_featured, p.created_at,
    COALESCE(t.locale, '') AS locale_used,
    COALESCE(t.name, '') AS name
FROM products p
LEFT JOIN LATERAL (
    SELECT pt.locale, pt.name
    FROM product_translations pt
    WHERE pt.product_id = p.id
    ORDER BY
        CASE
            WHEN pt.locale = sqlc.arg('locale') THEN 0
            WHEN pt.locale = 'uz' THEN 1
            ELSE 2
        END
    LIMIT 1
) t ON true
WHERE p.shop_id = sqlc.arg('shop_id') AND p.id = sqlc.arg('id') AND p.deleted_at IS NULL AND p.is_active;

-- name: CreateProduct :one
INSERT INTO products (
    id, shop_id, category_id, unit_id, slug, sku,
    base_price, cost_price, promo_price, promo_from, promo_to,
    is_active, is_featured
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING *;

-- name: UpdateProduct :one
-- Patch with explicit clear flags for nullable fields: COALESCE cannot
-- express "set to NULL", so clear_category/clear_sku/clear_cost clear their
-- column outright, and clear_promo clears promo_price/promo_from/promo_to
-- together (they are one concept). has_variants is not editable here — see
-- SetProductHasVariants, a single-purpose statement like ClearUserPhone.
UPDATE products
SET
    category_id = CASE WHEN sqlc.arg('clear_category')::bool THEN NULL ELSE COALESCE(sqlc.narg('category_id'), category_id) END,
    unit_id = COALESCE(sqlc.narg('unit_id'), unit_id),
    slug = COALESCE(sqlc.narg('slug'), slug),
    sku = CASE WHEN sqlc.arg('clear_sku')::bool THEN NULL ELSE COALESCE(sqlc.narg('sku'), sku) END,
    base_price = COALESCE(sqlc.narg('base_price'), base_price),
    cost_price = CASE WHEN sqlc.arg('clear_cost')::bool THEN NULL ELSE COALESCE(sqlc.narg('cost_price'), cost_price) END,
    promo_price = CASE WHEN sqlc.arg('clear_promo')::bool THEN NULL ELSE COALESCE(sqlc.narg('promo_price'), promo_price) END,
    promo_from = CASE WHEN sqlc.arg('clear_promo')::bool THEN NULL ELSE COALESCE(sqlc.narg('promo_from'), promo_from) END,
    promo_to = CASE WHEN sqlc.arg('clear_promo')::bool THEN NULL ELSE COALESCE(sqlc.narg('promo_to'), promo_to) END,
    is_active = COALESCE(sqlc.narg('is_active'), is_active),
    is_featured = COALESCE(sqlc.narg('is_featured'), is_featured),
    updated_at = now()
WHERE shop_id = sqlc.arg('shop_id') AND id = sqlc.arg('id') AND deleted_at IS NULL
RETURNING *;

-- name: SetProductHasVariants :exec
-- Flipped by the service once the second variant is added (or back once
-- only the default variant remains) — never part of the general patch.
UPDATE products
SET has_variants = $3, updated_at = now()
WHERE shop_id = $1 AND id = $2;

-- name: SoftDeleteProduct :exec
UPDATE products
SET deleted_at = now(), updated_at = now()
WHERE shop_id = $1 AND id = $2;

-- name: UpsertProductTranslation :exec
INSERT INTO product_translations (product_id, locale, name, description)
VALUES ($1, $2, $3, $4)
ON CONFLICT (product_id, locale) DO UPDATE
SET name = EXCLUDED.name, description = EXCLUDED.description;

-- name: ListProductTranslations :many
-- shop_id is joined through the parent product, not a column on
-- product_translations itself: a translation row must not be readable
-- through the wrong shop_id (hard rule 1 — every query filters by
-- shop_id), even though the bare product_id FK would otherwise let it scan.
SELECT t.* FROM product_translations t
JOIN products p ON p.id = t.product_id AND p.shop_id = $2
WHERE t.product_id = $1
ORDER BY t.locale;
