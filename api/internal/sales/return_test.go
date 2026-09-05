package sales_test

// Integration tests for CreateSaleReturnTx (docs/04-DATA-MODEL.md § 4
// Rules D-58/D-61/D-64/D-66) — mirrors create_test.go's own shape:
// newTestQueries per test, real Postgres via testcontainers, calling the
// handler method directly on a transaction this file opens and commits/
// rolls back itself (createSaleReturn below), the same shape
// httpx.CreateSaleReturn gives CreateSaleReturnTx via httpx.Idempotent in
// production (internal/httpx/sales_test.go covers the Idempotency-Key
// replay/reuse behaviour itself, mirrors create_test.go's own split).

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/sales"
)

// createSaleReturn runs CreateSaleReturnTx on its own transaction,
// committing on success and rolling back on error — mirrors createSale
// (sales_test.go)/voidSale (void_test.go).
func createSaleReturn(ctx context.Context, t *testing.T, h *sales.Handler, pool *pgxpool.Pool, q *db.Queries, originalSaleID uuid.UUID, body *gen.SaleReturnCreate) (gen.Sale, error) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("createSaleReturn: begin: %v", err)
	}
	qtx := q.WithTx(tx)
	resp, err := h.CreateSaleReturnTx(ctx, qtx, originalSaleID, body)
	if err != nil {
		_ = tx.Rollback(ctx)
		return gen.Sale{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("createSaleReturn: commit: %v", err)
	}
	return resp, nil
}

// lastByID returns items' element with the greatest Id.String() — the
// D-64 "last line by sale_items.id" the remainder discount share lands
// on, determined directly from the response rather than assumed from
// insertion order.
func lastByID(items []gen.SaleItem) gen.SaleItem {
	last := items[0]
	for _, it := range items[1:] {
		if it.Id.String() > last.Id.String() {
			last = it
		}
	}
	return last
}

func TestCreateSaleReturn_partialReturnRestoresStockAndFlagsHasReturns(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "return-partial")
	owner := seedUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "return-partial-product", "20.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	ownerCtx := ctxAs(shop.ID, owner)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "5.000")

	sale, err := createSale(ownerCtx, t, h, pool, q, saleBody(loc.ID, variant.ID, "2.000"))
	if err != nil {
		t.Fatalf("createSale: %v", err)
	}
	levelAfterSale, _ := readLevel(ctx, t, pool, shop.ID, variant.ID, loc.ID)
	if !levelAfterSale.Equal(d(t, "3.000")) {
		t.Fatalf("level after sale = %s, want 3.000", levelAfterSale)
	}

	ret, err := createSaleReturn(ownerCtx, t, h, pool, q, sale.Id, &gen.SaleReturnCreate{
		Items: []gen.SaleReturnItemCreate{{SaleItemId: sale.Items[0].Id, Qty: "1.000"}},
	})
	if err != nil {
		t.Fatalf("createSaleReturn: %v", err)
	}
	if ret.Kind != gen.SaleKind(db.SaleKindReturn) {
		t.Fatalf("Kind = %q, want return", ret.Kind)
	}
	originalID, err := ret.OriginalSaleId.Get()
	if err != nil || originalID != sale.Id {
		t.Fatalf("OriginalSaleId = %v (err %v), want %s", ret.OriginalSaleId, err, sale.Id)
	}
	if ret.Total != "20.00" {
		t.Fatalf("return Total = %q, want 20.00 (no discount on the original sale)", ret.Total)
	}

	levelAfterReturn, _ := readLevel(ctx, t, pool, shop.ID, variant.ID, loc.ID)
	if !levelAfterReturn.Equal(d(t, "4.000")) {
		t.Fatalf("level after return = %s, want 4.000 (stock back at the sale's location)", levelAfterReturn)
	}
	if got := countMovements(ctx, t, pool, shop.ID, variant.ID, loc.ID, db.StockMovementKindReturnIn); got != 1 {
		t.Fatalf("return_in movements = %d, want 1", got)
	}

	resp, err := h.GetSale(ownerCtx, gen.GetSaleRequestObject{Id: sale.Id})
	if err != nil {
		t.Fatalf("GetSale (original): %v", err)
	}
	got, ok := resp.(gen.GetSale200JSONResponse)
	if !ok {
		t.Fatalf("GetSale response type = %T", resp)
	}
	if !got.HasReturns {
		t.Fatal("HasReturns = false, want true after a completed return")
	}
	if got.Items[0].ReturnedQty != "1.000" {
		t.Fatalf("ReturnedQty = %q, want 1.000", got.Items[0].ReturnedQty)
	}
}

