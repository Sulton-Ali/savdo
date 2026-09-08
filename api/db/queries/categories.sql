-- name: ListCategories :many
-- Flat list (the service builds the tree from parent_id); resolved name by
-- locale fallback (requested -> 'uz' -> any); include_inactive toggles
-- whether inactive categories are returned (the admin category picker wants
-- them, the public/cashier catalog does not).
SELECT
    c.*,
    -- COALESCE to '': a category with zero translations (e.g. mid-creation,
    -- before its first UpsertCategoryTranslation) must still list, not fail
    -- to scan — LEFT JOIN LATERAL leaves t.locale/t.name NULL when no
    -- translation matches, and sqlc does not infer that as nullable. An
    -- empty string, not a real locale, signals "no translation yet".
    COALESCE(t.locale, '') AS locale_used,
    COALESCE(t.name, '') AS name,
    -- No COALESCE here, unlike name/locale_used above: description is
    -- nullable at the column level (category_translations.description has
    -- no NOT NULL), so sqlc already infers *string for it straight from the
    -- schema — the LATERAL-join NULL-when-no-match case lands on the same
    -- nullable type instead of needing a '' sentinel.
    t.description
FROM categories c
LEFT JOIN LATERAL (
    SELECT ct.locale, ct.name, ct.description
    FROM category_translations ct
    WHERE ct.category_id = c.id
    ORDER BY
        CASE
            WHEN ct.locale = sqlc.arg('locale') THEN 0
            WHEN ct.locale = 'uz' THEN 1
            ELSE 2
        END
    LIMIT 1
) t ON true
WHERE c.shop_id = sqlc.arg('shop_id')
    AND c.deleted_at IS NULL
    AND (sqlc.arg('include_inactive')::bool OR c.is_active)
ORDER BY c.sort_order, c.slug;

-- name: GetCategory :one
-- Same locale-fallback + COALESCE pattern as ListCategories.
SELECT
    c.*,
    COALESCE(t.locale, '') AS locale_used,
    COALESCE(t.name, '') AS name,
    t.description
FROM categories c
LEFT JOIN LATERAL (
    SELECT ct.locale, ct.name, ct.description
    FROM category_translations ct
    WHERE ct.category_id = c.id
    ORDER BY
        CASE
            WHEN ct.locale = sqlc.arg('locale') THEN 0
            WHEN ct.locale = 'uz' THEN 1
            ELSE 2
        END
    LIMIT 1
) t ON true
WHERE c.shop_id = sqlc.arg('shop_id') AND c.id = sqlc.arg('id') AND c.deleted_at IS NULL;

-- name: ListCategoryTranslations :many
-- shop_id is joined through the parent category, same reasoning as
-- ListProductTranslations: a translation row must not be readable through
-- the wrong shop_id (hard rule 1), even though the bare category_id FK
-- would otherwise let it scan.
SELECT t.locale, t.name, t.description
FROM category_translations t
JOIN categories c ON c.id = t.category_id AND c.shop_id = $2
WHERE t.category_id = $1
ORDER BY t.locale;

