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
    p.low_stock_threshold, p.created_at, p.updated_at,
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
    p.low_stock_threshold, p.created_at, p.updated_at,
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
    is_active, is_featured, low_stock_threshold
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING *;

-- name: UpdateProduct :one
-- Patch with explicit clear flags for nullable fields: COALESCE cannot
-- express "set to NULL", so clear_category/clear_sku/clear_cost/
-- clear_low_stock_threshold clear their column outright, and clear_promo
-- clears promo_price/promo_from/promo_to together (they are one concept).
-- has_variants is not editable here — see SetProductHasVariants, a
-- single-purpose statement like ClearUserPhone.
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
    low_stock_threshold = CASE WHEN sqlc.arg('clear_low_stock_threshold')::bool THEN NULL ELSE COALESCE(sqlc.narg('low_stock_threshold'), low_stock_threshold) END,
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

-- Phase 6 public catalogue (docs/06-ROADMAP.md Phase 6, D-99, D-103). Both
-- queries below LEFT JOIN categories (orchestrator ruling, O-row TBD): a
-- product with no category (category_id IS NULL) still belongs in the
-- public catalogue — only a product whose category exists and is
-- soft-deleted/inactive is hidden. category_slug is therefore nullable:
-- NULL when the product has no category at all. When category_slug is
-- given as a filter, an uncategorized product's c.slug is NULL and never
-- equals the filter, so it is excluded from a category-filtered list, and
-- the visibility condition below still requires that category to be
-- active — a category-filtered list never surfaces a product whose
-- matching category is inactive. Neither embeds a cover image: the
-- handler reuses ListCoverImagesForProducts (D-83, same one-query-per-page
-- pattern ListProducts already uses) against this query's page of product
-- ids instead of duplicating that selection logic (flagged cover else
-- first by position) in another LATERAL here. GetPublicProductBySlug's
-- own LATERAL (below) resolves name+description+locale in the same
-- pass as the product row (T3 review round 1: previously the handler
-- discarded that LATERAL's name/locale_used and ran a second
-- ListProductTranslations query, redoing the same fallback in Go for
-- name+description together — now description is selected alongside
-- name so the LATERAL is the single source, no second query). Variants
-- and images for the product-detail page still reuse
-- ListVariantsForCashier (filtered to is_active in the handler — its
-- column set already excludes cost_override, the only thing rule 8
-- requires a separate query for) and ListProductImages.

-- name: ListPublicProducts :many
-- T7: `category_slug` matches the named category AND every one of its
-- active, non-deleted descendants (matched_categories, a recursive walk
-- down parent_id from the one category named by slug) — a parent slug
-- like "erkaklar" now lists its children's products too, not just
-- (usually zero) products assigned directly to the parent. When
-- category_slug is NULL the base case's own IS NOT NULL guard makes
-- matched_categories empty and the OR short-circuits, same as before
-- T7. A product whose direct category is inactive is still excluded
-- entirely by the unrelated (p.category_id IS NULL OR (c.deleted_at IS
-- NULL AND c.is_active)) line below (O-22) — matched_categories only
-- widens which *active* categories count as a match, it does not loosen
-- that visibility rule.
WITH RECURSIVE matched_categories AS (
    SELECT id FROM categories
    WHERE shop_id = sqlc.arg('shop_id') AND deleted_at IS NULL AND is_active
        AND sqlc.narg('category_slug')::text IS NOT NULL AND slug = sqlc.narg('category_slug')

    UNION ALL

    SELECT ch.id
    FROM categories ch
    JOIN matched_categories m ON ch.parent_id = m.id
    WHERE ch.shop_id = sqlc.arg('shop_id') AND ch.deleted_at IS NULL AND ch.is_active
)
SELECT
    p.id, p.shop_id, p.category_id, p.unit_id, p.slug, p.sku,
    p.base_price, p.promo_price, p.promo_from, p.promo_to,
    p.is_featured, p.created_at,
    c.slug AS category_slug,
    COALESCE(t.locale, '') AS locale_used,
    COALESCE(t.name, '') AS name
FROM products p
LEFT JOIN categories c ON c.id = p.category_id AND c.shop_id = p.shop_id
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
    AND (p.category_id IS NULL OR (c.deleted_at IS NULL AND c.is_active))
    AND (sqlc.narg('category_slug')::text IS NULL OR p.category_id IN (SELECT id FROM matched_categories))
    AND (sqlc.narg('featured')::bool IS NULL OR p.is_featured = sqlc.narg('featured'))
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

-- name: GetPublicProductBySlug :one
SELECT
    p.id, p.shop_id, p.category_id, p.unit_id, p.slug, p.sku,
    p.base_price, p.promo_price, p.promo_from, p.promo_to,
    p.is_featured, p.created_at,
    c.slug AS category_slug,
    COALESCE(t.locale, '') AS locale_used,
    COALESCE(t.name, '') AS name,
    t.description
FROM products p
LEFT JOIN categories c ON c.id = p.category_id AND c.shop_id = p.shop_id
LEFT JOIN LATERAL (
    SELECT pt.locale, pt.name, pt.description
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
    AND p.slug = sqlc.arg('slug')
    AND p.deleted_at IS NULL
    AND p.is_active
    AND (p.category_id IS NULL OR (c.deleted_at IS NULL AND c.is_active));