// TestCreateSaleReturn_refundMathTwoEqualLinesFullReturn is D-64's own
// worked fixture: two 50.00 lines, a 10.00 fixed discount, returning line
// 1 in full must refund exactly 45.00 (its 50.00 line_total minus its
// 5.00 discount share).
func TestCreateSaleReturn_refundMathTwoEqualLinesFullReturn(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "return-refund-two-lines")
	owner := seedUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	productA := seedProduct(ctx, t, q, shop.ID, unit.ID, "return-refund-a", "50.00", productOpts{})
	productB := seedProduct(ctx, t, q, shop.ID, unit.ID, "return-refund-b", "50.00", productOpts{})
	variantA := seedVariant(ctx, t, q, shop.ID, productA.ID)
	variantB := seedVariant(ctx, t, q, shop.ID, productB.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	ownerCtx := ctxAs(shop.ID, owner)
	stockIn(ctx, t, pool, q, shop.ID, variantA.ID, loc.ID, "5.000")
	stockIn(ctx, t, pool, q, shop.ID, variantB.ID, loc.ID, "5.000")

	sale, err := createSale(ownerCtx, t, h, pool, q, &gen.SaleCreate{
		LocationId: loc.ID,
		Items: []gen.SaleItemCreate{
			{VariantId: variantA.ID, Qty: "1.000"},
			{VariantId: variantB.ID, Qty: "1.000"},
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

	ret, err := createSaleReturn(ownerCtx, t, h, pool, q, sale.Id, &gen.SaleReturnCreate{
		Items: []gen.SaleReturnItemCreate{{SaleItemId: sale.Items[0].Id, Qty: "1.000"}},
	})
	if err != nil {
		t.Fatalf("createSaleReturn: %v", err)
	}
	if ret.Total != "45.00" {
		t.Fatalf("return Total = %q, want 45.00", ret.Total)
	}
	if len(ret.Items) != 1 || ret.Items[0].LineTotal != "45.00" {
		t.Fatalf("return Items = %+v, want one line with lineTotal 45.00", ret.Items)
	}
}

// TestCreateSaleReturn_refundMathThreeLinesRemainderShare is D-64's
// rounding-remainder fixture: three lines of 33.33/33.33/33.34 (subtotal
// 100.00) with a 10.00 fixed discount — the two non-last lines each get
// round(33.33*10/100, 2) = 3.33, and the line with the greatest
// sale_items.id takes the remainder (10.00 - 3.33 - 3.33 = 3.34), so
// returning that line in full refunds exactly 33.34 - 3.34 = 30.00.
func TestCreateSaleReturn_refundMathThreeLinesRemainderShare(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "return-refund-three-lines")
	owner := seedUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	productA := seedProduct(ctx, t, q, shop.ID, unit.ID, "return-refund-3a", "33.33", productOpts{})
	productB := seedProduct(ctx, t, q, shop.ID, unit.ID, "return-refund-3b", "33.33", productOpts{})
	productC := seedProduct(ctx, t, q, shop.ID, unit.ID, "return-refund-3c", "33.34", productOpts{})
	variantA := seedVariant(ctx, t, q, shop.ID, productA.ID)
	variantB := seedVariant(ctx, t, q, shop.ID, productB.ID)
	variantC := seedVariant(ctx, t, q, shop.ID, productC.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	ownerCtx := ctxAs(shop.ID, owner)
	for _, v := range []uuid.UUID{variantA.ID, variantB.ID, variantC.ID} {
		stockIn(ctx, t, pool, q, shop.ID, v, loc.ID, "5.000")
	}

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
	if sale.Subtotal != "100.00" {
		t.Fatalf("Subtotal = %q, want 100.00", sale.Subtotal)
	}

	last := lastByID(sale.Items)
	ret, err := createSaleReturn(ownerCtx, t, h, pool, q, sale.Id, &gen.SaleReturnCreate{
		Items: []gen.SaleReturnItemCreate{{SaleItemId: last.Id, Qty: "1.000"}},
	})
	if err != nil {
		t.Fatalf("createSaleReturn: %v", err)
	}
	if ret.Total != "30.00" {
		t.Fatalf("return Total = %q, want 30.00 (net of the remainder discount share)", ret.Total)
	}
}

// TestCreateSaleReturn_partialQuantityRefundsSumToNet is D-61's own
// exactness guarantee: returning a two-unit line one unit at a time must
// have its two refunds sum to exactly the line's net (line_total minus
// its discount share), regardless of any rounding the first partial
// return's round(net*r/q, 2) introduces — the second (fully-returning)
// return always takes net minus whatever the first already took, never a
// second independently-rounded half.
func TestCreateSaleReturn_partialQuantityRefundsSumToNet(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "return-partial-qty")
	owner := seedUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "return-partial-qty-product", "25.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	ownerCtx := ctxAs(shop.ID, owner)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "5.000")

	// line_total 50.00, discount 0.99 (the only line, so it takes the
	// whole discount as its "remainder" share) -> net 49.01, an odd-cent
	// net where round(net/2, 2) cannot land on an exact half.
	sale, err := createSale(ownerCtx, t, h, pool, q, &gen.SaleCreate{
		LocationId: loc.ID,
		Items:      []gen.SaleItemCreate{{VariantId: variant.ID, Qty: "2.000"}},
		Payment:    gen.SalePaymentCreate{Method: gen.Cash},
		Discount:   &gen.SaleDiscount{Type: gen.Fixed, Value: "0.99"},
	})
	if err != nil {
		t.Fatalf("createSale: %v", err)
	}

	ret1, err := createSaleReturn(ownerCtx, t, h, pool, q, sale.Id, &gen.SaleReturnCreate{
		Items: []gen.SaleReturnItemCreate{{SaleItemId: sale.Items[0].Id, Qty: "1.000"}},
	})
	if err != nil {
		t.Fatalf("first partial return: %v", err)
	}
	ret2, err := createSaleReturn(ownerCtx, t, h, pool, q, sale.Id, &gen.SaleReturnCreate{
		Items: []gen.SaleReturnItemCreate{{SaleItemId: sale.Items[0].Id, Qty: "1.000"}},
	})
	if err != nil {
		t.Fatalf("second partial return: %v", err)
	}

	sum := d(t, ret1.Total).Add(d(t, ret2.Total))
	if sum.StringFixed(2) != "49.01" {
		t.Fatalf("refund1 + refund2 = %s + %s = %s, want 49.01", ret1.Total, ret2.Total, sum.StringFixed(2))
	}
}

// TestCreateSaleReturn_overRefundCapAcrossEightSteps is the over-refund
// cap's own worked fixture (review ruling): a line of qty 8 with net
// 0.20, returned one unit at a time. Each step's uncapped
// round(net*1/8, 2) = round(0.025, 2) = 0.03 (round-half-away-from-zero,
// shopspring/decimal's own Round), so steps 1-6 each take 0.03
// (cumulative 0.18); step 7's uncapped 0.03 would push the cumulative to
// 0.21 > net, so the cap takes over and it refunds only 0.02 (cumulative
// exactly 0.20); step 8 is the line's last unit (newQty == soldQty), so
// it takes the exact remainder, 0.00 — the cumulative refund across all
// eight steps must never exceed 0.20 and must land on it exactly.
func TestCreateSaleReturn_overRefundCapAcrossEightSteps(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "return-cap-eight-steps")
	owner := seedUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "return-cap-eight-steps-product", "0.10", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	ownerCtx := ctxAs(shop.ID, owner)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "10.000")

	// line_total 0.10*8 = 0.80, discount 0.60 (the only line, so it takes
	// the whole discount) -> net 0.20.
	sale, err := createSale(ownerCtx, t, h, pool, q, &gen.SaleCreate{
		LocationId: loc.ID,
		Items:      []gen.SaleItemCreate{{VariantId: variant.ID, Qty: "8.000"}},
		Payment:    gen.SalePaymentCreate{Method: gen.Cash},
		Discount:   &gen.SaleDiscount{Type: gen.Fixed, Value: "0.60"},
	})
	if err != nil {
		t.Fatalf("createSale: %v", err)
	}

	wantEachStep := []string{"0.03", "0.03", "0.03", "0.03", "0.03", "0.03", "0.02", "0.00"}
	cumulative := decimal.Zero
	for i, want := range wantEachStep {
		ret, err := createSaleReturn(ownerCtx, t, h, pool, q, sale.Id, &gen.SaleReturnCreate{
			Items: []gen.SaleReturnItemCreate{{SaleItemId: sale.Items[0].Id, Qty: "1.000"}},
		})
		if err != nil {
			t.Fatalf("step %d: createSaleReturn: %v", i+1, err)
		}
		if ret.Total != want {
			t.Fatalf("step %d: Total = %q, want %q", i+1, ret.Total, want)
		}
		cumulative = cumulative.Add(d(t, ret.Total))
		if cumulative.GreaterThan(d(t, "0.20")) {
			t.Fatalf("step %d: cumulative = %s, must never exceed 0.20", i+1, cumulative)
		}
	}
	if cumulative.StringFixed(2) != "0.20" {
		t.Fatalf("final cumulative = %s, want exactly 0.20", cumulative)
	}
}

