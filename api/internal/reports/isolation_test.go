package reports_test

import (
	"context"
	"testing"
	"time"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/reports"
)

// TestReports_isolatedPerShop proves shop_id scoping (ADR-004) on both
// report endpoints: shop A has a completed sale, shop B has none at all —
// shop B's manager must see an empty, zeroed-out report, not shop A's
// data. Mirrors api/internal/stock/isolation_test.go's own shape.
func TestReports_isolatedPerShop(t *testing.T) {
	_, q := newTestQueries(t)
	ctx := context.Background()
	h := reports.NewHandler(reports.NewService(q))

	shopA := seedShop(ctx, t, q, "isolation-shop-a")
	shopB := seedShop(ctx, t, q, "isolation-shop-b")
	managerA := seedUser(ctx, t, q, shopA.ID, "manager-a", db.UserRoleManager)
	managerB := seedUser(ctx, t, q, shopB.ID, "manager-b", db.UserRoleManager)
	cashierA := seedUser(ctx, t, q, shopA.ID, "cashier-a", db.UserRoleCashier)
	unitA := seedUnit(ctx, t, q, shopA.ID, "pcs")
	productA := seedProduct(ctx, t, q, shopA.ID, unitA.ID, "isolation-product-a")
	variantA := seedVariant(ctx, t, q, shopA.ID, productA.ID)
	locationA := seedLocation(ctx, t, q, shopA.ID, "Main A")

	newSale(ctx, t, q, shopA.ID, locationA.ID, cashierA.ID, db.SaleKindSale, nil,
		"50.00", "0.00", "50.00", []saleItemSpec{
			{variantID: variantA.ID, qty: "1.000", unitPrice: "50.00", unitCost: "10.00", lineTotal: "50.00"},
		})

	now := time.Now()
	from, to := now.AddDate(0, 0, -1), now.AddDate(0, 0, 1)

	// Shop B's manager: zero counts, "0.00" everywhere, no leak.
	summaryResp, err := h.GetSalesSummaryReport(ctxAs(shopB.ID, managerB), summaryParams(from, to))
	if err != nil {
		t.Fatalf("GetSalesSummaryReport(shop B): %v", err)
	}
	summaryB := summaryResp.(gen.GetSalesSummaryReport200JSONResponse)
	if summaryB.SalesCount != 0 {
		t.Fatalf("shop B salesCount = %d, want 0", summaryB.SalesCount)
	}
	assertDecimal(t, "shop B revenue", summaryB.Revenue, "0.00")
	assertDecimal(t, "shop B netRevenue", summaryB.NetRevenue, "0.00")
	if summaryB.Cost == nil || *summaryB.Cost != "0.00" {
		t.Fatalf("shop B cost = %v, want 0.00", summaryB.Cost)
	}

	byProductResp, err := h.ListSalesByProduct(ctxAs(shopB.ID, managerB), byProductParams(from, to, nil, nil))
	if err != nil {
		t.Fatalf("ListSalesByProduct(shop B): %v", err)
	}
	byProductB := byProductResp.(gen.ListSalesByProduct200JSONResponse)
	if len(byProductB.Items) != 0 {
		t.Fatalf("shop B by-product items = %+v, want none (shop A's sale must not leak)", byProductB.Items)
	}

	// Sanity: shop A's own manager does see its own sale — proves the
	// fixture is not just universally empty.
	summaryAResp, err := h.GetSalesSummaryReport(ctxAs(shopA.ID, managerA), summaryParams(from, to))
	if err != nil {
		t.Fatalf("GetSalesSummaryReport(shop A): %v", err)
	}
	summaryA := summaryAResp.(gen.GetSalesSummaryReport200JSONResponse)
	if summaryA.SalesCount != 1 {
		t.Fatalf("shop A salesCount = %d, want 1", summaryA.SalesCount)
	}
}
