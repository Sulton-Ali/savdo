package db_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

// ptr takes the address of a value, for constructing *T fields (e.g.
// AdjustmentReason) from a typed constant without a separate local var at
// every call site.
func ptr[T any](v T) *T { return &v }

// numericString renders a pgtype.Numeric the way Postgres would print it
// (fixed scale, e.g. "12.000" for numeric(12,3)), for exact-match
// assertions instead of float comparison (§ 04-DATA-MODEL.md rule 3: never
// compare money/quantity as float).
func numericString(t *testing.T, n pgtype.Numeric) string {
	t.Helper()
	v, err := n.Value()
	if err != nil {
		t.Fatalf("numeric Value(): %v", err)
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("numeric Value() returned %T, want string", v)
	}
	return s
}

// normalizeScale3 pads/truncates a decimal string's fractional part to
// exactly 3 digits by string manipulation (never float arithmetic —
// § 04-DATA-MODEL.md rule 3), so a bare "0" (SumMovementsForLevel's
// COALESCE(..., 0::numeric) fallback, which does not carry the
// numeric(12,3) column's display scale) compares equal to a column-sourced
// "0.000".
func normalizeScale3(s string) string {
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	intPart, fracPart, _ := strings.Cut(s, ".")
	for len(fracPart) < 3 {
		fracPart += "0"
	}
	out := intPart + "." + fracPart[:3]
	if neg && out != "0.000" {
		out = "-" + out
	}
	return out
}

// stockLocation creates a location for the ledger tests below; kind and
// is_active are fixed since none of these tests exercise location
// behaviour itself.
func stockLocation(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, name string) db.Location {
	t.Helper()
	loc, err := q.CreateLocation(ctx, db.CreateLocationParams{
		ID: uuid.New(), ShopID: shopID, Name: name, Kind: db.LocationKindStore, IsActive: true,
	})
	if err != nil {
		t.Fatalf("CreateLocation(%q): %v", name, err)
	}
	return loc
}

// stockVariant creates a variant on the given product — every product
// needs at least one (§ 04-DATA-MODEL.md). attrs must be unique per
// product ((product_id, attributes) is a unique index); pass "{}" for a
// product's only variant, or a distinguishing value (e.g. `{"size":"S"}`)
// when a test needs more than one variant on the same product.
func stockVariant(ctx context.Context, t *testing.T, q *db.Queries, shopID, productID uuid.UUID, attrs string) db.ProductVariant {
	t.Helper()
	v, err := q.CreateVariant(ctx, db.CreateVariantParams{
		ID: uuid.New(), ShopID: shopID, ProductID: productID,
		Attributes: json.RawMessage(attrs), IsActive: true,
	})
	if err != nil {
		t.Fatalf("CreateVariant: %v", err)
	}
	return v
}

// insertMovement is a small wrapper around InsertMovement with the common
// fields filled in, for tests that only care about one or two of them.
func insertMovement(ctx context.Context, q *db.Queries, arg db.InsertMovementParams) (db.StockMovement, error) {
	if arg.ID == uuid.Nil {
		arg.ID = uuid.New()
	}
	return q.InsertMovement(ctx, arg)
}

