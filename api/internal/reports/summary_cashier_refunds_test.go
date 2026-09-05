package reports_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/money"
	"github.com/Sulton-Ali/savdo/api/internal/reports"
)

// assertNumeric compares a raw pgtype.Numeric column (the sqlc row type,
// before the handler's own requiredDecimal renders it to the wire) against
// its expected decimal literal — used by the direct-query test below,
// which calls SalesSummaryForStaff without going through the handler.
func assertNumeric(t *testing.T, label string, got pgtype.Numeric, want string) {
	t.Helper()
	d, err := money.FromNumeric(got)
	if err != nil {
		t.Fatalf("%s: money.FromNumeric: %v", label, err)
	}
	if s := money.String(d); s != want {
		t.Fatalf("%s = %q, want %q", label, s, want)
	}
}

// d71Fixture mirrors newD64Fixture but the return is created by a manager
// against the cashier's sale (D-58: a return's own cashier_id is whoever
// processed it, typically a manager, not the original cashier) — the
// shape D-71 exists for. Sale: 100.00, one line, unit_cost 40.00. Return:
// a partial 30.00 refund of that same line, cashier_id = the manager's.
type d71Fixture struct {
	shop    db.Shop
	cashier db.User
	manager db.User
}

func newD71Fixture(ctx context.Context, t *testing.T, q *db.Queries, slug string) d71Fixture {
	t.Helper()
	shopRow := seedShop(ctx, t, q, slug)
	cashier := seedUser(ctx, t, q, shopRow.ID, "cashier1", db.UserRoleCashier)
	manager := seedUser(ctx, t, q, shopRow.ID, "manager1", db.UserRoleManager)
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")
	product := seedProduct(ctx, t, q, shopRow.ID, unit.ID, slug+"-product")
	variant := seedVariant(ctx, t, q, shopRow.ID, product.ID)
	loc := seedLocation(ctx, t, q, shopRow.ID, "Main")

	sale, items := newSale(ctx, t, q, shopRow.ID, loc.ID, cashier.ID, db.SaleKindSale, nil,
		"100.00", "0.00", "100.00", []saleItemSpec{
			{variantID: variant.ID, qty: "1.000", unitPrice: "100.00", unitCost: "40.00", lineTotal: "100.00"},
		})
	// Created by the manager (D-58) — its own cashier_id is the
	// manager's, distinct from the original sale's cashier.
	newSale(ctx, t, q, shopRow.ID, loc.ID, manager.ID, db.SaleKindReturn, &sale.ID,
		"30.00", "0.00", "30.00", []saleItemSpec{
			{variantID: variant.ID, qty: "1.000", unitPrice: "30.00", unitCost: "40.00", lineTotal: "30.00", originalSaleItemID: &items[0].ID},
		})

	return d71Fixture{shop: shopRow, cashier: cashier, manager: manager}
}

// TestGetSalesSummaryReport_cashierNetsOwnReturns proves D-71: a
// cashier's own-day summary nets a refund against their own sale even
// though the return itself was created by a manager, not by the cashier.
func TestGetSalesSummaryReport_cashierNetsOwnReturns(t *testing.T) {
	_, q := newTestQueries(t)
	ctx := context.Background()
	h := reports.NewHandler(reports.NewService(q))

	f := newD71Fixture(ctx, t, q, "d71-cashier-nets")

	resp, err := h.GetSalesSummaryReport(ctxAs(f.shop.ID, f.cashier), gen.GetSalesSummaryReportRequestObject{})
	if err != nil {
		t.Fatalf("GetSalesSummaryReport: %v", err)
	}
	body := resp.(gen.GetSalesSummaryReport200JSONResponse)

	if body.SalesCount != 1 {
		t.Fatalf("salesCount = %d, want 1", body.SalesCount)
	}
	if body.ReturnsCount != 1 {
		t.Fatalf("returnsCount = %d, want 1 (D-71: return attributed to the original sale's cashier)", body.ReturnsCount)
	}
	assertDecimal(t, "revenue", body.Revenue, "100.00")
	assertDecimal(t, "refunds", body.Refunds, "30.00")
	assertDecimal(t, "netRevenue", body.NetRevenue, "70.00")
	if body.Cost != nil {
		t.Fatal("want cost absent for a cashier, got a value")
	}
	if body.Margin != nil {
		t.Fatal("want margin absent for a cashier, got a value")
	}
	if !body.CashierId.IsSpecified() || body.CashierId.IsNull() {
		t.Fatal("want cashierId echoed (non-null) for a cashier caller")
	}
	if got, _ := body.CashierId.Get(); got != f.cashier.ID {
		t.Fatalf("cashierId = %s, want %s", got, f.cashier.ID)
	}
}

