-- name: SalesSummaryForStaff :one
-- Counts and sums split by kind over [from, to) — from/to are timestamptz
-- bounds computed by the service (§ 04-DATA-MODEL.md rule 9: shop
-- timezone is applied there, not in SQL). The "sf" CTE filters the sales
-- header once; a second CTE aggregates cost from sale_items separately so
-- joining items never fans out the header-level sums (revenue, refunds,
-- discounts) — a naive single JOIN would multiply each sale's total by
-- its item count. cost = cost of sold items minus cost of returned items
-- (net cost of goods actually kept by customers); staff only (§
-- 04-DATA-MODEL.md rule 8) — see SalesSummaryForCashier for the cashier
-- equivalent with no cost column.
WITH sf AS (
    SELECT * FROM sales sales_row
    WHERE sales_row.shop_id = sqlc.arg('shop_id')
        AND sales_row.status = 'completed'
        AND sales_row.completed_at >= sqlc.arg('from')
        AND sales_row.completed_at < sqlc.arg('to')
        AND (sqlc.narg('location_id')::uuid IS NULL OR sales_row.location_id = sqlc.narg('location_id'))
        AND (sqlc.narg('cashier_id')::uuid IS NULL OR sales_row.cashier_id = sqlc.narg('cashier_id'))
),
sale_cost AS (
    SELECT sf.kind, sum(i.qty * i.unit_cost) AS total_cost
    FROM sf
    JOIN sale_items i ON i.sale_id = sf.id AND i.shop_id = sf.shop_id
    GROUP BY sf.kind
)
SELECT
    count(*) FILTER (WHERE kind = 'sale')::bigint AS sales_count,
    count(*) FILTER (WHERE kind = 'return')::bigint AS returns_count,
    COALESCE(sum(total) FILTER (WHERE kind = 'sale'), 0)::numeric(14,2) AS revenue,
    COALESCE(sum(total) FILTER (WHERE kind = 'return'), 0)::numeric(14,2) AS refunds,
    -- Sale leg only (D-64): a fully returned discounted sale still reports
    -- the discount it gave at checkout, it is not netted back out here —
    -- SalesByProduct's per-line net revenue is what already reflects the
    -- return's effect on discount-adjusted revenue.
    COALESCE(sum(discount_amount) FILTER (WHERE kind = 'sale'), 0)::numeric(14,2) AS discounts,
    (
        COALESCE((SELECT total_cost FROM sale_cost WHERE kind = 'sale'), 0)
        - COALESCE((SELECT total_cost FROM sale_cost WHERE kind = 'return'), 0)
    )::numeric(14,2) AS cost
FROM sf;

-- name: SalesSummaryForCashier :one
-- Same filters as SalesSummaryForStaff, no cost column (§ 04-DATA-MODEL.md
-- rule 8, D-63) — the permission matrix's "own-day sales only" for a
-- cashier is enforced by the service passing from/to and cashier_id, not
-- by this query.
WITH sf AS (
    SELECT * FROM sales
    WHERE shop_id = sqlc.arg('shop_id')
        AND status = 'completed'
        AND completed_at >= sqlc.arg('from')
        AND completed_at < sqlc.arg('to')
        AND (sqlc.narg('location_id')::uuid IS NULL OR location_id = sqlc.narg('location_id'))
        AND (sqlc.narg('cashier_id')::uuid IS NULL OR cashier_id = sqlc.narg('cashier_id'))
)
SELECT
    count(*) FILTER (WHERE kind = 'sale')::bigint AS sales_count,
    count(*) FILTER (WHERE kind = 'return')::bigint AS returns_count,
    COALESCE(sum(total) FILTER (WHERE kind = 'sale'), 0)::numeric(14,2) AS revenue,
    COALESCE(sum(total) FILTER (WHERE kind = 'return'), 0)::numeric(14,2) AS refunds,
    COALESCE(sum(discount_amount) FILTER (WHERE kind = 'sale'), 0)::numeric(14,2) AS discounts
FROM sf;

