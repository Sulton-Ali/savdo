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