func TestStockMovements_appendOnlyRejectsUpdateAndDelete(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-ledger-immutable")
	unit := catalogUnit(ctx, t, q, shop.ID, "pcs")
	product := catalogProduct(ctx, t, q, shop.ID, unit.ID, "hoodie")
	variant := stockVariant(ctx, t, q, shop.ID, product.ID, "{}")
	loc := stockLocation(ctx, t, q, shop.ID, "Main")

	mv, err := insertMovement(ctx, q, db.InsertMovementParams{
		ShopID: shop.ID, VariantID: variant.ID, LocationID: loc.ID,
		Kind: db.StockMovementKindPurchaseIn, Qty: numeric(t, "10.000"),
	})
	if err != nil {
		t.Fatalf("InsertMovement: %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE stock_movements SET qty = qty + 1 WHERE id = $1`, mv.ID); err == nil {
		t.Fatal("want UPDATE on stock_movements to be rejected by the append-only trigger, got no error")
	} else if !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("want an append-only trigger error, got: %v", err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM stock_movements WHERE id = $1`, mv.ID); err == nil {
		t.Fatal("want DELETE on stock_movements to be rejected by the append-only trigger, got no error")
	} else if !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("want an append-only trigger error, got: %v", err)
	}
}

func TestStockMovements_qtyCannotBeZero(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-ledger-qty-zero")
	unit := catalogUnit(ctx, t, q, shop.ID, "pcs")
	product := catalogProduct(ctx, t, q, shop.ID, unit.ID, "hoodie")
	variant := stockVariant(ctx, t, q, shop.ID, product.ID, "{}")
	loc := stockLocation(ctx, t, q, shop.ID, "Main")

	_, err := insertMovement(ctx, q, db.InsertMovementParams{
		ShopID: shop.ID, VariantID: variant.ID, LocationID: loc.ID,
		Kind: db.StockMovementKindAdjustment, Qty: numeric(t, "0.000"),
		AdjustmentReason: ptr(db.AdjustmentReasonCountCorrection),
	})
	if err == nil {
		t.Fatal("want a check-constraint error inserting a zero-qty movement, got none")
	}
}

func TestStockMovements_adjustmentReasonRequiredOnlyForAdjustment(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-ledger-reason")
	unit := catalogUnit(ctx, t, q, shop.ID, "pcs")
	product := catalogProduct(ctx, t, q, shop.ID, unit.ID, "hoodie")
	variant := stockVariant(ctx, t, q, shop.ID, product.ID, "{}")
	loc := stockLocation(ctx, t, q, shop.ID, "Main")

	// adjustment without a reason: rejected.
	if _, err := insertMovement(ctx, q, db.InsertMovementParams{
		ShopID: shop.ID, VariantID: variant.ID, LocationID: loc.ID,
		Kind: db.StockMovementKindAdjustment, Qty: numeric(t, "1.000"),
	}); err == nil {
		t.Fatal("want a check-constraint error for an adjustment with no adjustment_reason, got none")
	}

	// non-adjustment with a reason: rejected.
	if _, err := insertMovement(ctx, q, db.InsertMovementParams{
		ShopID: shop.ID, VariantID: variant.ID, LocationID: loc.ID,
		Kind: db.StockMovementKindPurchaseIn, Qty: numeric(t, "1.000"),
		AdjustmentReason: ptr(db.AdjustmentReasonDamaged),
	}); err == nil {
		t.Fatal("want a check-constraint error for a non-adjustment movement with adjustment_reason set, got none")
	}

	// adjustment with a reason: accepted.
	if _, err := insertMovement(ctx, q, db.InsertMovementParams{
		ShopID: shop.ID, VariantID: variant.ID, LocationID: loc.ID,
		Kind: db.StockMovementKindAdjustment, Qty: numeric(t, "1.000"),
		AdjustmentReason: ptr(db.AdjustmentReasonCountCorrection),
	}); err != nil {
		t.Fatalf("want an adjustment with a reason to succeed, got: %v", err)
	}

	// non-adjustment with no reason: accepted.
	if _, err := insertMovement(ctx, q, db.InsertMovementParams{
		ShopID: shop.ID, VariantID: variant.ID, LocationID: loc.ID,
		Kind: db.StockMovementKindPurchaseIn, Qty: numeric(t, "1.000"),
	}); err != nil {
		t.Fatalf("want a purchase_in with no reason to succeed, got: %v", err)
	}
}

// applyDelta runs stock.Service.Move's write sequence (UpsertLevelRow,
// GetLevelForUpdate, ApplyLevelDelta) plus the matching movement insert,
// inside one transaction — the same as the stock service will run it, so
// the FOR UPDATE lock GetLevelForUpdate takes is actually held across all
// four statements instead of being released the instant each one's own
// implicit, separate transaction commits.
func applyDelta(ctx context.Context, t *testing.T, pool *pgxpool.Pool, shopID, variantID, locationID uuid.UUID, kind db.StockMovementKind, delta string) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op once committed
	qtx := db.New(tx)

	if err := qtx.UpsertLevelRow(ctx, db.UpsertLevelRowParams{ShopID: shopID, VariantID: variantID, LocationID: locationID}); err != nil {
		t.Fatalf("UpsertLevelRow: %v", err)
	}
	if _, err := qtx.GetLevelForUpdate(ctx, db.GetLevelForUpdateParams{ShopID: shopID, VariantID: variantID, LocationID: locationID}); err != nil {
		t.Fatalf("GetLevelForUpdate: %v", err)
	}
	if _, err := insertMovement(ctx, qtx, db.InsertMovementParams{
		ShopID: shopID, VariantID: variantID, LocationID: locationID,
		Kind: kind, Qty: numeric(t, delta),
	}); err != nil {
		t.Fatalf("InsertMovement: %v", err)
	}
	if _, err := qtx.ApplyLevelDelta(ctx, db.ApplyLevelDeltaParams{
		ShopID: shopID, VariantID: variantID, LocationID: locationID, Delta: numeric(t, delta),
	}); err != nil {
		t.Fatalf("ApplyLevelDelta: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

// levelSet reads every stock_levels row for a shop directly (raw SQL is
// fine here — this is the test's own verification query, not a codepath
// under test), keyed by (variant_id, location_id).
type levelKey struct{ variantID, locationID uuid.UUID }

func levelSet(ctx context.Context, t *testing.T, pool *pgxpool.Pool, shopID uuid.UUID) map[levelKey]string {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT variant_id, location_id, qty FROM stock_levels WHERE shop_id = $1`, shopID)
	if err != nil {
		t.Fatalf("levelSet query: %v", err)
	}
	defer rows.Close()
	result := map[levelKey]string{}
	for rows.Next() {
		var k levelKey
		var qty pgtype.Numeric
		if err := rows.Scan(&k.variantID, &k.locationID, &qty); err != nil {
			t.Fatalf("levelSet scan: %v", err)
		}
		result[k] = numericString(t, qty)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("levelSet rows: %v", err)
	}
	return result
}

// levelQty returns the qty for (variantID, locationID) in set, or "0.000"
// if the combination has no row — treating an absent row as zero, not a
// failure, since a (variant, location) pair that never had a movement
// never gets a stock_levels row at all (§ 04-DATA-MODEL.md: only
// stock.Service.Move writes it).
func levelQty(set map[levelKey]string, variantID, locationID uuid.UUID) string {
	if v, ok := set[levelKey{variantID, locationID}]; ok {
		return v
	}
	return "0.000"
}

func TestStockRebuild_matchesLedgerSum(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-rebuild")
	unit := catalogUnit(ctx, t, q, shop.ID, "pcs")
	product := catalogProduct(ctx, t, q, shop.ID, unit.ID, "hoodie")
	v1 := stockVariant(ctx, t, q, shop.ID, product.ID, `{"size":"S"}`)
	v2 := stockVariant(ctx, t, q, shop.ID, product.ID, `{"size":"M"}`)
	l1 := stockLocation(ctx, t, q, shop.ID, "L1")
	l2 := stockLocation(ctx, t, q, shop.ID, "L2")

	// v1/l1: two movements, net 7. v2/l2: one movement, net 5.
	// v1/l2 and v2/l1 are deliberately left untouched — no stock_levels row
	// for either combination, on either side of the rebuild.
	applyDelta(ctx, t, pool, shop.ID, v1.ID, l1.ID, db.StockMovementKindPurchaseIn, "10.000")
	applyDelta(ctx, t, pool, shop.ID, v1.ID, l1.ID, db.StockMovementKindSaleOut, "-3.000")
	applyDelta(ctx, t, pool, shop.ID, v2.ID, l2.ID, db.StockMovementKindPurchaseIn, "5.000")

	before := levelSet(ctx, t, pool, shop.ID)
	combos := []levelKey{{v1.ID, l1.ID}, {v1.ID, l2.ID}, {v2.ID, l1.ID}, {v2.ID, l2.ID}}
	want := map[levelKey]string{
		{v1.ID, l1.ID}: "7.000",
		{v1.ID, l2.ID}: "0.000", // absent row
		{v2.ID, l1.ID}: "0.000", // absent row
		{v2.ID, l2.ID}: "5.000",
	}
	for _, c := range combos {
		if got := levelQty(before, c.variantID, c.locationID); got != want[c] {
			t.Errorf("before rebuild: want %s for %+v, got %s", want[c], c, got)
		}
	}

	// savdo stock rebuild: wipe this shop's levels and recompute from the
	// ledger; the full set must match what ApplyLevelDelta already
	// produced, combo for combo, including the two that stay absent.
	if err := q.TruncateLevelsForShop(ctx, shop.ID); err != nil {
		t.Fatalf("TruncateLevelsForShop: %v", err)
	}
	if err := q.RebuildLevelsFromMovements(ctx, shop.ID); err != nil {
		t.Fatalf("RebuildLevelsFromMovements: %v", err)
	}

	after := levelSet(ctx, t, pool, shop.ID)
	for _, c := range combos {
		b, a := levelQty(before, c.variantID, c.locationID), levelQty(after, c.variantID, c.locationID)
		if a != b {
			t.Errorf("rebuild mismatch for %+v: before %s, after %s", c, b, a)
		}
	}

	for _, c := range combos {
		sum, err := q.SumMovementsForLevel(ctx, db.SumMovementsForLevelParams{ShopID: shop.ID, VariantID: c.variantID, LocationID: c.locationID})
		if err != nil {
			t.Fatalf("SumMovementsForLevel %+v: %v", c, err)
		}
		if got := normalizeScale3(numericString(t, sum)); got != want[c] {
			t.Errorf("SumMovementsForLevel %+v: want %s, got %s", c, want[c], got)
		}
	}
}

func TestListLow_productOverrideElseShopDefault(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-low-stock")
	// shops.low_stock_threshold defaults to 2 (0009_shop_product_thresholds.sql).
	unit := catalogUnit(ctx, t, q, shop.ID, "pcs")
	loc := stockLocation(ctx, t, q, shop.ID, "Main")

	// Product A: no override, uses the shop default (2).
	productA := catalogProduct(ctx, t, q, shop.ID, unit.ID, "shirt-a")
	lowA := stockVariant(ctx, t, q, shop.ID, productA.ID, `{"size":"S"}`)    // qty 1 <= 2: low
	notLowA := stockVariant(ctx, t, q, shop.ID, productA.ID, `{"size":"M"}`) // qty 3 > 2: not low

	// Product B: overrides the threshold to 5.
	five := int32(5)
	productB, err := q.CreateProduct(ctx, db.CreateProductParams{
		ID: uuid.New(), ShopID: shop.ID, UnitID: unit.ID, Slug: "shirt-b",
		BasePrice: numeric(t, "125000.00"), IsActive: true, LowStockThreshold: &five,
	})
	if err != nil {
		t.Fatalf("CreateProduct with low_stock_threshold override: %v", err)
	}
	lowB := stockVariant(ctx, t, q, shop.ID, productB.ID, `{"size":"S"}`)    // qty 4 <= 5: low
	notLowB := stockVariant(ctx, t, q, shop.ID, productB.ID, `{"size":"M"}`) // qty 10 > 5: not low

	// Never stocked: no applyDelta call at all, so no stock_levels row ever
	// exists for it. Owner ruling: this does NOT count as low — it is
	// untracked, not "0 and therefore low".
	neverStocked := stockVariant(ctx, t, q, shop.ID, productA.ID, `{"size":"L"}`)

	// A variant on an inactive product: stocked, low qty, but the product
	// itself is not active — excluded.
	productC, err := q.CreateProduct(ctx, db.CreateProductParams{
		ID: uuid.New(), ShopID: shop.ID, UnitID: unit.ID, Slug: "shirt-c",
		BasePrice: numeric(t, "125000.00"), IsActive: false,
	})
	if err != nil {
		t.Fatalf("CreateProduct (inactive): %v", err)
	}
	inactiveProductVariant, err := q.CreateVariant(ctx, db.CreateVariantParams{
		ID: uuid.New(), ShopID: shop.ID, ProductID: productC.ID,
		Attributes: json.RawMessage(`{}`), IsActive: true,
	})
	if err != nil {
		t.Fatalf("CreateVariant on inactive product: %v", err)
	}

	// An inactive variant on productA (which is itself active): stocked,
	// low qty, but the variant itself is not active — excluded.
	inactiveVariant, err := q.CreateVariant(ctx, db.CreateVariantParams{
		ID: uuid.New(), ShopID: shop.ID, ProductID: productA.ID,
		Attributes: json.RawMessage(`{"size":"XL"}`), IsActive: false,
	})
	if err != nil {
		t.Fatalf("CreateVariant (inactive variant): %v", err)
	}

	applyDelta(ctx, t, pool, shop.ID, lowA.ID, loc.ID, db.StockMovementKindPurchaseIn, "1.000")
	applyDelta(ctx, t, pool, shop.ID, notLowA.ID, loc.ID, db.StockMovementKindPurchaseIn, "3.000")
	applyDelta(ctx, t, pool, shop.ID, lowB.ID, loc.ID, db.StockMovementKindPurchaseIn, "4.000")
	applyDelta(ctx, t, pool, shop.ID, notLowB.ID, loc.ID, db.StockMovementKindPurchaseIn, "10.000")
	applyDelta(ctx, t, pool, shop.ID, inactiveProductVariant.ID, loc.ID, db.StockMovementKindPurchaseIn, "1.000")
	applyDelta(ctx, t, pool, shop.ID, inactiveVariant.ID, loc.ID, db.StockMovementKindPurchaseIn, "1.000")

	rows, err := q.ListLow(ctx, db.ListLowParams{ShopID: shop.ID, Limit: 100})
	if err != nil {
		t.Fatalf("ListLow: %v", err)
	}

	got := map[uuid.UUID]int32{}
	for _, r := range rows {
		got[r.VariantID] = r.Threshold
	}

	if threshold, ok := got[lowA.ID]; !ok {
		t.Error("want the shop-default-threshold low variant in ListLow, missing")
	} else if threshold != 2 {
		t.Errorf("want shop default threshold 2 for lowA, got %d", threshold)
	}
	if threshold, ok := got[lowB.ID]; !ok {
		t.Error("want the product-override-threshold low variant in ListLow, missing")
	} else if threshold != 5 {
		t.Errorf("want product override threshold 5 for lowB, got %d", threshold)
	}
	if _, ok := got[notLowA.ID]; ok {
		t.Error("want the above-shop-default variant absent from ListLow, found it")
	}
	if _, ok := got[notLowB.ID]; ok {
		t.Error("want the above-product-override variant absent from ListLow, found it")
	}
	if _, ok := got[neverStocked.ID]; ok {
		t.Error("want a never-stocked variant (no stock_levels row) absent from ListLow, found it")
	}
	if _, ok := got[inactiveProductVariant.ID]; ok {
		t.Error("want a variant of an inactive product absent from ListLow, found it")
	}
	if _, ok := got[inactiveVariant.ID]; ok {
		t.Error("want an inactive variant absent from ListLow, found it")
	}
}

// paginateAllMovements walks ListMovements page by page using its
// (created_at, id) cursor until a short page signals the end, and returns
// every row collected in the order the pages produced them.
func paginateAllMovements(ctx context.Context, t *testing.T, q *db.Queries, base db.ListMovementsParams, pageSize int32) []db.StockMovement {
	t.Helper()
	p := base
	p.Limit = pageSize
	p.CursorCreatedAt = nil
	p.CursorID = nil
	var all []db.StockMovement
	for {
		page, err := q.ListMovements(ctx, p)
		if err != nil {
			t.Fatalf("ListMovements: %v", err)
		}
		all = append(all, page...)
		if int32(len(page)) < pageSize {
			return all
		}
		last := page[len(page)-1]
		ca, id := last.CreatedAt, last.ID
		p.CursorCreatedAt, p.CursorID = &ca, &id
	}
}

// paginateAllLevels walks ListLevels page by page using its
// (variant_created_at, variant_id, location_id) cursor (D-92: newest
// variant first, then location) until a short page signals the end.
func paginateAllLevels(ctx context.Context, t *testing.T, q *db.Queries, base db.ListLevelsParams, pageSize int32) []db.ListLevelsRow {
	t.Helper()
	p := base
	p.Limit = pageSize
	p.CursorVariantCreatedAt = nil
	p.CursorVariantID = nil
	p.CursorLocationID = nil
	var all []db.ListLevelsRow
	for {
		page, err := q.ListLevels(ctx, p)
		if err != nil {
			t.Fatalf("ListLevels: %v", err)
		}
		all = append(all, page...)
		if int32(len(page)) < pageSize {
			return all
		}
		last := page[len(page)-1]
		ca, vid, lid := last.VariantCreatedAt, last.VariantID, last.LocationID
		p.CursorVariantCreatedAt, p.CursorVariantID, p.CursorLocationID = &ca, &vid, &lid
	}
}

// paginateAllLow walks ListLow page by page using its (variant_created_at,
// variant_id) cursor (D-92: newest variant first) until a short page
// signals the end.
func paginateAllLow(ctx context.Context, t *testing.T, q *db.Queries, base db.ListLowParams, pageSize int32) []db.ListLowRow {
	t.Helper()
	p := base
	p.Limit = pageSize
	p.CursorVariantCreatedAt = nil
	p.CursorVariantID = nil
	var all []db.ListLowRow
	for {
		page, err := q.ListLow(ctx, p)
		if err != nil {
			t.Fatalf("ListLow: %v", err)
		}
		all = append(all, page...)
		if int32(len(page)) < pageSize {
			return all
		}
		last := page[len(page)-1]
		ca, vid := last.VariantCreatedAt, last.VariantID
		p.CursorVariantCreatedAt, p.CursorVariantID = &ca, &vid
	}
}

func TestListMovements_cursorPagination(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-movements-cursor")
	unit := catalogUnit(ctx, t, q, shop.ID, "pcs")
	product := catalogProduct(ctx, t, q, shop.ID, unit.ID, "hoodie")
	variant := stockVariant(ctx, t, q, shop.ID, product.ID, "{}")
	loc := stockLocation(ctx, t, q, shop.ID, "Main")

	const n = 5
	for i := 0; i < n; i++ {
		if _, err := insertMovement(ctx, q, db.InsertMovementParams{
			ShopID: shop.ID, VariantID: variant.ID, LocationID: loc.ID,
			Kind: db.StockMovementKindPurchaseIn, Qty: numeric(t, "1.000"),
		}); err != nil {
			t.Fatalf("InsertMovement %d: %v", i, err)
		}
	}

	// A batch inserted inside one transaction shares that transaction's
	// start time for every now() call, so these rows tie on created_at —
	// the exact case ListMovements' (created_at, id) tiebreaker exists for.
	// Without this, every row above got its own implicit transaction and a
	// distinct timestamp, and the tiebreaker was never actually exercised.
	const batchN = 4
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	qtx := db.New(tx)
	for i := 0; i < batchN; i++ {
		if _, err := insertMovement(ctx, qtx, db.InsertMovementParams{
			ShopID: shop.ID, VariantID: variant.ID, LocationID: loc.ID,
			Kind: db.StockMovementKindPurchaseIn, Qty: numeric(t, "1.000"),
		}); err != nil {
			t.Fatalf("InsertMovement (batch) %d: %v", i, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit (batch): %v", err)
	}

	base := db.ListMovementsParams{ShopID: shop.ID}
	refParams := base
	refParams.Limit = 100
	reference, err := q.ListMovements(ctx, refParams)
	if err != nil {
		t.Fatalf("ListMovements (reference): %v", err)
	}
	if len(reference) != n+batchN {
		t.Fatalf("want %d movements, got %d", n+batchN, len(reference))
	}
	// Confirm the tie is actually present in this run, or the assertion
	// above is only exercising the untied path by luck.
	tied := false
	for i := 1; i < len(reference); i++ {
		if reference[i-1].CreatedAt.Equal(reference[i].CreatedAt) {
			tied = true
			break
		}
	}
	if !tied {
		t.Fatal("want at least one created_at tie among the reference rows (the batch insert should produce one), got none")
	}

	paginated := paginateAllMovements(ctx, t, q, base, 2)
	assertSameOrder(t, "ListMovements", idsOfMovements(reference), idsOfMovements(paginated))
}

func TestListLevels_cursorPagination(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-levels-cursor")
	unit := catalogUnit(ctx, t, q, shop.ID, "pcs")
	product := catalogProduct(ctx, t, q, shop.ID, unit.ID, "hoodie")
	loc := stockLocation(ctx, t, q, shop.ID, "Main")

	const n = 5
	for i := 0; i < n; i++ {
		v := stockVariant(ctx, t, q, shop.ID, product.ID, fmt.Sprintf(`{"size":"S%d"}`, i))
		applyDelta(ctx, t, pool, shop.ID, v.ID, loc.ID, db.StockMovementKindPurchaseIn, "1.000")
	}

	base := db.ListLevelsParams{ShopID: shop.ID}
	refParams := base
	refParams.Limit = 100
	reference, err := q.ListLevels(ctx, refParams)
	if err != nil {
		t.Fatalf("ListLevels (reference): %v", err)
	}
	if len(reference) != n {
		t.Fatalf("want %d levels, got %d", n, len(reference))
	}

	paginated := paginateAllLevels(ctx, t, q, base, 2)
	assertSameOrder(t, "ListLevels", idsOfLevels(reference), idsOfLevels(paginated))
}

func TestListLow_cursorPagination(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-low-cursor")
	unit := catalogUnit(ctx, t, q, shop.ID, "pcs")
	product := catalogProduct(ctx, t, q, shop.ID, unit.ID, "hoodie")
	loc := stockLocation(ctx, t, q, shop.ID, "Main")

	const n = 5
	for i := 0; i < n; i++ {
		v := stockVariant(ctx, t, q, shop.ID, product.ID, fmt.Sprintf(`{"size":"S%d"}`, i))
		// shop default threshold is 2; qty 1 is low for every one of them.
		applyDelta(ctx, t, pool, shop.ID, v.ID, loc.ID, db.StockMovementKindPurchaseIn, "1.000")
	}

	base := db.ListLowParams{ShopID: shop.ID}
	refParams := base
	refParams.Limit = 100
	reference, err := q.ListLow(ctx, refParams)
	if err != nil {
		t.Fatalf("ListLow (reference): %v", err)
	}
	if len(reference) != n {
		t.Fatalf("want %d low variants, got %d", n, len(reference))
	}

	paginated := paginateAllLow(ctx, t, q, base, 2)
	assertSameOrder(t, "ListLow", idsOfLow(reference), idsOfLow(paginated))
}

func idsOfMovements(rows []db.StockMovement) []uuid.UUID {
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids
}

func idsOfLevels(rows []db.ListLevelsRow) []uuid.UUID {
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.VariantID
	}
	return ids
}

func idsOfLow(rows []db.ListLowRow) []uuid.UUID {
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.VariantID
	}
	return ids
}

// assertSameOrder asserts paginated reproduces reference exactly —
// element for element, in order — which by construction also proves no
// duplicate and no skipped row (a duplicate or a skip would change the
// length or an element at some index).
func assertSameOrder(t *testing.T, label string, reference, paginated []uuid.UUID) {
	t.Helper()
	if len(paginated) != len(reference) {
		t.Fatalf("%s: want %d rows paginated (matching the unpaginated reference), got %d", label, len(reference), len(paginated))
	}
	for i := range reference {
		if paginated[i] != reference[i] {
			t.Fatalf("%s: order mismatch at index %d: reference %s, paginated %s", label, i, reference[i], paginated[i])
		}
	}
}

// movementFixture is shared setup for the ListMovements filter tests
// below: two variants, two locations, one movement per (variant,
// location) combination, each a different kind, so a single filter value
// picks out an unambiguous subset.
type movementFixture struct {
	q                  *db.Queries
	shopID             uuid.UUID
	v1, v2             uuid.UUID
	l1, l2             uuid.UUID
	mvA, mvB, mvC, mvD db.StockMovement
}

func newMovementFixture(t *testing.T) movementFixture {
	t.Helper()
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-movement-filters")
	unit := catalogUnit(ctx, t, q, shop.ID, "pcs")
	product := catalogProduct(ctx, t, q, shop.ID, unit.ID, "hoodie")
	v1 := stockVariant(ctx, t, q, shop.ID, product.ID, `{"size":"S"}`)
	v2 := stockVariant(ctx, t, q, shop.ID, product.ID, `{"size":"M"}`)
	l1 := stockLocation(ctx, t, q, shop.ID, "L1")
	l2 := stockLocation(ctx, t, q, shop.ID, "L2")

	mvA, err := insertMovement(ctx, q, db.InsertMovementParams{
		ShopID: shop.ID, VariantID: v1.ID, LocationID: l1.ID,
		Kind: db.StockMovementKindPurchaseIn, Qty: numeric(t, "5.000"),
	})
	if err != nil {
		t.Fatalf("insert mvA: %v", err)
	}
	mvB, err := insertMovement(ctx, q, db.InsertMovementParams{
		ShopID: shop.ID, VariantID: v1.ID, LocationID: l2.ID,
		Kind: db.StockMovementKindPurchaseIn, Qty: numeric(t, "3.000"),
	})
	if err != nil {
		t.Fatalf("insert mvB: %v", err)
	}
	mvC, err := insertMovement(ctx, q, db.InsertMovementParams{
		ShopID: shop.ID, VariantID: v2.ID, LocationID: l1.ID,
		Kind: db.StockMovementKindSaleOut, Qty: numeric(t, "-2.000"),
	})
	if err != nil {
		t.Fatalf("insert mvC: %v", err)
	}
	mvD, err := insertMovement(ctx, q, db.InsertMovementParams{
		ShopID: shop.ID, VariantID: v2.ID, LocationID: l2.ID,
		Kind: db.StockMovementKindAdjustment, Qty: numeric(t, "1.000"),
		AdjustmentReason: ptr(db.AdjustmentReasonCountCorrection),
	})
	if err != nil {
		t.Fatalf("insert mvD: %v", err)
	}

	return movementFixture{q: q, shopID: shop.ID, v1: v1.ID, v2: v2.ID, l1: l1.ID, l2: l2.ID, mvA: mvA, mvB: mvB, mvC: mvC, mvD: mvD}
}

// assertMovementIDs asserts got contains exactly the given movement ids
// (order-independent — the filter tests only care about set membership).
func assertMovementIDs(t *testing.T, got []db.StockMovement, want ...uuid.UUID) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("want %d movements, got %d: %+v", len(want), len(got), got)
	}
	wantSet := map[uuid.UUID]bool{}
	for _, id := range want {
		wantSet[id] = true
	}
	for _, mv := range got {
		if !wantSet[mv.ID] {
			t.Errorf("unexpected movement %s in result", mv.ID)
		}
	}
}

