package stock_test

import (
	"context"
	"testing"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/stock"
)

// isolationFixture seeds two shops, each with one manager, one variant and
// one location, and gives shop A's variant one committed movement (a
// purchase_in of 3) — everything a levels/movements/low isolation check
// needs.
type isolationFixture struct {
	h                    *stock.Handler
	shopA, shopB         db.Shop
	managerA, managerB   db.User
	variantA, variantB   db.ProductVariant
	locationA, locationB db.Location
}

func newIsolationFixture(t *testing.T) isolationFixture {
	t.Helper()
	pool, q := newTestQueries(t)
	ctx := context.Background()
	svc := stock.NewService(pool, q)
	h := stock.NewHandler(svc)

	shopA := seedShop(ctx, t, q, "isolation-shop-a")
	shopB := seedShop(ctx, t, q, "isolation-shop-b")
	managerA := seedUser(ctx, t, q, shopA.ID, "manager-a", db.UserRoleManager)
	managerB := seedUser(ctx, t, q, shopB.ID, "manager-b", db.UserRoleManager)
	unitA := seedUnit(ctx, t, q, shopA.ID, "pcs")
	unitB := seedUnit(ctx, t, q, shopB.ID, "pcs")
	productA := seedProduct(ctx, t, q, shopA.ID, unitA.ID, "isolation-product-a")
	productB := seedProduct(ctx, t, q, shopB.ID, unitB.ID, "isolation-product-b")
	variantA := seedVariant(ctx, t, q, shopA.ID, productA.ID, "{}")
	variantB := seedVariant(ctx, t, q, shopB.ID, productB.ID, "{}")
	locationA := seedLocation(ctx, t, q, shopA.ID, "Main A")
	locationB := seedLocation(ctx, t, q, shopB.ID, "Main B")

	if _, err := svc.MoveInTx(ctx, stock.MoveParams{
		ShopID: shopA.ID, VariantID: variantA.ID, LocationID: locationA.ID,
		Kind: db.StockMovementKindPurchaseIn, Qty: d(t, "3.000"),
	}); err != nil {
		t.Fatalf("seed shop A stock: %v", err)
	}
	if _, err := svc.MoveInTx(ctx, stock.MoveParams{
		ShopID: shopB.ID, VariantID: variantB.ID, LocationID: locationB.ID,
		Kind: db.StockMovementKindPurchaseIn, Qty: d(t, "1.000"),
	}); err != nil {
		t.Fatalf("seed shop B stock: %v", err)
	}

	return isolationFixture{
		h: h, shopA: shopA, shopB: shopB, managerA: managerA, managerB: managerB,
		variantA: variantA, variantB: variantB, locationA: locationA, locationB: locationB,
	}
}

func TestListStockLevels_isolatedPerShop(t *testing.T) {
	f := newIsolationFixture(t)

	resp, err := f.h.ListStockLevels(ctxAs(f.shopA.ID, f.managerA), gen.ListStockLevelsRequestObject{})
	if err != nil {
		t.Fatalf("ListStockLevels(shop A): %v", err)
	}
	levels, ok := resp.(gen.ListStockLevels200JSONResponse)
	if !ok {
		t.Fatalf("response type = %T", resp)
	}
	if len(levels.Items) != 1 || levels.Items[0].VariantId != f.variantA.ID {
		t.Fatalf("shop A levels = %+v, want exactly variant A's row", levels.Items)
	}
	for _, item := range levels.Items {
		if item.VariantId == f.variantB.ID || item.LocationId == f.locationB.ID {
			t.Fatalf("shop A levels leaked a shop B row: %+v", item)
		}
	}
}

func TestListStockMovements_isolatedPerShop(t *testing.T) {
	f := newIsolationFixture(t)

	resp, err := f.h.ListStockMovements(ctxAs(f.shopA.ID, f.managerA), gen.ListStockMovementsRequestObject{})
	if err != nil {
		t.Fatalf("ListStockMovements(shop A): %v", err)
	}
	movements, ok := resp.(gen.ListStockMovements200JSONResponse)
	if !ok {
		t.Fatalf("response type = %T", resp)
	}
	if len(movements.Items) != 1 || movements.Items[0].VariantId != f.variantA.ID {
		t.Fatalf("shop A movements = %+v, want exactly variant A's row", movements.Items)
	}
}

func TestListLowStock_isolatedPerShop(t *testing.T) {
	f := newIsolationFixture(t)

	// Both variants are at/below the shop default threshold (2): A has 3
	// (not low), B has 1 (low) — chosen so a leak would be obvious either
	// way, not just an empty-vs-nonempty coincidence.
	resp, err := f.h.ListLowStock(ctxAs(f.shopB.ID, f.managerB), gen.ListLowStockRequestObject{})
	if err != nil {
		t.Fatalf("ListLowStock(shop B): %v", err)
	}
	low, ok := resp.(gen.ListLowStock200JSONResponse)
	if !ok {
		t.Fatalf("response type = %T", resp)
	}
	if len(low.Items) != 1 || low.Items[0].VariantId != f.variantB.ID {
		t.Fatalf("shop B low stock = %+v, want exactly variant B's row", low.Items)
	}

	respA, err := f.h.ListLowStock(ctxAs(f.shopA.ID, f.managerA), gen.ListLowStockRequestObject{})
	if err != nil {
		t.Fatalf("ListLowStock(shop A): %v", err)
	}
	lowA, ok := respA.(gen.ListLowStock200JSONResponse)
	if !ok {
		t.Fatalf("response type = %T", respA)
	}
	if len(lowA.Items) != 0 {
		t.Fatalf("shop A low stock = %+v, want none (3 > default threshold 2)", lowA.Items)
	}
}