// TestCreateSaleReturn_thirdsRefundAcrossThreeSteps is the same
// over-refund cap fixture at a coarser granularity (review ruling): a
// line of qty 3 with net 10.00, returned 1 unit at a time, must refund
// 3.33, 3.33, then 3.34 (the third step's exact remainder), never a
// fourth independently-rounded 3.33 that would overshoot net.
func TestCreateSaleReturn_thirdsRefundAcrossThreeSteps(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "return-thirds")
	owner := seedUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "return-thirds-product", "5.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	ownerCtx := ctxAs(shop.ID, owner)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "5.000")

	// line_total 5.00*3 = 15.00, discount 5.00 (the only line) -> net 10.00.
	sale, err := createSale(ownerCtx, t, h, pool, q, &gen.SaleCreate{
		LocationId: loc.ID,
		Items:      []gen.SaleItemCreate{{VariantId: variant.ID, Qty: "3.000"}},
		Payment:    gen.SalePaymentCreate{Method: gen.Cash},
		Discount:   &gen.SaleDiscount{Type: gen.Fixed, Value: "5.00"},
	})
	if err != nil {
		t.Fatalf("createSale: %v", err)
	}

	wantEachStep := []string{"3.33", "3.33", "3.34"}
	cumulative := decimal.Zero
	for i, want := range wantEachStep {
		ret, err := createSaleReturn(ownerCtx, t, h, pool, q, sale.Id, &gen.SaleReturnCreate{
			Items: []gen.SaleReturnItemCreate{{SaleItemId: sale.Items[0].Id, Qty: "1.000"}},
		})
		if err != nil {
			t.Fatalf("step %d: createSaleReturn: %v", i+1, err)
		}
		if ret.Total != want {
			t.Fatalf("step %d: Total = %q, want %q", i+1, ret.Total, want)
		}
		cumulative = cumulative.Add(d(t, ret.Total))
	}
	if cumulative.StringFixed(2) != "10.00" {
		t.Fatalf("final cumulative = %s, want exactly 10.00", cumulative)
	}
}