// TestGetSalesSummaryReport_cashierExcludesOtherCashiersReturn proves the
// D-71 attribution is scoped correctly: a return of a *different*
// cashier's sale must not appear in this cashier's own-day summary, even
// though both sales happen today and the return is created by the same
// manager either way.
func TestGetSalesSummaryReport_cashierExcludesOtherCashiersReturn(t *testing.T) {
	_, q := newTestQueries(t)
	ctx := context.Background()
	h := reports.NewHandler(reports.NewService(q))

	shopRow := seedShop(ctx, t, q, "d71-exclude-other")
	cashier := seedUser(ctx, t, q, shopRow.ID, "cashier1", db.UserRoleCashier)
	otherCashier := seedUser(ctx, t, q, shopRow.ID, "cashier2", db.UserRoleCashier)
	manager := seedUser(ctx, t, q, shopRow.ID, "manager1", db.UserRoleManager)
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")
	product := seedProduct(ctx, t, q, shopRow.ID, unit.ID, "d71-exclude-other-product")
	variant := seedVariant(ctx, t, q, shopRow.ID, product.ID)
	loc := seedLocation(ctx, t, q, shopRow.ID, "Main")

	// otherCashier's sale, returned by the manager — must never surface
	// in `cashier`'s own-day summary.
	otherSale, otherItems := newSale(ctx, t, q, shopRow.ID, loc.ID, otherCashier.ID, db.SaleKindSale, nil,
		"50.00", "0.00", "50.00", []saleItemSpec{
			{variantID: variant.ID, qty: "1.000", unitPrice: "50.00", unitCost: "20.00", lineTotal: "50.00"},
		})
	newSale(ctx, t, q, shopRow.ID, loc.ID, manager.ID, db.SaleKindReturn, &otherSale.ID,
		"20.00", "0.00", "20.00", []saleItemSpec{
			{variantID: variant.ID, qty: "1.000", unitPrice: "20.00", unitCost: "20.00", lineTotal: "20.00", originalSaleItemID: &otherItems[0].ID},
		})

	resp, err := h.GetSalesSummaryReport(ctxAs(shopRow.ID, cashier), gen.GetSalesSummaryReportRequestObject{})
	if err != nil {
		t.Fatalf("GetSalesSummaryReport: %v", err)
	}
	body := resp.(gen.GetSalesSummaryReport200JSONResponse)

	if body.SalesCount != 0 {
		t.Fatalf("salesCount = %d, want 0", body.SalesCount)
	}
	if body.ReturnsCount != 0 {
		t.Fatalf("returnsCount = %d, want 0 (another cashier's return must not leak in)", body.ReturnsCount)
	}
	assertDecimal(t, "revenue", body.Revenue, "0.00")
	assertDecimal(t, "refunds", body.Refunds, "0.00")
	assertDecimal(t, "netRevenue", body.NetRevenue, "0.00")
}

