package reports_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/reports"
)

// d64Fixture builds the shared shop/location/cashier/product/variant plus
// the D-64 sale (subtotal 100 / discount 10 / total 90, two 50.00 lines at
// unit_cost 10.00 each) and its completed return of one full line
// (refunding 45.00, line1's net share) — the fixture both
// TestGetSalesSummaryReport_managerD64 and
// TestListSalesByProduct_managerD64 assert against (task's own acceptance
// numbers). Discount is split per line via D-64's rounding rule: each
// line's raw share is round(50 * 10 / 100, 2) = 5.00; the last line (by
// sale_items.id / insertion order) takes the remainder
// (10.00 - 5.00 = 5.00), so both lines land on a 5.00 share and a 45.00
// net revenue, matching this fixture either way.
type d64Fixture struct {
	shopID, locationID, cashierID, productID, variantID uuid.UUID
	sale                                                db.Sale
	saleItems                                           []db.SaleItem
}

func newD64Fixture(ctx context.Context, t *testing.T, q *db.Queries, slug string) d64Fixture {
	t.Helper()
	shopRow := seedShop(ctx, t, q, slug)
	cashier := seedUser(ctx, t, q, shopRow.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")
	product := seedProduct(ctx, t, q, shopRow.ID, unit.ID, slug+"-product")
	variant := seedVariant(ctx, t, q, shopRow.ID, product.ID)
	loc := seedLocation(ctx, t, q, shopRow.ID, "Main")

	sale, items := newSale(ctx, t, q, shopRow.ID, loc.ID, cashier.ID, db.SaleKindSale, nil,
		"100.00", "10.00", "90.00", []saleItemSpec{
			{variantID: variant.ID, qty: "1.000", unitPrice: "50.00", unitCost: "10.00", lineTotal: "50.00"},
			{variantID: variant.ID, qty: "1.000", unitPrice: "50.00", unitCost: "10.00", lineTotal: "50.00"},
		})
	// The return refunds line 1 in full: its net share (line_total 50.00
	// minus its 5.00 discount share) is 45.00 (D-61's formula, which D-64
	// reuses for reporting).
	newSale(ctx, t, q, shopRow.ID, loc.ID, cashier.ID, db.SaleKindReturn, &sale.ID,
		"45.00", "0.00", "45.00", []saleItemSpec{
			{variantID: variant.ID, qty: "1.000", unitPrice: "45.00", unitCost: "10.00", lineTotal: "45.00", originalSaleItemID: &items[0].ID},
		})

	return d64Fixture{
		shopID: shopRow.ID, locationID: loc.ID, cashierID: cashier.ID,
		productID: product.ID, variantID: variant.ID, sale: sale, saleItems: items,
	}
}

// summaryParams builds a manager+ GetSalesSummaryReportRequestObject for
// [from, to] inclusive, both YYYY-MM-DD in whatever timezone the caller
// means them (the handler applies the shop's own timezone).
func summaryParams(from, to time.Time) gen.GetSalesSummaryReportRequestObject {
	return gen.GetSalesSummaryReportRequestObject{Params: gen.GetSalesSummaryReportParams{
		From: dateParam(from), To: dateParam(to),
	}}
}

func TestGetSalesSummaryReport_managerD64(t *testing.T) {
	_, q := newTestQueries(t)
	ctx := context.Background()
	h := reports.NewHandler(reports.NewService(q))

	f := newD64Fixture(ctx, t, q, "summary-d64")
	owner := seedUser(ctx, t, q, f.shopID, "owner1", db.UserRoleOwner)

	now := time.Now()
	resp, err := h.GetSalesSummaryReport(ctxAs(f.shopID, owner), summaryParams(now.AddDate(0, 0, -1), now.AddDate(0, 0, 1)))
	if err != nil {
		t.Fatalf("GetSalesSummaryReport: %v", err)
	}
	body := resp.(gen.GetSalesSummaryReport200JSONResponse)

	assertDecimal(t, "revenue", body.Revenue, "90.00")
	assertDecimal(t, "discounts", body.Discounts, "10.00")
	assertDecimal(t, "refunds", body.Refunds, "45.00")
	assertDecimal(t, "netRevenue", body.NetRevenue, "45.00")
	if body.Cost == nil {
		t.Fatal("want cost present for manager+, got nil")
	}
	assertDecimal(t, "cost", *body.Cost, "10.00")
	if body.Margin == nil {
		t.Fatal("want margin present for manager+, got nil")
	}
	assertDecimal(t, "margin", *body.Margin, "35.00")
	if body.SalesCount != 1 {
		t.Fatalf("salesCount = %d, want 1", body.SalesCount)
	}
	if body.ReturnsCount != 1 {
		t.Fatalf("returnsCount = %d, want 1", body.ReturnsCount)
	}
	if body.CashierId.IsSpecified() && !body.CashierId.IsNull() {
		t.Fatalf("want cashierId null for a whole-shop manager+ summary, got a value")
	}
}

// TestGetSalesSummaryReport_voidedSaleExcluded proves a voided sale never
// contributes to the manager+ summary — VoidSale only ever runs against a
// `sale`-kind row in the real service, but the query itself filters on
// status = 'completed' regardless of kind, so voiding is enough here.
func TestGetSalesSummaryReport_voidedSaleExcluded(t *testing.T) {
	_, q := newTestQueries(t)
	ctx := context.Background()
	h := reports.NewHandler(reports.NewService(q))

	f := newD64Fixture(ctx, t, q, "summary-voided")
	owner := seedUser(ctx, t, q, f.shopID, "owner1", db.UserRoleOwner)

	extra, _ := newSale(ctx, t, q, f.shopID, f.locationID, f.cashierID, db.SaleKindSale, nil,
		"20.00", "0.00", "20.00", []saleItemSpec{
			{variantID: f.variantID, qty: "1.000", unitPrice: "20.00", unitCost: "5.00", lineTotal: "20.00"},
		})
	reason := "test void"
	if _, err := q.VoidSale(ctx, db.VoidSaleParams{ShopID: f.shopID, ID: extra.ID, VoidedBy: &owner.ID, VoidReason: &reason}); err != nil {
		t.Fatalf("VoidSale: %v", err)
	}

	now := time.Now()
	resp, err := h.GetSalesSummaryReport(ctxAs(f.shopID, owner), summaryParams(now.AddDate(0, 0, -1), now.AddDate(0, 0, 1)))
	if err != nil {
		t.Fatalf("GetSalesSummaryReport: %v", err)
	}
	body := resp.(gen.GetSalesSummaryReport200JSONResponse)

	// Same numbers as the D-64 fixture alone (TestGetSalesSummaryReport_managerD64)
	// — the voided extra sale (revenue 20.00) must not be added in.
	assertDecimal(t, "revenue", body.Revenue, "90.00")
	if body.SalesCount != 1 {
		t.Fatalf("salesCount = %d, want 1 (voided sale excluded)", body.SalesCount)
	}
}

// TestGetSalesSummaryReport_cashierOwnDay proves D-55: a cashier's request
// ignores every parameter sent, is scoped to today (shop timezone) and to
// that cashier's own sales, has no cost/margin, and echoes the effective
// from/to/cashierId.
func TestGetSalesSummaryReport_cashierOwnDay(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := reports.NewHandler(reports.NewService(q))

	shopRow := seedShop(ctx, t, q, "summary-cashier")
	cashier := seedUser(ctx, t, q, shopRow.ID, "cashier1", db.UserRoleCashier)
	otherCashier := seedUser(ctx, t, q, shopRow.ID, "cashier2", db.UserRoleCashier)
	loc := seedLocation(ctx, t, q, shopRow.ID, "Main")

	loc2, err := time.LoadLocation(shopRow.Timezone)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", shopRow.Timezone, err)
	}
	now := time.Now().In(loc2)
	// Noon, not midnight, so this fixture is never itself flaky near a
	// real local-midnight boundary.
	today := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, loc2)
	yesterday := today.AddDate(0, 0, -1)

	// Included: this cashier, today.
	insertSaleAt(ctx, t, pool, q, shopRow.ID, loc.ID, cashier.ID, today)
	// Excluded: a different cashier's sale, also today.
	insertSaleAt(ctx, t, pool, q, shopRow.ID, loc.ID, otherCashier.ID, today)
	// Excluded: this cashier, but yesterday (shop tz).
	insertSaleAt(ctx, t, pool, q, shopRow.ID, loc.ID, cashier.ID, yesterday)

	// Sent params are deliberately wrong/irrelevant — must be ignored.
	bogusFrom, bogusTo := yesterday.AddDate(0, 0, -30), yesterday.AddDate(0, 0, -20)
	resp, err := h.GetSalesSummaryReport(ctxAs(shopRow.ID, cashier), summaryParams(bogusFrom, bogusTo))
	if err != nil {
		t.Fatalf("GetSalesSummaryReport: %v", err)
	}
	body := resp.(gen.GetSalesSummaryReport200JSONResponse)

	if body.SalesCount != 1 {
		t.Fatalf("salesCount = %d, want 1 (own day only)", body.SalesCount)
	}
	// insertSaleAt's fixed amounts (10.00/0.00/10.00) — asserting the
	// actual money fields, not just the count, proves the other two
	// sales (another cashier's, and yesterday's) are excluded from the
	// sums too, not just from the count.
	assertDecimal(t, "revenue", body.Revenue, "10.00")
	assertDecimal(t, "refunds", body.Refunds, "0.00")
	assertDecimal(t, "netRevenue", body.NetRevenue, "10.00")
	if body.Cost != nil {
		t.Fatal("want cost absent for a cashier, got a value")
	}
	if body.Margin != nil {
		t.Fatal("want margin absent for a cashier, got a value")
	}
	wantDay := today.Format("2006-01-02")
	if got := body.From.Format("2006-01-02"); got != wantDay {
		t.Fatalf("from = %s, want effective today %s", got, wantDay)
	}
	if got := body.To.Format("2006-01-02"); got != wantDay {
		t.Fatalf("to = %s, want effective today %s", got, wantDay)
	}
	if !body.CashierId.IsSpecified() || body.CashierId.IsNull() {
		t.Fatal("want cashierId echoed (non-null) for a cashier caller")
	}
	if got, _ := body.CashierId.Get(); got != cashier.ID {
		t.Fatalf("cashierId = %s, want %s", got, cashier.ID)
	}
}

