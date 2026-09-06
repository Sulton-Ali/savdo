-- stock.Service.Move's three-step write, in order, inside one transaction:
--   1. UpsertLevelRow   — guarantee a (shop_id, variant_id, location_id) row
--                          exists, so step 2 has something to lock.
--   2. GetLevelForUpdate — SELECT ... FOR UPDATE, locks the row and reads
--                          the current qty so the service can decide
--                          whether the delta is allowed (D-41:
--                          allow_negative_stock).
--   3. ApplyLevelDelta   — the actual write.
-- ApplyLevelDelta is the ONLY statement in this codebase that writes
-- stock_levels (ADR-006, § 04-DATA-MODEL.md rule 2); every other query
-- here only reads.

-- name: UpsertLevelRow :exec
INSERT INTO stock_levels (shop_id, variant_id, location_id, qty, updated_at)
VALUES ($1, $2, $3, 0, now())
ON CONFLICT (shop_id, variant_id, location_id) DO NOTHING;

-- name: GetLevelForUpdate :one
SELECT * FROM stock_levels
WHERE shop_id = $1 AND variant_id = $2 AND location_id = $3
FOR UPDATE;

-- name: ApplyLevelDelta :one
-- THE ONLY WRITE TO stock_levels. Call only after GetLevelForUpdate has
-- locked the row and the service has approved the resulting qty.
UPDATE stock_levels
SET qty = qty + sqlc.arg('delta'), updated_at = now()
WHERE shop_id = sqlc.arg('shop_id') AND variant_id = sqlc.arg('variant_id') AND location_id = sqlc.arg('location_id')
RETURNING qty;