// TestCreateSaleReturn_returnExceedsSoldOnOverstep is D-58: a line's
// cumulative returned quantity may never exceed what was sold.
func TestCreateSaleReturn_returnExceedsSoldOnOverstep(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "return-exceeds-sold")
	owner := seedUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "return-exceeds-sold-product", "10.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	ownerCtx := ctxAs(shop.ID, owner)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "5.000")

	sale, err := createSale(ownerCtx, t, h, pool, q, saleBody(loc.ID, variant.ID, "2.000"))
	if err != nil {
		t.Fatalf("createSale: %v", err)
	}

	if _, err := createSaleReturn(ownerCtx, t, h, pool, q, sale.Id, &gen.SaleReturnCreate{
		Items: []gen.SaleReturnItemCreate{{SaleItemId: sale.Items[0].Id, Qty: "1.000"}},
	}); err != nil {
		t.Fatalf("first return (1 of 2): %v", err)
	}

	_, err = createSaleReturn(ownerCtx, t, h, pool, q, sale.Id, &gen.SaleReturnCreate{
		Items: []gen.SaleReturnItemCreate{{SaleItemId: sale.Items[0].Id, Qty: "2.000"}},
	})
	if err == nil {
		t.Fatal("want 409 RETURN_EXCEEDS_SOLD (1 already returned + 2 requested > 2 sold), got no error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 409 || apiErr.Code != gen.RETURNEXCEEDSSOLD {
		t.Fatalf("error = %v, want 409 RETURN_EXCEEDS_SOLD", err)
	}
	if got := apiErr.Details["saleItemId"]; got != sale.Items[0].Id.String() {
		t.Fatalf("details.saleItemId = %v, want %s", got, sale.Items[0].Id)
	}
}