// TestGetSalesSummaryReport_shopTimezoneBoundary proves the report buckets
// a sale by its calendar date in the shop's own timezone (Asia/Tashkent,
// UTC+5), not in UTC (§ 04-DATA-MODEL.md rule 9) — a sale at 23:30 local
// on day D is reported for D. The half-open bounds this package computes
// ([D 00:00, D+1 00:00) in loc) are also exercised at their sharpest edge
// by a sale at 00:30 local on day D: converted to UTC that instant falls
// on calendar day D-1 (Tashkent is UTC+5, so local 00:30 = UTC (D-1)
// 19:30) — a bug that bucketed by the UTC calendar date of `completed_at`
// instead of applying loc first would misfile this sale under D-1.
func TestGetSalesSummaryReport_shopTimezoneBoundary(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := reports.NewHandler(reports.NewService(q))

	shopRow := seedShop(ctx, t, q, "summary-tz-boundary")
	owner := seedUser(ctx, t, q, shopRow.ID, "owner1", db.UserRoleOwner)
	loc := seedLocation(ctx, t, q, shopRow.ID, "Main")

	tz, err := time.LoadLocation(shopRow.Timezone)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", shopRow.Timezone, err)
	}
	day := time.Date(2024, time.June, 15, 0, 0, 0, 0, tz)

	lateEvening := time.Date(2024, time.June, 15, 23, 30, 0, 0, tz)
	insertSaleAt(ctx, t, pool, q, shopRow.ID, loc.ID, owner.ID, lateEvening)

	earlyMorning := time.Date(2024, time.June, 16, 0, 30, 0, 0, tz)
	insertSaleAt(ctx, t, pool, q, shopRow.ID, loc.ID, owner.ID, earlyMorning)

	// The 23:30 sale (day 15) counts on day 15, not day 16.
	assertSalesCountForDay(t, h, shopRow.ID, owner, day, 1)
	assertSalesCountForDay(t, h, shopRow.ID, owner, day.AddDate(0, 0, 1), 1) // the 00:30 sale, on day 16.

	// The 00:30 sale (local day 16, UTC calendar day 15) must not leak
	// into day 15's count on top of the 23:30 sale already counted there.
	// day 15 already asserted == 1 above (not 2), which is the actual
	// regression guard for the UTC-vs-loc bucketing bug this test exists
	// for.
}

