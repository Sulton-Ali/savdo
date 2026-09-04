package stock_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/stock"
)

// TestRebuild_matchesLedgerSum is api/internal/db/stock_schema_test.go's
// TestStockRebuild_matchesLedgerSum, run through the CLI-facing
// stock.Rebuild function instead of TruncateLevelsForShop/
// RebuildLevelsFromMovements called directly, plus the two summary counts
// `savdo stock rebuild` prints.
func TestRebuild_matchesLedgerSum(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	svc := stock.NewService(pool, q)

	shop := seedShop(ctx, t, q, "rebuild-cli")
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "rebuild-cli-product")
	v1 := seedVariant(ctx, t, q, shop.ID, product.ID, `{"size":"S"}`)
	v2 := seedVariant(ctx, t, q, shop.ID, product.ID, `{"size":"M"}`)
	l1 := seedLocation(ctx, t, q, shop.ID, "L1")
	l2 := seedLocation(ctx, t, q, shop.ID, "L2")

	// v1/l1: two movements, net 7. v2/l2: one movement, net 5. v1/l2 and
	// v2/l1 are deliberately left untouched.
	mustMove(ctx, t, svc, shop.ID, v1.ID, l1.ID, db.StockMovementKindPurchaseIn, "10.000")
	mustMove(ctx, t, svc, shop.ID, v1.ID, l1.ID, db.StockMovementKindSaleOut, "-3.000")
	mustMove(ctx, t, svc, shop.ID, v2.ID, l2.ID, db.StockMovementKindPurchaseIn, "5.000")

	beforeV1L1, _ := readLevel(ctx, t, pool, shop.ID, v1.ID, l1.ID)
	beforeV2L2, _ := readLevel(ctx, t, pool, shop.ID, v2.ID, l2.ID)
	if !beforeV1L1.Equal(d(t, "7.000")) {
		t.Fatalf("before rebuild v1/l1 = %s, want 7.000", beforeV1L1)
	}
	if !beforeV2L2.Equal(d(t, "5.000")) {
		t.Fatalf("before rebuild v2/l2 = %s, want 5.000", beforeV2L2)
	}

	result, err := stock.Rebuild(ctx, pool, shop.Slug)
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	// Two (variant, location) combos ever got a stock_levels row (v1/l1,
	// v2/l2); v1/l2 and v2/l1 never moved and stay absent, per D-41's
	// "never below zero" default not implying "everything gets a row".
	if result.Levels != 2 {
		t.Fatalf("Levels = %d, want 2", result.Levels)
	}
	if result.Movements != 3 {
		t.Fatalf("Movements = %d, want 3", result.Movements)
	}

	afterV1L1, exists := readLevel(ctx, t, pool, shop.ID, v1.ID, l1.ID)
	if !exists || !afterV1L1.Equal(d(t, "7.000")) {
		t.Fatalf("after rebuild v1/l1 = (%s, exists=%v), want (7.000, true)", afterV1L1, exists)
	}
	afterV2L2, exists := readLevel(ctx, t, pool, shop.ID, v2.ID, l2.ID)
	if !exists || !afterV2L2.Equal(d(t, "5.000")) {
		t.Fatalf("after rebuild v2/l2 = (%s, exists=%v), want (5.000, true)", afterV2L2, exists)
	}
	if _, exists := readLevel(ctx, t, pool, shop.ID, v1.ID, l2.ID); exists {
		t.Fatal("v1/l2 must stay absent after rebuild (never moved)")
	}
	if _, exists := readLevel(ctx, t, pool, shop.ID, v2.ID, l1.ID); exists {
		t.Fatal("v2/l1 must stay absent after rebuild (never moved)")
	}
}

// TestRebuild_zeroNetLevelRowSurvives is MINOR 6's own test: a
// (variant, location) whose movements net to exactly zero still has a
// stock_levels row (qty 0) before the rebuild — D-50's `/stock/low` rules
// depend on that row existing and counting as "stocked" rather than
// "never tracked" (docs/00-DECISIONS.md D-50, api/db/queries/stock.sql's
// own ListLow comment) — and RebuildLevelsFromMovements' GROUP BY still
// produces a row for it (SUM = 0 is still a group with one member, not an
// empty group), so the rebuild must not make that row disappear.
func TestRebuild_zeroNetLevelRowSurvives(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	svc := stock.NewService(pool, q)

	shop := seedShop(ctx, t, q, "rebuild-zero-net")
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "rebuild-zero-net-product")
	variant := seedVariant(ctx, t, q, shop.ID, product.ID, "{}")
	loc := seedLocation(ctx, t, q, shop.ID, "Main")

	mustMove(ctx, t, svc, shop.ID, variant.ID, loc.ID, db.StockMovementKindPurchaseIn, "5.000")
	mustMove(ctx, t, svc, shop.ID, variant.ID, loc.ID, db.StockMovementKindSaleOut, "-5.000")

	before, exists := readLevel(ctx, t, pool, shop.ID, variant.ID, loc.ID)
	if !exists || !before.Equal(d(t, "0.000")) {
		t.Fatalf("before rebuild = (%s, exists=%v), want (0.000, true)", before, exists)
	}

	if _, err := stock.Rebuild(ctx, pool, shop.Slug); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	after, exists := readLevel(ctx, t, pool, shop.ID, variant.ID, loc.ID)
	if !exists {
		t.Fatal("want the zero-net row to survive the rebuild, found none")
	}
	if !after.Equal(d(t, "0.000")) {
		t.Fatalf("after rebuild = %s, want 0.000", after)
	}
}

func TestRebuild_unknownShopSlugErrors(t *testing.T) {
	pool, _ := newTestQueries(t)
	if _, err := stock.Rebuild(context.Background(), pool, "no-such-shop"); err == nil {
		t.Fatal("want an error for an unknown shop slug, got none")
	}
}

// mustMove is MoveInTx failing the test on error, for setup code that
// isn't itself the thing under test.
func mustMove(ctx context.Context, t *testing.T, svc *stock.Service, shopID, variantID, locationID uuid.UUID, kind db.StockMovementKind, qty string) {
	t.Helper()
	_, err := svc.MoveInTx(ctx, stock.MoveParams{
		ShopID: shopID, VariantID: variantID, LocationID: locationID, Kind: kind, Qty: d(t, qty),
	})
	if err != nil {
		t.Fatalf("mustMove(%s): %v", qty, err)
	}
}