// TestCreateSaleReturn_cashierForbidden proves sales.void (which also
// gates returns, D-58's "manager+") is owner/manager only.
func TestCreateSaleReturn_cashierForbidden(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "return-cashier-forbidden")
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "return-forbidden-product", "10.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	cashierCtx := ctxAs(shop.ID, cashier)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "5.000")

	sale, err := createSale(cashierCtx, t, h, pool, q, saleBody(loc.ID, variant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createSale: %v", err)
	}

	_, err = createSaleReturn(cashierCtx, t, h, pool, q, sale.Id, &gen.SaleReturnCreate{
		Items: []gen.SaleReturnItemCreate{{SaleItemId: sale.Items[0].Id, Qty: "1.000"}},
	})
	assertAPIErr(t, "cashier return", err, 403, gen.FORBIDDEN)
}

// TestCreateSaleReturn_foreignSaleIs404 proves the lock query filters by
// the actor's own shop_id (ADR-004/hard rule 1).
func TestCreateSaleReturn_foreignSaleIs404(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shopA := seedShop(ctx, t, q, "return-foreign-a")
	shopB := seedShop(ctx, t, q, "return-foreign-b")
	ownerA := seedUser(ctx, t, q, shopA.ID, "owner-a", db.UserRoleOwner)
	ownerB := seedUser(ctx, t, q, shopB.ID, "owner-b", db.UserRoleOwner)
	unit := seedUnit(ctx, t, q, shopA.ID, "pcs")
	product := seedProduct(ctx, t, q, shopA.ID, unit.ID, "return-foreign-product", "10.00", productOpts{})
	variant := seedVariant(ctx, t, q, shopA.ID, product.ID)
	loc := seedLocation(ctx, t, q, shopA.ID, "Main")
	stockIn(ctx, t, pool, q, shopA.ID, variant.ID, loc.ID, "5.000")

	sale, err := createSale(ctxAs(shopA.ID, ownerA), t, h, pool, q, saleBody(loc.ID, variant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createSale: %v", err)
	}

	_, err = createSaleReturn(ctxAs(shopB.ID, ownerB), t, h, pool, q, sale.Id, &gen.SaleReturnCreate{
		Items: []gen.SaleReturnItemCreate{{SaleItemId: sale.Items[0].Id, Qty: "1.000"}},
	})
	assertAPIErr(t, "foreign shop return", err, 404, gen.NOTFOUND)
}

// TestCreateSaleReturn_returnOfReturnIsNotReturnable is D-66: a
// return-kind sale cannot itself be the target of a further return —
// answered as 409 SALE_NOT_RETURNABLE (review ruling), not 400
// VALIDATION_FAILED, since {id} is a path parameter naming a real sale,
// not a malformed request field.
func TestCreateSaleReturn_returnOfReturnIsNotReturnable(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "return-of-return")
	owner := seedUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "return-of-return-product", "10.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	ownerCtx := ctxAs(shop.ID, owner)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "5.000")

	sale, err := createSale(ownerCtx, t, h, pool, q, saleBody(loc.ID, variant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createSale: %v", err)
	}
	ret, err := createSaleReturn(ownerCtx, t, h, pool, q, sale.Id, &gen.SaleReturnCreate{
		Items: []gen.SaleReturnItemCreate{{SaleItemId: sale.Items[0].Id, Qty: "1.000"}},
	})
	if err != nil {
		t.Fatalf("createSaleReturn: %v", err)
	}

	_, err = createSaleReturn(ownerCtx, t, h, pool, q, ret.Id, &gen.SaleReturnCreate{
		Items: []gen.SaleReturnItemCreate{{SaleItemId: ret.Items[0].Id, Qty: "1.000"}},
	})
	assertAPIErr(t, "return of a return", err, 409, gen.SALENOTRETURNABLE)
}

// TestCreateSaleReturn_voidedOriginalIsAlreadyVoided proves a return
// against an already-voided sale is rejected the same way a second void
// would be (D-58 requires a completed original).
func TestCreateSaleReturn_voidedOriginalIsAlreadyVoided(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "return-voided-original")
	owner := seedUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "return-voided-original-product", "10.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	ownerCtx := ctxAs(shop.ID, owner)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "5.000")

	sale, err := createSale(ownerCtx, t, h, pool, q, saleBody(loc.ID, variant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createSale: %v", err)
	}
	if _, err := voidSale(ownerCtx, t, h, pool, q, sale.Id, nil); err != nil {
		t.Fatalf("voidSale: %v", err)
	}

	_, err = createSaleReturn(ownerCtx, t, h, pool, q, sale.Id, &gen.SaleReturnCreate{
		Items: []gen.SaleReturnItemCreate{{SaleItemId: sale.Items[0].Id, Qty: "1.000"}},
	})
	assertAPIErr(t, "return against a voided sale", err, 409, gen.SALEALREADYVOIDED)
}

// TestCreateSaleReturn_itemsFieldValidation mirrors
// TestCreateSale_itemsFieldValidation (create_test.go).
func TestCreateSaleReturn_itemsFieldValidation(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "return-items-validation")
	owner := seedUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "return-items-validation-product", "10.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	ownerCtx := ctxAs(shop.ID, owner)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "5.000")

	sale, err := createSale(ownerCtx, t, h, pool, q, saleBody(loc.ID, variant.ID, "3.000"))
	if err != nil {
		t.Fatalf("createSale: %v", err)
	}

	assertValidation := func(t *testing.T, label string, err error, wantField, wantReason string) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: want 400 VALIDATION_FAILED, got no error", label)
		}
		var apiErr *apierr.Error
		if !errors.As(err, &apiErr) || apiErr.Status != 400 {
			t.Fatalf("%s: error = %v, want 400 VALIDATION_FAILED", label, err)
		}
		if got := apiErr.Details["fields"].(map[string]string)[wantField]; got != wantReason {
			t.Fatalf("%s: details.fields.%s = %q, want %q", label, wantField, got, wantReason)
		}
	}

	// Empty items.
	_, err = createSaleReturn(ownerCtx, t, h, pool, q, sale.Id, &gen.SaleReturnCreate{Items: []gen.SaleReturnItemCreate{}})
	assertValidation(t, "empty items", err, "items", "required")

	// Unknown saleItemId.
	_, err = createSaleReturn(ownerCtx, t, h, pool, q, sale.Id, &gen.SaleReturnCreate{
		Items: []gen.SaleReturnItemCreate{{SaleItemId: uuid.New(), Qty: "1.000"}},
	})
	assertValidation(t, "unknown saleItemId", err, "items[0].saleItemId", "invalid")

	// Duplicate saleItemId.
	_, err = createSaleReturn(ownerCtx, t, h, pool, q, sale.Id, &gen.SaleReturnCreate{
		Items: []gen.SaleReturnItemCreate{
			{SaleItemId: sale.Items[0].Id, Qty: "1.000"},
			{SaleItemId: sale.Items[0].Id, Qty: "1.000"},
		},
	})
	assertValidation(t, "duplicate saleItemId", err, "items[1].saleItemId", "invalid")

	// qty zero.
	_, err = createSaleReturn(ownerCtx, t, h, pool, q, sale.Id, &gen.SaleReturnCreate{
		Items: []gen.SaleReturnItemCreate{{SaleItemId: sale.Items[0].Id, Qty: "0.000"}},
	})
	assertValidation(t, "qty zero", err, "items[0].qty", "invalid")

	// qty with four decimal places (numeric(12,3) only holds three).
	_, err = createSaleReturn(ownerCtx, t, h, pool, q, sale.Id, &gen.SaleReturnCreate{
		Items: []gen.SaleReturnItemCreate{{SaleItemId: sale.Items[0].Id, Qty: "1.0001"}},
	})
	assertValidation(t, "qty four decimals", err, "items[0].qty", "invalid")

	// saleItemId belonging to a different sale in the same shop — a real
	// sale_items row, just not one of *this* original sale's own lines.
	otherSale, err := createSale(ownerCtx, t, h, pool, q, saleBody(loc.ID, variant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createSale (other sale): %v", err)
	}
	_, err = createSaleReturn(ownerCtx, t, h, pool, q, sale.Id, &gen.SaleReturnCreate{
		Items: []gen.SaleReturnItemCreate{{SaleItemId: otherSale.Items[0].Id, Qty: "1.000"}},
	})
	assertValidation(t, "saleItemId from a different sale", err, "items[0].saleItemId", "invalid")
}

