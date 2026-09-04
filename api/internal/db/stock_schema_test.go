package db_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

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
// exactly as the stock service will, so these schema tests do not read a
// level that was never locked.
func applyDelta(ctx context.Context, t *testing.T, q *db.Queries, shopID, variantID, locationID uuid.UUID, kind db.StockMovementKind, delta string) {
	t.Helper()
	if err := q.UpsertLevelRow(ctx, db.UpsertLevelRowParams{ShopID: shopID, VariantID: variantID, LocationID: locationID}); err != nil {
		t.Fatalf("UpsertLevelRow: %v", err)
	}
	if _, err := q.GetLevelForUpdate(ctx, db.GetLevelForUpdateParams{ShopID: shopID, VariantID: variantID, LocationID: locationID}); err != nil {
		t.Fatalf("GetLevelForUpdate: %v", err)
	}
	if _, err := insertMovement(ctx, q, db.InsertMovementParams{
		ShopID: shopID, VariantID: variantID, LocationID: locationID,
		Kind: kind, Qty: numeric(t, delta),
	}); err != nil {
		t.Fatalf("InsertMovement: %v", err)
	}
	if _, err := q.ApplyLevelDelta(ctx, db.ApplyLevelDeltaParams{
		ShopID: shopID, VariantID: variantID, LocationID: locationID, Delta: numeric(t, delta),
	}); err != nil {
		t.Fatalf("ApplyLevelDelta: %v", err)
	}
}

func TestStockRebuild_matchesLedgerSum(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-rebuild")
	unit := catalogUnit(ctx, t, q, shop.ID, "pcs")
	product := catalogProduct(ctx, t, q, shop.ID, unit.ID, "hoodie")
	variant := stockVariant(ctx, t, q, shop.ID, product.ID, "{}")
	loc := stockLocation(ctx, t, q, shop.ID, "Main")

	applyDelta(ctx, t, q, shop.ID, variant.ID, loc.ID, db.StockMovementKindPurchaseIn, "10.000")
	applyDelta(ctx, t, q, shop.ID, variant.ID, loc.ID, db.StockMovementKindSaleOut, "-3.000")
	applyDelta(ctx, t, q, shop.ID, variant.ID, loc.ID, db.StockMovementKindPurchaseIn, "5.000")

	level, err := q.GetLevelForUpdate(ctx, db.GetLevelForUpdateParams{ShopID: shop.ID, VariantID: variant.ID, LocationID: loc.ID})
	if err != nil {
		t.Fatalf("GetLevelForUpdate: %v", err)
	}
	levelStr := numericString(t, level.Qty)
	if levelStr != "12.000" {
		t.Fatalf("want stock_levels.qty 12.000 after 10-3+5, got %s", levelStr)
	}

	// savdo stock rebuild: wipe this shop's levels and recompute from the
	// ledger; the result must match what ApplyLevelDelta already produced.
	if err := q.TruncateLevelsForShop(ctx, shop.ID); err != nil {
		t.Fatalf("TruncateLevelsForShop: %v", err)
	}
	if err := q.RebuildLevelsFromMovements(ctx, shop.ID); err != nil {
		t.Fatalf("RebuildLevelsFromMovements: %v", err)
	}

	rebuilt, err := q.GetLevelForUpdate(ctx, db.GetLevelForUpdateParams{ShopID: shop.ID, VariantID: variant.ID, LocationID: loc.ID})
	if err != nil {
		t.Fatalf("GetLevelForUpdate after rebuild: %v", err)
	}
	if numericString(t, rebuilt.Qty) != "12.000" {
		t.Fatalf("want rebuilt qty 12.000, got %s", numericString(t, rebuilt.Qty))
	}

	sum, err := q.SumMovementsForLevel(ctx, db.SumMovementsForLevelParams{ShopID: shop.ID, VariantID: variant.ID, LocationID: loc.ID})
	if err != nil {
		t.Fatalf("SumMovementsForLevel: %v", err)
	}
	if numericString(t, sum) != "12.000" {
		t.Fatalf("want SumMovementsForLevel 12.000, got %s", numericString(t, sum))
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

	applyDelta(ctx, t, q, shop.ID, lowA.ID, loc.ID, db.StockMovementKindPurchaseIn, "1.000")
	applyDelta(ctx, t, q, shop.ID, notLowA.ID, loc.ID, db.StockMovementKindPurchaseIn, "3.000")
	applyDelta(ctx, t, q, shop.ID, lowB.ID, loc.ID, db.StockMovementKindPurchaseIn, "4.000")
	applyDelta(ctx, t, q, shop.ID, notLowB.ID, loc.ID, db.StockMovementKindPurchaseIn, "10.000")
	applyDelta(ctx, t, q, shop.ID, inactiveProductVariant.ID, loc.ID, db.StockMovementKindPurchaseIn, "1.000")
	applyDelta(ctx, t, q, shop.ID, inactiveVariant.ID, loc.ID, db.StockMovementKindPurchaseIn, "1.000")

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