-- name: InsertMovement :one
-- Append-only (a trigger on stock_movements rejects UPDATE/DELETE,
-- 0012_stock_ledger.sql) — this is the only statement that ever inserts
-- here; nothing updates or deletes a row afterwards.
INSERT INTO stock_movements (
    id, shop_id, variant_id, location_id, kind, qty, unit_cost,
    ref_type, ref_id, adjustment_reason, reason, created_by
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: ListMovements :many
-- Keyset pagination on (created_at, id), newest first; variant/location/
-- kind/from/to are optional filters (sqlc.narg).
SELECT * FROM stock_movements
WHERE shop_id = sqlc.arg('shop_id')
    AND (sqlc.narg('variant_id')::uuid IS NULL OR variant_id = sqlc.narg('variant_id'))
    AND (sqlc.narg('location_id')::uuid IS NULL OR location_id = sqlc.narg('location_id'))
    AND (sqlc.narg('kind')::stock_movement_kind IS NULL OR kind = sqlc.narg('kind'))
    AND (sqlc.narg('from')::timestamptz IS NULL OR created_at >= sqlc.narg('from'))
    AND (sqlc.narg('to')::timestamptz IS NULL OR created_at <= sqlc.narg('to'))
    AND (
        sqlc.narg('cursor_created_at')::timestamptz IS NULL
        OR (created_at, id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::uuid)
    )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('limit');

-- name: ListLevels :many
-- variant/product/location are optional filters (sqlc.narg); product_id is
-- reached through product_variants since stock_levels itself has no
-- product_id column. Ordered newest variant first, then location (D-92):
-- ORDER BY pv.created_at DESC, pv.id DESC, sl.location_id. The cursor
-- encodes (variant_created_at, variant_id, location_id); the first two
-- columns sort DESC and the third sorts ASC, so a single tuple comparison
-- would silently get the third column's direction wrong — the WHERE
-- clause instead spells out "this row comes after the cursor row in that
-- order" explicitly, one OR branch per tie-break level.
SELECT sl.shop_id, sl.variant_id, sl.location_id, sl.qty, sl.updated_at, pv.product_id, pv.created_at AS variant_created_at
FROM stock_levels sl
JOIN product_variants pv ON pv.id = sl.variant_id AND pv.shop_id = sl.shop_id
WHERE sl.shop_id = sqlc.arg('shop_id')
    AND (sqlc.narg('variant_id')::uuid IS NULL OR sl.variant_id = sqlc.narg('variant_id'))
    AND (sqlc.narg('product_id')::uuid IS NULL OR pv.product_id = sqlc.narg('product_id'))
    AND (sqlc.narg('location_id')::uuid IS NULL OR sl.location_id = sqlc.narg('location_id'))
    -- Invariant: cursor_variant_created_at, cursor_variant_id and
    -- cursor_location_id are either all NULL (first page) or all set
    -- (every later page) — never a partial cursor. levelCursorPtr
    -- (api/internal/stock/pagination.go) guarantees this by only ever
    -- returning all three pointers or all three nil. A partial cursor
    -- would silently drop rows: e.g. cursor_variant_created_at set with
    -- cursor_variant_id NULL would make the IS NULL check below only
    -- gate on the first arg, and the second OR branch's
    -- `pv.id < NULL::uuid` would then be NULL (neither true nor false)
    -- for every row, so a whole tied created_at group could vanish.
    AND (
        sqlc.narg('cursor_variant_created_at')::timestamptz IS NULL
        OR pv.created_at < sqlc.narg('cursor_variant_created_at')::timestamptz
        OR (pv.created_at = sqlc.narg('cursor_variant_created_at')::timestamptz AND pv.id < sqlc.narg('cursor_variant_id')::uuid)
        OR (
            pv.created_at = sqlc.narg('cursor_variant_created_at')::timestamptz
            AND pv.id = sqlc.narg('cursor_variant_id')::uuid
            AND sl.location_id > sqlc.narg('cursor_location_id')::uuid
        )
    )
ORDER BY pv.created_at DESC, pv.id DESC, sl.location_id
LIMIT sqlc.arg('limit');

-- name: ListLow :many
-- A variant is low when its total qty across every location is at or
-- below the effective threshold: the product's own low_stock_threshold
-- override, else the shop's default (D-44). Owner ruling: only variants
-- that have been stocked at least once (an INNER JOIN to stock_levels — a
-- variant that has never had a stock_levels row, i.e. never moved, is not
-- "low", it is simply not tracked yet; a variant that sold out to qty 0
-- DOES still count, since it has a row), and only active products and
-- active variants (soft-deleted rows are already excluded by
-- deleted_at IS NULL). Ordered newest variant first (D-92): ORDER BY
-- variant_created_at DESC, variant_id DESC — both columns sort the same
-- direction, so the cursor is an ordinary two-column keyset comparison.
SELECT t.variant_id, t.product_id, t.qty::numeric(12,3) AS qty, COALESCE(p.low_stock_threshold, s.low_stock_threshold) AS threshold, t.variant_created_at
FROM (
    SELECT pv.id AS variant_id, pv.product_id, pv.created_at AS variant_created_at, SUM(sl.qty) AS qty
    FROM product_variants pv
    JOIN stock_levels sl ON sl.variant_id = pv.id AND sl.shop_id = pv.shop_id
    WHERE pv.shop_id = sqlc.arg('shop_id') AND pv.deleted_at IS NULL AND pv.is_active
    GROUP BY pv.id, pv.product_id, pv.created_at
) t
JOIN products p ON p.id = t.product_id AND p.deleted_at IS NULL AND p.is_active
JOIN shops s ON s.id = sqlc.arg('shop_id')
WHERE t.qty <= COALESCE(p.low_stock_threshold, s.low_stock_threshold)
    AND (
        sqlc.narg('cursor_variant_created_at')::timestamptz IS NULL
        OR (t.variant_created_at, t.variant_id) < (sqlc.narg('cursor_variant_created_at')::timestamptz, sqlc.narg('cursor_variant_id')::uuid)
    )
ORDER BY t.variant_created_at DESC, t.variant_id DESC
LIMIT sqlc.arg('limit');

-- name: TruncateLevelsForShop :exec
-- Step 1 of `savdo stock rebuild`: clear this shop's levels (a scoped
-- DELETE, not a table-level TRUNCATE — other shops' rows must survive),
-- then RebuildLevelsFromMovements recomputes them, in the same transaction.
DELETE FROM stock_levels WHERE shop_id = $1;

-- name: RebuildLevelsFromMovements :exec
-- Step 2 of `savdo stock rebuild`. stock_movements is the only source of
-- truth (ADR-006); this recomputes every level for the shop from it.
INSERT INTO stock_levels (shop_id, variant_id, location_id, qty, updated_at)
SELECT m.shop_id, m.variant_id, m.location_id, SUM(m.qty), now()
FROM stock_movements m
WHERE m.shop_id = $1
GROUP BY m.shop_id, m.variant_id, m.location_id;

-- name: SumMovementsForLevel :one
-- Used by the rebuild-equals-levels test: sums the ledger directly for one
-- (variant, location) and compares it to the corresponding stock_levels
-- row, proving ApplyLevelDelta and RebuildLevelsFromMovements agree.
SELECT COALESCE(SUM(qty), 0::numeric)::numeric(12,3) AS qty
FROM stock_movements
WHERE shop_id = $1 AND variant_id = $2 AND location_id = $3;

-- name: ListMovementsWithCreatedByName :many
-- GET /stock/movements' own read: same filters and (created_at, id) cursor
-- as ListMovements, plus the creating user's display name (contract's
-- additive `createdByName`, T3) so the admin never has to look the user up
-- itself. LEFT JOIN, not JOIN: created_by is nullable (a seed or a future
-- system-written movement may carry no actor), and a user row could in
-- principle be gone later — either case must still return the movement,
-- with created_by_name simply NULL. u.shop_id = m.shop_id is belt-and-
-- braces tenant scoping on the join (hard rule 1), even though created_by
-- already only ever holds an id from the movement's own shop.
SELECT m.id, m.shop_id, m.variant_id, m.location_id, m.kind, m.qty, m.unit_cost,
    m.ref_type, m.ref_id, m.adjustment_reason, m.reason, m.created_by, m.created_at,
    u.full_name AS created_by_name
FROM stock_movements m
LEFT JOIN users u ON u.id = m.created_by AND u.shop_id = m.shop_id
WHERE m.shop_id = sqlc.arg('shop_id')
    AND (sqlc.narg('variant_id')::uuid IS NULL OR m.variant_id = sqlc.narg('variant_id'))
    AND (sqlc.narg('location_id')::uuid IS NULL OR m.location_id = sqlc.narg('location_id'))
    AND (sqlc.narg('kind')::stock_movement_kind IS NULL OR m.kind = sqlc.narg('kind'))
    AND (sqlc.narg('from')::timestamptz IS NULL OR m.created_at >= sqlc.narg('from'))
    AND (sqlc.narg('to')::timestamptz IS NULL OR m.created_at <= sqlc.narg('to'))
    AND (
        sqlc.narg('cursor_created_at')::timestamptz IS NULL
        OR (m.created_at, m.id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::uuid)
    )
ORDER BY m.created_at DESC, m.id DESC
LIMIT sqlc.arg('limit');

-- name: CountLevelsForShop :one
-- `savdo stock rebuild`'s summary line: how many stock_levels rows the
-- shop has after the rebuild.
SELECT count(*) FROM stock_levels WHERE shop_id = $1;

-- name: CountMovementsForShop :one
-- `savdo stock rebuild`'s summary line: how many stock_movements rows the
-- rebuild's sum was computed from.
SELECT count(*) FROM stock_movements WHERE shop_id = $1;