// TestGetSalesSummaryReport_cashierNetRevenueCanGoNegative proves D-71's
// other edge: a return is counted on the day it happens, not the day of
// the original sale, so a cashier's own-day summary can show refunds (and
// a negative netRevenue) on a day it made no sales of its own at all —
// cashier C sold yesterday; a manager returns part of that sale today.
func TestGetSalesSummaryReport_cashierNetRevenueCanGoNegative(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := reports.NewHandler(reports.NewService(q))

	shopRow := seedShop(ctx, t, q, "d71-negative")
	cashier := seedUser(ctx, t, q, shopRow.ID, "cashier1", db.UserRoleCashier)
	manager := seedUser(ctx, t, q, shopRow.ID, "manager1", db.UserRoleManager)
	loc := seedLocation(ctx, t, q, shopRow.ID, "Main")

	tz, err := time.LoadLocation(shopRow.Timezone)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", shopRow.Timezone, err)
	}
	now := time.Now().In(tz)
	yesterdayNoon := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, tz).AddDate(0, 0, -1)

	// Yesterday's sale (10.00, insertSaleAt's fixed amount) — out of
	// today's window entirely.
	saleID := insertSaleAt(ctx, t, pool, q, shopRow.ID, loc.ID, cashier.ID, yesterdayNoon)

	// Today's return against it, created by the manager (D-58); no items
	// needed — SalesSummaryForCashier never joins sale_items.
	newSale(ctx, t, q, shopRow.ID, loc.ID, manager.ID, db.SaleKindReturn, &saleID,
		"30.00", "0.00", "30.00", nil)

	resp, err := h.GetSalesSummaryReport(ctxAs(shopRow.ID, cashier), gen.GetSalesSummaryReportRequestObject{})
	if err != nil {
		t.Fatalf("GetSalesSummaryReport: %v", err)
	}
	body := resp.(gen.GetSalesSummaryReport200JSONResponse)

	if body.SalesCount != 0 {
		t.Fatalf("salesCount = %d, want 0 (no sales today)", body.SalesCount)
	}
	if body.ReturnsCount != 1 {
		t.Fatalf("returnsCount = %d, want 1 (counted on the day the return happens)", body.ReturnsCount)
	}
	assertDecimal(t, "revenue", body.Revenue, "0.00")
	assertDecimal(t, "refunds", body.Refunds, "30.00")
	assertDecimal(t, "netRevenue", body.NetRevenue, "-30.00")
	if body.Cost != nil {
		t.Fatal("want cost absent for a cashier, got a value")
	}
	if body.Margin != nil {
		t.Fatal("want margin absent for a cashier, got a value")
	}
}

// TestSalesSummaryForStaff_cashierIdAttributesReturnToOriginalCashier
// exercises SalesSummaryForStaff's own cashier_id filter directly (no
// handler today passes it — GetSalesSummaryReportParams has no cashierId
// query parameter for manager+ — but the query supports it for D-71
// symmetry with SalesSummaryForCashier, and it must apply the same
// attribution rule): filtering by the original sale's cashier picks up
// the return; filtering by the return's own cashier_id (the manager who
// created it) must not, since that manager made no sale of their own and
// the return is not theirs by the D-71 rule.
func TestSalesSummaryForStaff_cashierIdAttributesReturnToOriginalCashier(t *testing.T) {
	_, q := newTestQueries(t)
	ctx := context.Background()

	f := newD71Fixture(ctx, t, q, "d71-staff-filter")

	from := time.Now().AddDate(0, 0, -1)
	to := time.Now().AddDate(0, 0, 1)

	byOriginalCashier, err := q.SalesSummaryForStaff(ctx, db.SalesSummaryForStaffParams{
		ShopID: f.shop.ID, From: from, To: to, CashierID: &f.cashier.ID,
	})
	if err != nil {
		t.Fatalf("SalesSummaryForStaff(cashier=original): %v", err)
	}
	if byOriginalCashier.SalesCount != 1 {
		t.Fatalf("salesCount = %d, want 1", byOriginalCashier.SalesCount)
	}
	if byOriginalCashier.ReturnsCount != 1 {
		t.Fatalf("returnsCount = %d, want 1 (D-71: attributed to the original sale's cashier)", byOriginalCashier.ReturnsCount)
	}
	assertNumeric(t, "revenue", byOriginalCashier.Revenue, "100.00")
	assertNumeric(t, "refunds", byOriginalCashier.Refunds, "30.00")
	// cost = sold (1 * 40.00) - returned (1 * 40.00) = 0.00.
	assertNumeric(t, "cost", byOriginalCashier.Cost, "0.00")

	byReturnsOwnCashier, err := q.SalesSummaryForStaff(ctx, db.SalesSummaryForStaffParams{
		ShopID: f.shop.ID, From: from, To: to, CashierID: &f.manager.ID,
	})
	if err != nil {
		t.Fatalf("SalesSummaryForStaff(cashier=manager): %v", err)
	}
	if byReturnsOwnCashier.SalesCount != 0 {
		t.Fatalf("salesCount = %d, want 0 (the manager made no sale of their own)", byReturnsOwnCashier.SalesCount)
	}
	if byReturnsOwnCashier.ReturnsCount != 0 {
		t.Fatalf("returnsCount = %d, want 0 (D-71: not counted against the return's own cashier_id)", byReturnsOwnCashier.ReturnsCount)
	}
	assertNumeric(t, "revenue", byReturnsOwnCashier.Revenue, "0.00")
	assertNumeric(t, "refunds", byReturnsOwnCashier.Refunds, "0.00")
	assertNumeric(t, "cost", byReturnsOwnCashier.Cost, "0.00")
}
