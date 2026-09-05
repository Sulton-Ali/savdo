package sales_test

// TestReconciliation_summaryAndByProductAgreeAfterAPartialReturn is the
// review's own reconciliation check (D-64/D-61, docs/00-DECISIONS.md):
// running a real discounted multi-product sale and a real partial return
// through the actual write path (CreateSaleTx/CreateSaleReturnTx, not a
// hand-crafted fixture) must still leave the two report queries agreeing
// with each other — Σ by-product revenue equals the summary's net revenue
// (revenue - refunds) and Σ by-product cost equals the summary's cost —
// the same invariant internal/db/sales_schema_test.go already proves at
// the SQL level with raw fixtures, exercised here end to end through the
// service that actually writes the rows a real sale and return produce.
// The fixture is D-64's own rounding-remainder shape (three lines,
// 33.33/33.33/33.34, on three different products, fixed discount 10.00)
// with the return specifically targeting the greatest-id line — the one
// whose discount share is the leftover remainder, not a plain rounded
// share — so the reconciliation is checked on exactly the line where a
// share/remainder mismatch would first show up.

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/money"
	"github.com/Sulton-Ali/savdo/api/internal/sales"
)

func TestReconciliation_summaryAndByProductAgreeAfterAPartialReturn(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "reconciliation")
	owner := seedUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	productA := seedProduct(ctx, t, q, shop.ID, unit.ID, "reconciliation-a", "33.33", productOpts{costPrice: strPtr("10.00")})
	productB := seedProduct(ctx, t, q, shop.ID, unit.ID, "reconciliation-b", "33.33", productOpts{costPrice: strPtr("12.00")})
	productC := seedProduct(ctx, t, q, shop.ID, unit.ID, "reconciliation-c", "33.34", productOpts{costPrice: strPtr("15.00")})
	variantA := seedVariant(ctx, t, q, shop.ID, productA.ID)
	variantB := seedVariant(ctx, t, q, shop.ID, productB.ID)
	variantC := seedVariant(ctx, t, q, shop.ID, productC.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	ownerCtx := ctxAs(shop.ID, owner)
	stockIn(ctx, t, pool, q, shop.ID, variantA.ID, loc.ID, "10.000")
	stockIn(ctx, t, pool, q, shop.ID, variantB.ID, loc.ID, "10.000")
	stockIn(ctx, t, pool, q, shop.ID, variantC.ID, loc.ID, "10.000")

	// subtotal 100.00 (33.33+33.33+33.34), fixed discount 10.00 -> total
	// 90.00. D-64: the two non-last lines each get round(33.33*10/100, 2)
	// = 3.33; the greatest-id line takes the remainder, 10.00 - 3.33 -
	// 3.33 = 3.34.
	sale, err := createSale(ownerCtx, t, h, pool, q, &gen.SaleCreate{
		LocationId: loc.ID,
		Items: []gen.SaleItemCreate{
			{VariantId: variantA.ID, Qty: "1.000"},
			{VariantId: variantB.ID, Qty: "1.000"},
			{VariantId: variantC.ID, Qty: "1.000"},
		},
		Payment:  gen.SalePaymentCreate{Method: gen.Cash},
		Discount: &gen.SaleDiscount{Type: gen.Fixed, Value: "10.00"},
	})
	if err != nil {
		t.Fatalf("createSale: %v", err)
	}
	if sale.Subtotal != "100.00" || sale.DiscountAmount != "10.00" || sale.Total != "90.00" {
		t.Fatalf("Subtotal/DiscountAmount/Total = %s/%s/%s, want 100.00/10.00/90.00", sale.Subtotal, sale.DiscountAmount, sale.Total)
	}

	// Partially return the greatest-id line — the one whose discount
	// share is the D-64 remainder, not a plain rounded share.
	last := lastByID(sale.Items)
	if _, err := createSaleReturn(ownerCtx, t, h, pool, q, sale.Id, &gen.SaleReturnCreate{
		Items: []gen.SaleReturnItemCreate{{SaleItemId: last.Id, Qty: "1.000"}},
	}); err != nil {
		t.Fatalf("createSaleReturn: %v", err)
	}

	from := time.Now().Add(-time.Hour)
	to := time.Now().Add(time.Hour)

	summary, err := q.SalesSummaryForStaff(ctx, db.SalesSummaryForStaffParams{ShopID: shop.ID, From: from, To: to})
	if err != nil {
		t.Fatalf("SalesSummaryForStaff: %v", err)
	}
	revenue, err := money.FromNumeric(summary.Revenue)
	if err != nil {
		t.Fatalf("summary revenue: %v", err)
	}
	refunds, err := money.FromNumeric(summary.Refunds)
	if err != nil {
		t.Fatalf("summary refunds: %v", err)
	}
	cost, err := money.FromNumeric(summary.Cost)
	if err != nil {
		t.Fatalf("summary cost: %v", err)
	}
	netRevenue := revenue.Sub(refunds)

	byProduct, err := q.SalesByProduct(ctx, db.SalesByProductParams{
		ShopID: shop.ID, From: from, To: to, Limit: 100, Locale: "uz",
	})
	if err != nil {
		t.Fatalf("SalesByProduct: %v", err)
	}
	if len(byProduct) != 3 {
		t.Fatalf("SalesByProduct rows = %d, want 3", len(byProduct))
	}

	revenueSum := decimal.Zero
	costSum := decimal.Zero
	for _, row := range byProduct {
		rowRevenue, err := money.FromNumeric(row.Revenue)
		if err != nil {
			t.Fatalf("by-product revenue: %v", err)
		}
		rowCost, err := money.FromNumeric(row.Cost)
		if err != nil {
			t.Fatalf("by-product cost: %v", err)
		}
		revenueSum = revenueSum.Add(rowRevenue)
		costSum = costSum.Add(rowCost)
	}

	if !revenueSum.Equal(netRevenue) {
		t.Fatalf("Σ by-product revenue = %s, want summary netRevenue (revenue %s - refunds %s) = %s",
			revenueSum, revenue, refunds, netRevenue)
	}
	if !costSum.Equal(cost) {
		t.Fatalf("Σ by-product cost = %s, want summary cost %s", costSum, cost)
	}
}
