-- name: ListProductImages :many
-- Joined with media_files so the caller gets storage_key/mime/dimensions
-- without a second round trip per image.
SELECT
    pi.id, pi.shop_id, pi.product_id, pi.variant_id, pi.media_id,
    pi.sort_order, pi.is_cover, pi.created_at, pi.updated_at,
    mf.storage_key, mf.mime, mf.size_bytes, mf.width, mf.height
FROM product_images pi
JOIN media_files mf ON mf.id = pi.media_id
WHERE pi.shop_id = $1 AND pi.product_id = $2
ORDER BY pi.sort_order, pi.id;

-- name: AddProductImage :one
INSERT INTO product_images (id, shop_id, product_id, variant_id, media_id, sort_order, is_cover)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: RemoveProductImage :exec
-- Hard delete: product_images is a join row (§ 04-DATA-MODEL.md rule 7).
DELETE FROM product_images
WHERE shop_id = $1 AND id = $2;

-- name: CountProductImages :one
-- Enforces the "up to 8 images per product" limit (D-34) in the service.
SELECT count(*) FROM product_images
WHERE product_id = $1;

-- name: ClearCover :exec
-- Run before SetCover in the same transaction so the partial unique index
-- on (product_id) WHERE is_cover is never violated — same pattern as
-- ClearDefaultLocation before UpdateLocation(..., is_default = true).
UPDATE product_images
SET is_cover = false, updated_at = now()
WHERE shop_id = $1 AND product_id = $2 AND is_cover;

-- name: SetCover :exec
UPDATE product_images
SET is_cover = true, updated_at = now()
WHERE shop_id = $1 AND id = $2;

-- name: UpdateImageOrder :exec
UPDATE product_images
SET sort_order = $3, updated_at = now()
WHERE shop_id = $1 AND id = $2;

-- name: ListImageIDsForProduct :many
-- Used by the service to validate that a reorder request names exactly the
-- product's current image set before applying UpdateImageOrder per id.
SELECT id FROM product_images
WHERE shop_id = $1 AND product_id = $2;
