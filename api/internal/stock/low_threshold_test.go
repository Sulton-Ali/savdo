package stock_test

import (
	"context"
	"testing"

	"github.com/oapi-codegen/nullable"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/catalog"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/stock"
)

// containsVariant reports whether items has a StockLowItem for variantID,
// returning it so the caller can assert on its Threshold too.
func containsVariant(items []gen.StockLowItem, variantID [16]byte) (gen.StockLowItem, bool) {
	for _, item := range items {
		if [16]byte(item.VariantId) == variantID {
			return item, true
		}
	}
	return gen.StockLowItem{}, false
}

// TestListLowStock_honoursProductThresholdOverride is D-44/D-50's end-to-
// end check across two modules: the shop default threshold is 2 (seeded),
// a variant sitting at a total of 3 is not low against that default, and
// PATCHing the product's own lowStockThreshold to 3 through
// catalog.Handler.UpdateProduct (the fix this task ships) makes
// stock.Handler.ListLowStock start reporting it, with the product's
// override as the item's effective threshold — proving the SQL's
// COALESCE(p.low_stock_threshold, s.low_stock_threshold) (already
// correct) actually sees a value once catalog wires the column.
func TestListLowStock_honoursProductThresholdOverride(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()

	shopRow := seedShop(ctx, t, q, "low-threshold-shop")
	if shopRow.LowStockThreshold != 2 {
		t.Fatalf("seeded shops.low_stock_threshold = %d, want the D-50 default of 2", shopRow.LowStockThreshold)
	}
	manager := seedUser(ctx, t, q, shopRow.ID, "manager-1", db.UserRoleManager)
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")
	product := seedProduct(ctx, t, q, shopRow.ID, unit.ID, "threshold-product")
	variant := seedVariant(ctx, t, q, shopRow.ID, product.ID, "{}")
	location := seedLocation(ctx, t, q, shopRow.ID, "Main")

	stockSvc := stock.NewService(pool, q)
	stockHandler := stock.NewHandler(stockSvc)
	if _, err := stockSvc.MoveInTx(ctx, stock.MoveParams{
		ShopID: shopRow.ID, VariantID: variant.ID, LocationID: location.ID,
		Kind: db.StockMovementKindPurchaseIn, Qty: d(t, "3.000"),
	}); err != nil {
		t.Fatalf("seed opening stock: %v", err)
	}

	mgrCtx := ctxAs(shopRow.ID, manager)

	// Before the override: 3 > the shop default of 2, so the variant is
	// not low.
	resp, err := stockHandler.ListLowStock(mgrCtx, gen.ListLowStockRequestObject{})
	if err != nil {
		t.Fatalf("ListLowStock (before override): %v", err)
	}
	before := resp.(gen.ListLowStock200JSONResponse)
	if _, found := containsVariant(before.Items, variant.ID); found {
		t.Fatalf("variant listed as low before any override: %+v", before.Items)
	}

	// PATCH the product's own threshold to 3 through the real catalog
	// handler — this is the wiring TestCreateProduct_lowStockThreshold and
	// TestUpdateProduct_lowStockThreshold cover in isolation; here it must
	// also be visible to stock's ListLow query.
	catalogSvc := catalog.NewService(pool, q, "uz", "/media")
	catalogHandler := catalog.NewHandler(catalogSvc)
	if _, err := catalogHandler.UpdateProduct(mgrCtx, gen.UpdateProductRequestObject{
		Id:   product.ID,
		Body: &gen.UpdateProductJSONRequestBody{LowStockThreshold: nullable.NewNullableWithValue(3)},
	}); err != nil {
		t.Fatalf("UpdateProduct(lowStockThreshold=3): %v", err)
	}

	resp, err = stockHandler.ListLowStock(mgrCtx, gen.ListLowStockRequestObject{})
	if err != nil {
		t.Fatalf("ListLowStock (after override): %v", err)
	}
	after := resp.(gen.ListLowStock200JSONResponse)
	item, found := containsVariant(after.Items, variant.ID)
	if !found {
		t.Fatalf("variant not listed as low after threshold override: %+v", after.Items)
	}
	if item.Threshold != 3 {
		t.Fatalf("item.Threshold = %d, want the product override 3", item.Threshold)
	}
}
