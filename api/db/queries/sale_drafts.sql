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
INSERT INTO sale_draft_items (id, shop_id, sale_draft_id, variant_id, qty, position)
VALUES (sqlc.arg('id'), sqlc.arg('shop_id'), sqlc.arg('sale_draft_id'), sqlc.arg('variant_id'), sqlc.arg('qty'), sqlc.arg('position'))
RETURNING *;

-- name: GetSaleDraft :one
-- LEFT JOIN, not JOIN: created_by/customer_id are both nullable (D-89's
-- "no creator on record" case; a draft with no customer attached) and
-- either referenced row could in principle be gone later — every case
-- still returns the draft, with created_by_name/customer_name simply
-- NULL, same reasoning as ListMovementsWithCreatedByName (stock.sql).
-- Resolving both names here avoids a second lookup by the admin/mobile
-- client. customers has no composite (id, shop_id) FK from sale_drafts
-- (customer_id REFERENCES customers(id) alone, 0017_sale_drafts.sql), so
-- c.shop_id = sd.shop_id is this join's own guard against ever resolving
-- another shop's customer's name (hard rule 1) — Sale.customerName's own
-- join (sales.sql) predates this guard and is unscoped; not touched here,
-- out of this task's scope.
SELECT sd.*, u.full_name AS created_by_name, c.full_name AS customer_name
FROM sale_drafts sd
LEFT JOIN users u ON u.id = sd.created_by AND u.shop_id = sd.shop_id
LEFT JOIN customers c ON c.id = sd.customer_id AND c.shop_id = sd.shop_id
WHERE sd.shop_id = $1 AND sd.id = $2;

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
-- filter (GET /sales/drafts?createdBy=..., § 05-API.md). LEFT JOIN users/
-- customers for created_by_name/customer_name, same one-query-per-page
-- shape ListMovementsWithCreatedByName (stock.sql) and ListSalesForStaff's
-- own cashier_name/customer_name joins already use — a page's worth of
-- names in this same query, never a lookup per row (no N+1). The
-- customers join is shop-scoped (c.shop_id = sd.shop_id) for the same
-- reason GetSaleDraft's own join above is (hard rule 1).
SELECT sd.*, u.full_name AS created_by_name, c.full_name AS customer_name
FROM sale_drafts sd
LEFT JOIN users u ON u.id = sd.created_by AND u.shop_id = sd.shop_id
LEFT JOIN customers c ON c.id = sd.customer_id AND c.shop_id = sd.shop_id
WHERE sd.shop_id = sqlc.arg('shop_id')
    AND (sqlc.narg('created_by')::uuid IS NULL OR sd.created_by = sqlc.narg('created_by'))
    AND (
        sqlc.narg('cursor_created_at')::timestamptz IS NULL
        OR (sd.created_at, sd.id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::uuid)
    )
ORDER BY sd.created_at DESC, sd.id DESC
LIMIT sqlc.arg('limit');

-- name: ListSaleDraftItems :many
-- Ordered by position (line order), `, id` tiebreaker for determinism if
-- two lines ever share a position (same reasoning as
-- ListPurchaseItems/ListVariantsForStaff's own tiebreakers).
SELECT * FROM sale_draft_items
WHERE shop_id = $1 AND sale_draft_id = $2
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
WHERE shop_id = $1 AND sale_draft_id = $2;

-- name: DeleteSaleDraft :execrows
-- Hard delete (D-89: drafts are hard-deleted, no ledger effect). The
-- ON DELETE CASCADE on sale_draft_items.sale_draft_id (0017_sale_drafts.sql)
-- removes its lines in the same statement; DeleteSaleDraftItems above
-- exists only for the PATCH replace-items path, not for this delete.
DELETE FROM sale_drafts
WHERE shop_id = $1 AND id = $2;

-- name: ListSaleDraftItemsForPricing :many
-- Batched replacement for a per-line GetVariantForCashier +
-- GetProductForCashier round trip (review MAJOR: N+1 in ListSaleDrafts):
-- one query prices every line of every draft named in sale_draft_ids at
-- once, the same ANY($ids) batching ListCoverImagesForProducts already
-- uses for D-83. Cost-free (product_variants/products columns only, no
-- cost_price/cost_override) — a draft never exposes cost to any role
-- (§ 04-DATA-MODEL.md rule 8). Plain (not LEFT) JOINs are safe here: the
-- NOT NULL FK on both product_variants.id and products.id blocks a hard
-- delete while a draft line still references them (rule 7), so a row
-- always resolves even after a soft delete — variant_available/
-- product_available surface that state (deleted_at IS NULL AND
-- is_active) instead of a missing row, so the service can render the
-- line with available: false rather than erroring (review CRITICAL:
-- priceDraftItems must not 500 on an inactive/soft-deleted line).
SELECT
    sdi.sale_draft_id, sdi.id, sdi.variant_id, sdi.qty, sdi.position,
    pv.sku AS variant_sku, pv.attributes AS variant_attributes, pv.price_override,
    (pv.is_active AND pv.deleted_at IS NULL) AS variant_available,
    p.id AS product_id,
    (p.is_active AND p.deleted_at IS NULL) AS product_available,
    p.base_price, p.promo_price, p.promo_from, p.promo_to,
    COALESCE(t.name, '') AS product_name
FROM sale_draft_items sdi
JOIN product_variants pv ON pv.id = sdi.variant_id AND pv.shop_id = sdi.shop_id
JOIN products p ON p.id = pv.product_id AND p.shop_id = sdi.shop_id
LEFT JOIN LATERAL (
    SELECT pt.name
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
WHERE sdi.shop_id = sqlc.arg('shop_id') AND sdi.sale_draft_id = ANY(sqlc.arg('sale_draft_ids')::uuid[])
ORDER BY sdi.sale_draft_id, sdi.position, sdi.id;
