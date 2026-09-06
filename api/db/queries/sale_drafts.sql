-- name: CreateSaleDraft :one
-- The draft header. Written whole at POST time (unlike purchases' own
-- draft state, a sale draft has no separate "create empty, add items
-- later" step in the API — POST /sales/drafts takes the full body,
-- D-87) — discount_type/discount_value/discount_reason/customer_id/note
-- are all optional and simply narg here, no clear-flag dance (that is
-- only needed for the PATCH path, UpdateSaleDraft below). No price, no
-- sale number (D-87): those exist only once the draft completes.
INSERT INTO sale_drafts (
    id, shop_id, location_id, customer_id,
    discount_type, discount_value, discount_reason, note, created_by
)
VALUES (
    sqlc.arg('id'), sqlc.arg('shop_id'), sqlc.arg('location_id'), sqlc.narg('customer_id'),
    sqlc.narg('discount_type'), sqlc.narg('discount_value'), sqlc.narg('discount_reason'),
    sqlc.narg('note'), sqlc.narg('created_by')
)
RETURNING *;

-- name: InsertSaleDraftItem :one
-- One line. No price/cost column (D-87) — qty and position only; the
-- service assigns position (line order) as it inserts each line.
INSERT INTO sale_draft_items (id, shop_id, draft_id, variant_id, qty, position)
VALUES (sqlc.arg('id'), sqlc.arg('shop_id'), sqlc.arg('draft_id'), sqlc.arg('variant_id'), sqlc.arg('qty'), sqlc.arg('position'))
RETURNING *;

-- name: GetSaleDraft :one
SELECT * FROM sale_drafts
WHERE shop_id = $1 AND id = $2;

-- name: GetSaleDraftForUpdate :one
-- Locks the header row before the complete flow (D-87: completion
-- creates the sale via sales.Service and deletes the draft in the same
-- transaction), so two concurrent completions of the same draft cannot
-- both proceed — mirrors GetSaleForUpdate/GetPurchaseForUpdate.
SELECT * FROM sale_drafts
WHERE shop_id = $1 AND id = $2
FOR UPDATE;

-- name: ListSaleDrafts :many
-- Keyset pagination on (created_at, id), newest first, same convention as
-- ListSalesForStaff/ListPurchases. created_by is an optional exact-match
-- filter (GET /sales/drafts?createdBy=..., § 05-API.md).
SELECT * FROM sale_drafts
WHERE shop_id = sqlc.arg('shop_id')
    AND (sqlc.narg('created_by')::uuid IS NULL OR created_by = sqlc.narg('created_by'))
    AND (
        sqlc.narg('cursor_created_at')::timestamptz IS NULL
        OR (created_at, id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::uuid)
    )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('limit');

-- name: ListSaleDraftItems :many
-- Ordered by position (line order), `, id` tiebreaker for determinism if
-- two lines ever share a position (same reasoning as
-- ListPurchaseItems/ListVariantsForStaff's own tiebreakers).
SELECT * FROM sale_draft_items
WHERE shop_id = $1 AND draft_id = $2
ORDER BY position, id;

-- name: UpdateSaleDraft :one
-- Patch, same clear-flag shape as UpdatePurchaseHeader/UpdateProduct:
-- COALESCE cannot express "set to NULL". clear_customer clears
-- customer_id alone; clear_discount clears discount_type AND
-- discount_value together (they are one concept, same reasoning as
-- UpdateProduct's clear_promo) — discount_reason has its own
-- clear_discount_reason since a reason can outlive or be cleared
-- independently of the discount amount itself. items are not touched
-- here (PATCH .../drafts/{id} replaces the whole line set separately via
-- DeleteSaleDraftItems + InsertSaleDraftItem, same pattern as
-- purchase_items).
UPDATE sale_drafts
SET
    location_id = COALESCE(sqlc.narg('location_id'), location_id),
    customer_id = CASE WHEN sqlc.arg('clear_customer')::bool THEN NULL ELSE COALESCE(sqlc.narg('customer_id'), customer_id) END,
    discount_type = CASE WHEN sqlc.arg('clear_discount')::bool THEN NULL ELSE COALESCE(sqlc.narg('discount_type'), discount_type) END,
    discount_value = CASE WHEN sqlc.arg('clear_discount')::bool THEN NULL ELSE COALESCE(sqlc.narg('discount_value'), discount_value) END,
    discount_reason = CASE WHEN sqlc.arg('clear_discount_reason')::bool THEN NULL ELSE COALESCE(sqlc.narg('discount_reason'), discount_reason) END,
    note = CASE WHEN sqlc.arg('clear_note')::bool THEN NULL ELSE COALESCE(sqlc.narg('note'), note) END,
    updated_at = now()
WHERE shop_id = sqlc.arg('shop_id') AND id = sqlc.arg('id')
RETURNING *;

-- name: DeleteSaleDraftItems :execrows
-- Join rows, hard-deleted (§ 04-DATA-MODEL.md rule 7); used by the
-- service to replace a draft's whole line set on PATCH (delete then
-- re-InsertSaleDraftItem), same pattern as DeletePurchaseItems. Unlike
-- DeletePurchaseItems there is no status guard here — a draft has no
-- status, it is editable until it is completed or deleted.
DELETE FROM sale_draft_items
WHERE shop_id = $1 AND draft_id = $2;

-- name: DeleteSaleDraft :execrows
-- Hard delete (D-89: drafts are hard-deleted, no ledger effect). The
-- ON DELETE CASCADE on sale_draft_items.draft_id (0017_sale_drafts.sql)
-- removes its lines in the same statement; DeleteSaleDraftItems above
-- exists only for the PATCH replace-items path, not for this delete.
DELETE FROM sale_drafts
WHERE shop_id = $1 AND id = $2;