func TestListMovements_filterByVariant(t *testing.T) {
	f := newMovementFixture(t)
	rows, err := f.q.ListMovements(context.Background(), db.ListMovementsParams{ShopID: f.shopID, VariantID: &f.v1, Limit: 100})
	if err != nil {
		t.Fatalf("ListMovements: %v", err)
	}
	assertMovementIDs(t, rows, f.mvA.ID, f.mvB.ID)
}

func TestListMovements_filterByLocation(t *testing.T) {
	f := newMovementFixture(t)
	rows, err := f.q.ListMovements(context.Background(), db.ListMovementsParams{ShopID: f.shopID, LocationID: &f.l1, Limit: 100})
	if err != nil {
		t.Fatalf("ListMovements: %v", err)
	}
	assertMovementIDs(t, rows, f.mvA.ID, f.mvC.ID)
}

func TestListMovements_filterByKind(t *testing.T) {
	f := newMovementFixture(t)
	kind := db.StockMovementKindPurchaseIn
	rows, err := f.q.ListMovements(context.Background(), db.ListMovementsParams{ShopID: f.shopID, Kind: &kind, Limit: 100})
	if err != nil {
		t.Fatalf("ListMovements: %v", err)
	}
	assertMovementIDs(t, rows, f.mvA.ID, f.mvB.ID)
}

func TestListMovements_filterByFrom(t *testing.T) {
	f := newMovementFixture(t)
	from := f.mvC.CreatedAt
	rows, err := f.q.ListMovements(context.Background(), db.ListMovementsParams{ShopID: f.shopID, From: &from, Limit: 100})
	if err != nil {
		t.Fatalf("ListMovements: %v", err)
	}
	assertMovementIDs(t, rows, f.mvC.ID, f.mvD.ID)
}

func TestListMovements_filterByTo(t *testing.T) {
	f := newMovementFixture(t)
	to := f.mvB.CreatedAt
	rows, err := f.q.ListMovements(context.Background(), db.ListMovementsParams{ShopID: f.shopID, To: &to, Limit: 100})
	if err != nil {
		t.Fatalf("ListMovements: %v", err)
	}
	assertMovementIDs(t, rows, f.mvA.ID, f.mvB.ID)
}
