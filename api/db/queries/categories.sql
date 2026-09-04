-- name: ListCategories :many
-- Flat list (the service builds the tree from parent_id); resolved name by
-- locale fallback (requested -> 'uz' -> any); include_inactive toggles
-- whether inactive categories are returned (the admin category picker wants
-- them, the public/cashier catalog does not).
SELECT
    c.*,
    t.locale AS locale_used,
    t.name AS name
FROM categories c
LEFT JOIN LATERAL (
    SELECT ct.locale, ct.name
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
SELECT * FROM categories
WHERE shop_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: CreateCategory :one
INSERT INTO categories (id, shop_id, parent_id, slug, sort_order, is_active, image_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateCategory :one
-- Patch: parent_id and image_id cannot be cleared through COALESCE (there is
-- no "clear" flag for them here because re-parenting to NULL or removing the
-- category image are not exposed as edits in Phase 2 — only setting them to
-- a new value is). slug/sort_order/is_active are plain optional fields.
UPDATE categories
SET
    parent_id = COALESCE(sqlc.narg('parent_id'), parent_id),
    slug = COALESCE(sqlc.narg('slug'), slug),
    sort_order = COALESCE(sqlc.narg('sort_order'), sort_order),
    is_active = COALESCE(sqlc.narg('is_active'), is_active),
    image_id = COALESCE(sqlc.narg('image_id'), image_id),
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
WITH RECURSIVE ancestors AS (
    SELECT cat.id, cat.parent_id, 1 AS depth
    FROM categories cat
    WHERE cat.id = sqlc.arg('category_id') AND cat.shop_id = sqlc.arg('shop_id')

    UNION ALL

    SELECT c.id, c.parent_id, a.depth + 1
    FROM categories c
    JOIN ancestors a ON c.id = a.parent_id
)
SELECT max(a.depth)::int AS depth FROM ancestors a;
