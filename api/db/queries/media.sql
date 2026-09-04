-- name: CreateMediaFile :one
INSERT INTO media_files (id, shop_id, storage_key, mime, size_bytes, width, height, sha256, uploaded_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetMediaFile :one
SELECT * FROM media_files
WHERE shop_id = $1 AND id = $2;

-- name: GetMediaFileBySHA256 :one
-- Upload dedup: the service hashes the incoming file first and looks for an
-- existing row before writing a new one to disk.
SELECT * FROM media_files
WHERE shop_id = $1 AND sha256 = $2;

-- name: DeleteMediaFile :exec
-- media_files is not in the soft-delete list (§ 04-DATA-MODEL.md rule 7) —
-- only products, variants, customers, suppliers are. The FK from
-- product_images prevents deleting a file still attached to a product.
DELETE FROM media_files
WHERE shop_id = $1 AND id = $2;
