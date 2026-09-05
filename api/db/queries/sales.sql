-- name: NextSaleNumber :one
-- Row-locking increment, same pattern as NextPurchaseNumber
-- (purchases.sql, D-45): the UPDATE itself takes the row lock, so no
-- separate SELECT ... FOR UPDATE is needed. Returns the number this sale
-- should use (before the increment); never client-supplied (hard rule 8).
UPDATE shops
SET next_sale_number = next_sale_number + 1
WHERE id = $1
RETURNING (next_sale_number - 1)::bigint;

-- name: InsertSale :one
-- The sale header, written once and complete (no draft state — unlike
-- purchases). number always comes from NextSaleNumber. subtotal/total are
-- computed server-side from the items about to be inserted, never trusted
-- from the client (hard rule 8).
INSERT INTO sales (
    id, shop_id, number, kind, location_id, customer_id, cashier_id,
    original_sale_id, subtotal, discount_amount, discount_reason, total, note
)
VALUES (
    sqlc.arg('id'), sqlc.arg('shop_id'), sqlc.arg('number'), sqlc.arg('kind'),
    sqlc.arg('location_id'), sqlc.narg('customer_id'), sqlc.arg('cashier_id'),
    sqlc.narg('original_sale_id'), sqlc.arg('subtotal'), sqlc.arg('discount_amount'),
    sqlc.narg('discount_reason'), sqlc.arg('total'), sqlc.narg('note')
)
RETURNING *;

-- name: InsertSaleItem :one
-- unit_price/unit_cost are frozen at sale time by the service (catalogue
-- price/promo, cost_price or cost_override) — never client-supplied.
-- original_sale_item_id is set only for a return row (D-61).
INSERT INTO sale_items (
    id, shop_id, sale_id, variant_id, qty, unit_price, unit_cost, line_total, original_sale_item_id
)
VALUES (
    sqlc.arg('id'), sqlc.arg('shop_id'), sqlc.arg('sale_id'), sqlc.arg('variant_id'),
    sqlc.arg('qty'), sqlc.arg('unit_price'), sqlc.arg('unit_cost'), sqlc.arg('line_total'),
    sqlc.narg('original_sale_item_id')
)
RETURNING *;

-- name: InsertSalePayment :one
INSERT INTO sale_payments (id, shop_id, sale_id, method, amount)
VALUES (sqlc.arg('id'), sqlc.arg('shop_id'), sqlc.arg('sale_id'), sqlc.arg('method'), sqlc.arg('amount'))
RETURNING *;

-- name: GetSaleForUpdate :one
-- Locks the header row before a void or a return-against-it, so two
-- concurrent requests cannot both observe status = 'completed' and race
-- (mirrors GetPurchaseForUpdate).
SELECT * FROM sales
WHERE shop_id = $1 AND id = $2
FOR UPDATE;

-- name: GetSaleItemsForUpdate :many
-- Locks the original sale's item rows before computing "already returned
-- per line" for a new partial return, so two concurrent partial returns
-- against the same sale cannot both under-count what was already
-- refunded.
SELECT * FROM sale_items
WHERE shop_id = $1 AND sale_id = $2
ORDER BY created_at, id
FOR UPDATE;

-- name: CountCompletedReturnsForSale :one
-- D-62: a sale with an existing completed return cannot be voided; the
-- service checks this before calling VoidSale.
SELECT count(*) FROM sales
WHERE shop_id = $1 AND original_sale_id = $2 AND kind = 'return' AND status = 'completed';

-- name: VoidSale :one
-- The only UPDATE ever allowed on a sale (ADR-014): the four void
-- columns, and only from status = 'completed' AND kind = 'sale' — D-66:
-- returns are final and cannot be voided; a wrong return is corrected by
-- a new sale, not a void. The `kind = 'sale'` predicate here is this
-- query's own guard, not backed by any DB-level constraint — the
-- sales_immutable trigger does not look at kind at all (it only checks
-- the completed -> voided status transition and which columns changed),
-- so nothing in the schema stops a return from being voided by a
-- different code path; the service must not call this query, or any
-- other write, on a return for a void. A second void or a void of a
-- return-kind row returns no row; the service maps that to
-- SALE_NOT_VOIDABLE.
UPDATE sales
SET status = 'voided', voided_at = now(), voided_by = sqlc.arg('voided_by'), void_reason = sqlc.narg('void_reason')
WHERE shop_id = sqlc.arg('shop_id') AND id = sqlc.arg('id') AND status = 'completed' AND kind = 'sale'
RETURNING *;