-- name: SalesByProduct :many
-- Staff only (unit_cost, § 04-DATA-MODEL.md rule 8). Groups sale_items by
-- product over [from, to), computing quantity sold/returned and net
-- revenue/cost (sold minus returned).
--
-- net_line_revenue nets each sale-leg line's own share of its sale's
-- discount_amount so the sum reconciles exactly (owner ruling amending
-- D-64): every line except the last one (in sale_items.id order — uuid
-- v7, so insertion order) gets round(line_total * discount_amount /
-- subtotal, 2); the last line gets whatever is left over
-- (discount_amount minus the sum of the other lines' rounded shares),
-- which absorbs the rounding remainder so the per-line shares always add
-- up to exactly discount_amount, never a cent more or less. "raw" below
-- computes each line's row_number()/count() and its own rounded share
-- (raw_share) within its sale (partition by sale_id, order by item_id);
-- "items" then picks, per line, either that share (not the last line) or
-- discount_amount minus the running sum of every other line's share (the
-- last line — the window frame UNBOUNDED PRECEDING TO 1 PRECEDING is
-- exactly "every row before this one", which for the last row in the
-- partition is every other row). subtotal = 0 is guarded explicitly (a
-- line must never be dropped, not divided by zero); the sales service
-- applies this same allocation for D-61 refunds, this query only mirrors
-- it for the report — a return-leg line's line_total is already net (the
-- service computed it that way), so it is used as-is, no share to
-- compute. Keyset pagination on (revenue, product_id), both DESC — same
-- tuple-comparison shape as every other cursor here, just with a
-- computed sort key instead of a column, which is why the ordering and
-- the aggregation are split into their own CTEs (a WHERE clause cannot
-- filter on an aggregate directly).
WITH lines AS (
    SELECT
        i.id AS item_id,
        i.sale_id,
        p.id AS product_id,
        COALESCE(t.name, '') AS product_name,
        i.qty, i.unit_cost, i.line_total, s.kind, s.subtotal, s.discount_amount,
        row_number() OVER (PARTITION BY i.sale_id ORDER BY i.id) AS rn,
        count(*) OVER (PARTITION BY i.sale_id) AS line_count,
        -- 0 for a return leg or a zero-subtotal sale (nothing to
        -- allocate); never divides by zero.
        CASE
            WHEN s.kind = 'sale' AND s.subtotal <> 0
                THEN ROUND(i.line_total * s.discount_amount / s.subtotal, 2)
            ELSE 0
        END AS raw_share
    FROM sale_items i
    JOIN sales s ON s.id = i.sale_id AND s.shop_id = i.shop_id
    JOIN product_variants v ON v.id = i.variant_id AND v.shop_id = i.shop_id
    JOIN products p ON p.id = v.product_id AND p.shop_id = i.shop_id
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
    WHERE i.shop_id = sqlc.arg('shop_id')
        AND s.status = 'completed'
        AND s.completed_at >= sqlc.arg('from')
        AND s.completed_at < sqlc.arg('to')
        AND (sqlc.narg('location_id')::uuid IS NULL OR s.location_id = sqlc.narg('location_id'))
),
items AS (
    SELECT
        product_id, product_name, qty, unit_cost, kind,
        CASE
            WHEN kind <> 'sale' THEN line_total
            WHEN subtotal = 0 THEN line_total
            WHEN rn = line_count THEN
                line_total - (discount_amount - COALESCE(
                    SUM(raw_share) OVER (
                        PARTITION BY sale_id ORDER BY item_id
                        ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING
                    ), 0
                ))
            ELSE line_total - raw_share
        END AS net_line_revenue
    FROM lines
),
agg AS (
    SELECT
        product_id,
        max(product_name)::text AS product_name,
        COALESCE(sum(qty) FILTER (WHERE kind = 'sale'), 0)::numeric(12,3) AS qty_sold,
        COALESCE(sum(qty) FILTER (WHERE kind = 'return'), 0)::numeric(12,3) AS qty_returned,
        (
            COALESCE(sum(net_line_revenue) FILTER (WHERE kind = 'sale'), 0)
            - COALESCE(sum(net_line_revenue) FILTER (WHERE kind = 'return'), 0)
        )::numeric(14,2) AS revenue,
        (
            COALESCE(sum(qty * unit_cost) FILTER (WHERE kind = 'sale'), 0)
            - COALESCE(sum(qty * unit_cost) FILTER (WHERE kind = 'return'), 0)
        )::numeric(14,2) AS cost
    FROM items
    GROUP BY product_id
)
SELECT *
FROM agg
WHERE (
    sqlc.narg('cursor_revenue')::numeric IS NULL
    OR (revenue, product_id) < (sqlc.narg('cursor_revenue')::numeric, sqlc.narg('cursor_product_id')::uuid)
)
ORDER BY revenue DESC, product_id DESC
LIMIT sqlc.arg('limit');