// returnStep is one goroutine's half of a deterministic two-transaction
// interleaving — mirrors saleStep (create_test.go): it begins its own
// transaction, runs CreateSaleReturnTx (which blocks inside
// GetSaleItemsForUpdate's row lock if some other transaction already
// holds it), and, once CreateSaleReturnTx returns, waits on commit before
// finishing.
type returnStep struct {
	result gen.Sale
	err    error
	locked chan struct{}
	commit chan struct{}
	done   chan error
}

func startCreateSaleReturnHoldingLock(ctx context.Context, pool *pgxpool.Pool, q *db.Queries, h *sales.Handler, originalSaleID uuid.UUID, body *gen.SaleReturnCreate) *returnStep {
	s := &returnStep{locked: make(chan struct{}), commit: make(chan struct{}), done: make(chan error, 1)}
	go func() {
		tx, err := pool.Begin(ctx)
		if err != nil {
			s.err = err
			close(s.locked)
			s.done <- err
			return
		}
		s.result, s.err = h.CreateSaleReturnTx(ctx, q.WithTx(tx), originalSaleID, body)
		close(s.locked)

		<-s.commit
		if s.err != nil {
			s.done <- tx.Rollback(ctx)
			return
		}
		s.done <- tx.Commit(ctx)
	}()
	return s
}