-- name: CreateCategory :one
INSERT INTO categories (id, shop_id, parent_id, slug, sort_order, is_active, image_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateCategory :one
-- Patch with explicit clear flags for parent_id/image_id (COALESCE cannot
-- express "set to NULL"), same pattern as UpdateProduct/UpdateVariant.
-- slug/sort_order/is_active are plain optional fields.
UPDATE categories
SET
    parent_id = CASE WHEN sqlc.arg('clear_parent')::bool THEN NULL ELSE COALESCE(sqlc.narg('parent_id'), parent_id) END,
    slug = COALESCE(sqlc.narg('slug'), slug),
    sort_order = COALESCE(sqlc.narg('sort_order'), sort_order),
    is_active = COALESCE(sqlc.narg('is_active'), is_active),
    image_id = CASE WHEN sqlc.arg('clear_image')::bool THEN NULL ELSE COALESCE(sqlc.narg('image_id'), image_id) END,
    updated_at = now()
WHERE shop_id = sqlc.arg('shop_id') AND id = sqlc.arg('id') AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteCategory :exec
UPDATE categories
SET deleted_at = now(), updated_at = now()
WHERE shop_id = $1 AND id = $2;

-- name: CountProductsInCategory :one
-- Used by the service to refuse deleting a category that still has products
-- assigned to it (a conflict, not a cascading delete).
SELECT count(*) FROM products
WHERE shop_id = $1 AND category_id = $2 AND deleted_at IS NULL;

-- name: UpsertCategoryTranslation :exec
INSERT INTO category_translations (category_id, locale, name, description)
VALUES ($1, $2, $3, $4)
ON CONFLICT (category_id, locale) DO UPDATE
SET name = EXCLUDED.name, description = EXCLUDED.description;

-- name: GetCategoryDepth :one
-- Depth of category_id counting from 1 at a root (no parent), by walking
-- parent_id up to the root. The service calls this before creating or
-- re-parenting a category to enforce depth <= 3 (§ 04-DATA-MODEL.md).
-- Both arms filter deleted_at IS NULL and shop_id: a soft-deleted category
-- (or one that somehow belongs to another shop) must not be usable as a
-- parent, and must not silently participate in a live category's own
-- ancestor chain either.
WITH RECURSIVE ancestors AS (
    SELECT cat.id, cat.parent_id, 1 AS depth
    FROM categories cat
    WHERE cat.id = sqlc.arg('category_id') AND cat.shop_id = sqlc.arg('shop_id') AND cat.deleted_at IS NULL

    UNION ALL

    SELECT c.id, c.parent_id, a.depth + 1
    FROM categories c
    JOIN ancestors a ON c.id = a.parent_id
    WHERE c.shop_id = sqlc.arg('shop_id') AND c.deleted_at IS NULL
)
-- COALESCE: category_id not found (wrong shop, or soft-deleted) makes
-- ancestors empty, and max() over zero rows is NULL — 0 reads better than
-- NULL for "does not exist / has no ancestors" here.
SELECT COALESCE(max(a.depth), 0)::int AS depth FROM ancestors a;

-- name: GetCategorySubtreeHeight :one
-- Height of category_id's own subtree counting from 1 at itself (a leaf
-- has height 1), by walking parent_id -> children down from it. The
-- service calls this before re-parenting a category to make sure the
-- category's own descendants would not end up past depth 3: if placed
-- under a parent at depth(parent), the deepest descendant would land at
-- depth(parent) + height(self), which must stay <= 3. Both arms filter
-- deleted_at IS NULL and shop_id, same reasoning as GetCategoryDepth.
WITH RECURSIVE descendants AS (
    SELECT cat.id, 1 AS height
    FROM categories cat
    WHERE cat.id = sqlc.arg('category_id') AND cat.shop_id = sqlc.arg('shop_id') AND cat.deleted_at IS NULL

    UNION ALL

    SELECT c.id, d.height + 1
    FROM categories c
    JOIN descendants d ON c.parent_id = d.id
    WHERE c.shop_id = sqlc.arg('shop_id') AND c.deleted_at IS NULL
)
SELECT COALESCE(max(d.height), 0)::int AS height FROM descendants d;