-- name: GetSaleForStaff :one
-- has_returns backs D-62 (a sale with an existing return cannot be
-- voided) without a second round trip. payment_method/payment_amount
-- come from the sale's one payment (D-54, sale_payments unique(sale_id));
-- LEFT JOIN so a header still resolves even in the — currently
-- impossible, but defensive — case a payment row is missing.
SELECT
    s.*,
    l.name AS location_name,
    c.full_name AS customer_name,
    u.full_name AS cashier_name,
    p.method AS payment_method,
    p.amount AS payment_amount,
    EXISTS (
        SELECT 1 FROM sales r
        WHERE r.shop_id = s.shop_id AND r.original_sale_id = s.id
            AND r.kind = 'return' AND r.status = 'completed'
    ) AS has_returns
FROM sales s
JOIN locations l ON l.id = s.location_id
LEFT JOIN customers c ON c.id = s.customer_id
JOIN users u ON u.id = s.cashier_id
LEFT JOIN sale_payments p ON p.sale_id = s.id
WHERE s.shop_id = $1 AND s.id = $2;

-- name: GetSaleForCashier :one
-- Same shape as GetSaleForStaff — the sales header carries no cost/margin
-- column, so there is nothing to strip today (D-63). Kept as its own
-- query, not a shared one filtered in Go, so a future header-level cost
-- column cannot leak into the cashier path by accident (§ 04-DATA-MODEL.md
-- rule 8).
SELECT
    s.*,
    l.name AS location_name,
    c.full_name AS customer_name,
    u.full_name AS cashier_name,
    p.method AS payment_method,
    p.amount AS payment_amount,
    EXISTS (
        SELECT 1 FROM sales r
        WHERE r.shop_id = s.shop_id AND r.original_sale_id = s.id
            AND r.kind = 'return' AND r.status = 'completed'
    ) AS has_returns
FROM sales s
JOIN locations l ON l.id = s.location_id
LEFT JOIN customers c ON c.id = s.customer_id
JOIN users u ON u.id = s.cashier_id
LEFT JOIN sale_payments p ON p.sale_id = s.id
WHERE s.shop_id = $1 AND s.id = $2;

-- name: ListSalesForStaff :many
-- Keyset pagination on (completed_at, id), newest first, same convention
-- as ListMovements/ListPurchases. from/to are timestamptz bounds computed
-- by the service (e.g. a shop-timezone calendar day, § 04-DATA-MODEL.md
-- rule 9: timestamps are UTC, the shop timezone is applied in the
-- service/reports layer, not here). location/cashier/customer/kind/status
-- are optional exact-match filters.
SELECT
    s.*,
    l.name AS location_name,
    c.full_name AS customer_name,
    u.full_name AS cashier_name
FROM sales s
JOIN locations l ON l.id = s.location_id
LEFT JOIN customers c ON c.id = s.customer_id
JOIN users u ON u.id = s.cashier_id
WHERE s.shop_id = sqlc.arg('shop_id')
    AND (sqlc.narg('from')::timestamptz IS NULL OR s.completed_at >= sqlc.narg('from'))
    AND (sqlc.narg('to')::timestamptz IS NULL OR s.completed_at < sqlc.narg('to'))
    AND (sqlc.narg('location_id')::uuid IS NULL OR s.location_id = sqlc.narg('location_id'))
    AND (sqlc.narg('cashier_id')::uuid IS NULL OR s.cashier_id = sqlc.narg('cashier_id'))
    AND (sqlc.narg('customer_id')::uuid IS NULL OR s.customer_id = sqlc.narg('customer_id'))
    AND (sqlc.narg('kind')::sale_kind IS NULL OR s.kind = sqlc.narg('kind'))
    AND (sqlc.narg('status')::sale_status IS NULL OR s.status = sqlc.narg('status'))
    AND (
        sqlc.narg('cursor_completed_at')::timestamptz IS NULL
        OR (s.completed_at, s.id) < (sqlc.narg('cursor_completed_at')::timestamptz, sqlc.narg('cursor_id')::uuid)
    )
ORDER BY s.completed_at DESC, s.id DESC
LIMIT sqlc.arg('limit');

-- name: ListSalesForCashier :many
-- Same shape as ListSalesForStaff (D-63): the sales header carries no
-- cost/margin column, kept as its own query for the same reason as
-- GetSaleForCashier above.
SELECT
    s.*,
    l.name AS location_name,
    c.full_name AS customer_name,
    u.full_name AS cashier_name