func assertReturnStillBlocked(t *testing.T, label string, s *returnStep, timeout time.Duration) {
	t.Helper()
	select {
	case <-s.locked:
		t.Fatalf("%s: CreateSaleReturnTx returned before the blocking transaction committed, want it still blocked", label)
	case <-time.After(timeout):
	}
}

func waitReturnLocked(t *testing.T, label string, s *returnStep, timeout time.Duration) error {
	t.Helper()
	select {
	case <-s.locked:
		return s.err
	case <-time.After(timeout):
		t.Fatalf("%s: CreateSaleReturnTx did not return within %s", label, timeout)
		return nil
	}
}

func finishReturn(t *testing.T, label string, s *returnStep, timeout time.Duration) error {
	t.Helper()
	close(s.commit)
	select {
	case err := <-s.done:
		return err
	case <-time.After(timeout):
		t.Fatalf("%s: commit/rollback did not complete within %s", label, timeout)
		return nil
	}
}

const (
	returnBlockedWait = 200 * time.Millisecond
	returnResultWait  = 5 * time.Second
)

// TestCreateSaleReturn_concurrentReturnsOfLastUnitExactlyOneSucceeds is
// the correctness-critical race test resolveReturnLines' own doc comment
// promises: two concurrent returns of the same, single remaining unit,
// made deterministic (mirrors
// TestCreateSaleTx_concurrentLastUnitExactlyOneSucceeds) — A locks and
// holds; B is asserted still blocked while A holds; A commits; B then
// unblocks and must fail with RETURN_EXCEEDS_SOLD, having written no
// return_in movement of its own.
func TestCreateSaleReturn_concurrentReturnsOfLastUnitExactlyOneSucceeds(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "return-race")
	owner := seedUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "return-race-product", "10.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	ownerCtx := ctxAs(shop.ID, owner)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "5.000")

	sale, err := createSale(ownerCtx, t, h, pool, q, saleBody(loc.ID, variant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createSale: %v", err)
	}
	body := &gen.SaleReturnCreate{Items: []gen.SaleReturnItemCreate{{SaleItemId: sale.Items[0].Id, Qty: "1.000"}}}

	stepA := startCreateSaleReturnHoldingLock(ownerCtx, pool, q, h, sale.Id, body)
	if err := waitReturnLocked(t, "A", stepA, returnResultWait); err != nil {
		t.Fatalf("A: want CreateSaleReturnTx to succeed (first to the lock), got: %v", err)
	}

	stepB := startCreateSaleReturnHoldingLock(ownerCtx, pool, q, h, sale.Id, body)
	assertReturnStillBlocked(t, "B", stepB, returnBlockedWait)

	if err := finishReturn(t, "A", stepA, returnResultWait); err != nil {
		t.Fatalf("A: commit: %v", err)
	}

	bErr := waitReturnLocked(t, "B", stepB, returnResultWait)
	var apiErr *apierr.Error
	if !errors.As(bErr, &apiErr) || apiErr.Status != 409 || apiErr.Code != gen.RETURNEXCEEDSSOLD {
		t.Fatalf("B: want 409 RETURN_EXCEEDS_SOLD once unblocked, got: %v", bErr)
	}
	if err := finishReturn(t, "B", stepB, returnResultWait); err != nil {
		t.Fatalf("B: rollback: %v", err)
	}

	if got := countMovements(ctx, t, pool, shop.ID, variant.ID, loc.ID, db.StockMovementKindReturnIn); got != 1 {
		t.Fatalf("return_in movements = %d, want 1 (B's must not have written a second one)", got)
	}
	level, _ := readLevel(ctx, t, pool, shop.ID, variant.ID, loc.ID)
	if !level.Equal(d(t, "5.000")) {
		t.Fatalf("level = %s, want 5.000 (fully restored by A's return alone)", level)
	}
}