func assertSalesCountForDay(t *testing.T, h *reports.Handler, shopID uuid.UUID, owner db.User, day time.Time, want int) {
	t.Helper()
	resp, err := h.GetSalesSummaryReport(ctxAs(shopID, owner), summaryParams(day, day))
	if err != nil {
		t.Fatalf("GetSalesSummaryReport(%s): %v", day.Format("2006-01-02"), err)
	}
	body := resp.(gen.GetSalesSummaryReport200JSONResponse)
	if body.SalesCount != want {
		t.Fatalf("salesCount for %s = %d, want %d", day.Format("2006-01-02"), body.SalesCount, want)
	}
}

func TestGetSalesSummaryReport_managerRequiresFromTo(t *testing.T) {
	_, q := newTestQueries(t)
	ctx := context.Background()
	h := reports.NewHandler(reports.NewService(q))

	shopRow := seedShop(ctx, t, q, "summary-required")
	owner := seedUser(ctx, t, q, shopRow.ID, "owner1", db.UserRoleOwner)

	_, err := h.GetSalesSummaryReport(ctxAs(shopRow.ID, owner), gen.GetSalesSummaryReportRequestObject{})
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 400 {
		t.Fatalf("error = %v, want 400 VALIDATION_FAILED", err)
	}
	fields, _ := apiErr.Details["fields"].(map[string]string)
	if fields["from"] != "required" || fields["to"] != "required" {
		t.Fatalf("details.fields = %v, want from/to required", apiErr.Details)
	}
}