-- name: ListPublicCategories :many
-- Phase 6 public catalogue (D-99), extended by T7 for the landing's
-- parent/child grouping: active, non-deleted categories, each with its
-- resolved name (same locale-fallback + COALESCE '' pattern as
-- ListCategories, active-only like ListPublicProducts, no
-- include_inactive parameter at all), its immediate parent's slug/name
-- (parent_slug/parent_name), and a product_count that now sums active,
-- non-deleted products across the category AND every one of its active,
-- non-deleted descendants (descendants CTE) — a pure grouping parent
-- like "Erkaklar" (no products of its own) reports the total of its
-- children instead of 0.
--
-- parent_slug/parent_name are NULL for a root category (parent_id IS
-- NULL) and, deliberately, also for a category whose parent exists but
-- is inactive or soft-deleted: the same "inactive hides everything about
-- it, not just itself" rule O-22 already applies to a product's own
-- category — a child must not surface an ancestor's slug/name that this
-- same endpoint would never itself list as a row, since the landing
-- could never link to it. Such a child sorts as a top-level entry too
-- (paths CTE's own base case), consistent with having no visible parent.
--
-- Rows are ordered by a materialized (sort_order, slug) path built at
-- each level by the recursive paths CTE, so a parent always sorts
-- immediately before its own children, at any depth up to the schema's
-- depth <= 3 rule (04-DATA-MODEL.md) — not just the two levels the
-- current catalogue actually uses.
WITH RECURSIVE active_cats AS (
    SELECT c.id, c.parent_id, c.slug, c.sort_order
    FROM categories c
    WHERE c.shop_id = sqlc.arg('shop_id') AND c.deleted_at IS NULL AND c.is_active
),
descendants AS (
    -- Every active category is its own descendant at distance 0, so a
    -- leaf with no children still gets a product_counts row below,
    -- matching the pre-T7 per-category-only count exactly in that case.
    SELECT id AS ancestor_id, id AS descendant_id FROM active_cats

    UNION ALL

    SELECT d.ancestor_id, c.id
    FROM active_cats c
    JOIN descendants d ON c.parent_id = d.descendant_id
),
product_counts AS (
    SELECT d.ancestor_id AS category_id, count(p.id) AS product_count
    FROM descendants d
    JOIN products p ON p.category_id = d.descendant_id
        AND p.shop_id = sqlc.arg('shop_id') AND p.deleted_at IS NULL AND p.is_active
    GROUP BY d.ancestor_id
),
paths AS (
    SELECT c.id, lpad(c.sort_order::text, 10, '0') || ':' || c.slug AS sort_path
    FROM active_cats c
    WHERE c.parent_id IS NULL OR c.parent_id NOT IN (SELECT id FROM active_cats)

    UNION ALL

    SELECT c.id, p.sort_path || '/' || lpad(c.sort_order::text, 10, '0') || ':' || c.slug
    FROM active_cats c
    JOIN paths p ON c.parent_id = p.id
)
SELECT
    c.id, c.shop_id, c.parent_id, c.slug, c.sort_order, c.is_active, c.image_id, c.created_at, c.updated_at,
    COALESCE(t.locale, '') AS locale_used,
    COALESCE(t.name, '') AS name,
    t.description,
    parent.slug AS parent_slug,
    -- Same '' sentinel as name/locale_used above (sqlc infers a plain
    -- non-nullable string here, not *string, the same LATERAL-join
    -- quirk); the handler only surfaces parentName when parent_slug is
    -- non-nil, so a root category's '' here is never observed as a real
    -- value — see handler.go.
    COALESCE(pt.name, '') AS parent_name,
    COALESCE(pc.product_count, 0)::bigint AS product_count
FROM categories c
JOIN active_cats a ON a.id = c.id
JOIN paths pa ON pa.id = c.id
LEFT JOIN categories parent ON parent.id = c.parent_id AND parent.shop_id = c.shop_id
    AND parent.deleted_at IS NULL AND parent.is_active
LEFT JOIN LATERAL (
    SELECT ct.locale, ct.name, ct.description
    FROM category_translations ct
    WHERE ct.category_id = c.id
    ORDER BY
        CASE
            WHEN ct.locale = sqlc.arg('locale') THEN 0
            WHEN ct.locale = 'uz' THEN 1
            ELSE 2
        END
    LIMIT 1
) t ON true
LEFT JOIN LATERAL (
    SELECT ct.name
    FROM category_translations ct
    WHERE ct.category_id = parent.id
    ORDER BY
        CASE
            WHEN ct.locale = sqlc.arg('locale') THEN 0
            WHEN ct.locale = 'uz' THEN 1
            ELSE 2
        END
    LIMIT 1
) pt ON true
LEFT JOIN product_counts pc ON pc.category_id = c.id
WHERE c.shop_id = sqlc.arg('shop_id')
ORDER BY pa.sort_path;

-- name: PublicCategoryExists :one
-- T3 review round 3, MAJOR (c): public.CacheMiddleware's admission
-- control for GET /public/products?category= — a slug that names no
-- active, non-deleted category in this shop must never be cached (an
-- attacker sweeping many nonexistent slugs would otherwise mint one
-- cache entry per slug, all empty 200s). "Exists" here means the exact
-- same active/non-deleted visibility ListPublicCategories/
-- ListPublicProducts already apply — a real but inactive category counts
-- as not existing, the same as it already does everywhere else on this
-- surface (O-22).
SELECT EXISTS (
    SELECT 1 FROM categories
    WHERE shop_id = sqlc.arg('shop_id')
        AND slug = sqlc.arg('slug')
        AND deleted_at IS NULL
        AND is_active
) AS exists;