FROM sales s
JOIN locations l ON l.id = s.location_id
LEFT JOIN customers c ON c.id = s.customer_id
JOIN users u ON u.id = s.cashier_id
WHERE s.shop_id = sqlc.arg('shop_id')
    AND (sqlc.narg('from')::timestamptz IS NULL OR s.completed_at >= sqlc.narg('from'))
    AND (sqlc.narg('to')::timestamptz IS NULL OR s.completed_at < sqlc.narg('to'))
    AND (sqlc.narg('location_id')::uuid IS NULL OR s.location_id = sqlc.narg('location_id'))
    AND (sqlc.narg('cashier_id')::uuid IS NULL OR s.cashier_id = sqlc.narg('cashier_id'))
    AND (sqlc.narg('customer_id')::uuid IS NULL OR s.customer_id = sqlc.narg('customer_id'))
    AND (sqlc.narg('kind')::sale_kind IS NULL OR s.kind = sqlc.narg('kind'))
    AND (sqlc.narg('status')::sale_status IS NULL OR s.status = sqlc.narg('status'))
    AND (
        sqlc.narg('cursor_completed_at')::timestamptz IS NULL
        OR (s.completed_at, s.id) < (sqlc.narg('cursor_completed_at')::timestamptz, sqlc.narg('cursor_id')::uuid)
    )
ORDER BY s.completed_at DESC, s.id DESC
LIMIT sqlc.arg('limit');

-- name: ListSaleItemsForStaff :many
-- Mirrors ListPurchaseItemsWithLabels (purchases.sql): joins each
-- sale_items row to its variant and product for the response-only
-- productName/variantLabel/sku fields, with the same locale fallback
-- (requested -> 'uz' -> any, ADR-012). returned_qty sums the qty of
-- return-kind items whose original_sale_item_id points back at this row,
-- counting only completed returns (a voided return never happened).
-- unit_cost is included — staff only (§ 04-DATA-MODEL.md rule 8).
SELECT
    si.id, si.sale_id, si.variant_id, si.qty, si.unit_price, si.unit_cost, si.line_total,
    si.original_sale_item_id, si.created_at,
    v.sku AS variant_sku,
    v.attributes AS variant_attributes,
    COALESCE(t.locale, '') AS locale_used,
    COALESCE(t.name, '') AS product_name,
    COALESCE(r.returned_qty, 0)::numeric(12,3) AS returned_qty
FROM sale_items si
JOIN product_variants v ON v.id = si.variant_id AND v.shop_id = si.shop_id
JOIN products p ON p.id = v.product_id AND p.shop_id = si.shop_id
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
LEFT JOIN LATERAL (
    SELECT sum(ri.qty) AS returned_qty
    FROM sale_items ri
    JOIN sales rs ON rs.id = ri.sale_id
    WHERE ri.shop_id = si.shop_id AND ri.original_sale_item_id = si.id AND rs.shop_id = si.shop_id AND rs.kind = 'return' AND rs.status = 'completed'
) r ON true
WHERE si.shop_id = sqlc.arg('shop_id') AND si.sale_id = sqlc.arg('sale_id')
ORDER BY si.created_at, si.id;

-- name: ListSaleItemsForCashier :many
-- Same as ListSaleItemsForStaff, minus unit_cost (§ 04-DATA-MODEL.md rule
-- 8, D-63): a separate query, not the staff one filtered in Go.
SELECT
    si.id, si.sale_id, si.variant_id, si.qty, si.unit_price, si.line_total,
    si.original_sale_item_id, si.created_at,
    v.sku AS variant_sku,
    v.attributes AS variant_attributes,
    COALESCE(t.locale, '') AS locale_used,
    COALESCE(t.name, '') AS product_name,
    COALESCE(r.returned_qty, 0)::numeric(12,3) AS returned_qty
FROM sale_items si
JOIN product_variants v ON v.id = si.variant_id AND v.shop_id = si.shop_id
JOIN products p ON p.id = v.product_id AND p.shop_id = si.shop_id
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
LEFT JOIN LATERAL (
    SELECT sum(ri.qty) AS returned_qty
    FROM sale_items ri
    JOIN sales rs ON rs.id = ri.sale_id
    WHERE ri.shop_id = si.shop_id AND ri.original_sale_item_id = si.id AND rs.shop_id = si.shop_id AND rs.kind = 'return' AND rs.status = 'completed'
) r ON true
WHERE si.shop_id = sqlc.arg('shop_id') AND si.sale_id = sqlc.arg('sale_id')
ORDER BY si.created_at, si.id;
