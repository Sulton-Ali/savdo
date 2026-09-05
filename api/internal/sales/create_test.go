package sales_test

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/sales"
)

func TestCreateSale_computesTotalsServerSide(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "sale-totals")
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "totals-product", "100.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	cashierCtx := ctxAs(shop.ID, cashier)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "10.000")

	sale, err := createSale(cashierCtx, t, h, pool, q, saleBody(loc.ID, variant.ID, "3.000"))
	if err != nil {
		t.Fatalf("createSale: %v", err)
	}
	if sale.Subtotal != "300.00" {
		t.Fatalf("Subtotal = %q, want 300.00 (3 * 100, computed server-side)", sale.Subtotal)
	}
	if sale.DiscountAmount != "0.00" {
		t.Fatalf("DiscountAmount = %q, want 0.00 (no discount requested)", sale.DiscountAmount)
	}
	if sale.Total != "300.00" {
		t.Fatalf("Total = %q, want 300.00", sale.Total)
	}
	if sale.Payment.Amount != "300.00" || sale.Payment.Method != gen.Cash {
		t.Fatalf("Payment = %+v, want {cash 300.00}", sale.Payment)
	}
	if len(sale.Items) != 1 || sale.Items[0].UnitPrice != "100.00" || sale.Items[0].LineTotal != "300.00" || sale.Items[0].Qty != "3.000" {
		t.Fatalf("Items = %+v, want one line qty=3.000 unitPrice=100.00 lineTotal=300.00", sale.Items)
	}
	// SaleItemCreate carries only variantId/qty (gen.SaleItemCreate has no
	// price field at all) — a client-supplied price is therefore
	// impossible by type, not just rejected at runtime.
}

func TestCreateSale_customerAttachedAndReturnedInResponse(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "sale-customer")
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "customer-product", "10.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	customer := seedCustomer(ctx, t, q, shop.ID, "Jasur Aliyev")
	cashierCtx := ctxAs(shop.ID, cashier)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "5.000")

	body := saleBody(loc.ID, variant.ID, "1.000")
	body.CustomerId = &customer.ID
	sale, err := createSale(cashierCtx, t, h, pool, q, body)
	if err != nil {
		t.Fatalf("createSale: %v", err)
	}
	if !sale.CustomerId.IsSpecified() || sale.CustomerId.IsNull() || sale.CustomerId.MustGet() != customer.ID {
		t.Fatalf("CustomerId = %+v, want %s", sale.CustomerId, customer.ID)
	}
	if !sale.CustomerName.IsSpecified() || sale.CustomerName.IsNull() || sale.CustomerName.MustGet() != "Jasur Aliyev" {
		t.Fatalf("CustomerName = %+v, want Jasur Aliyev", sale.CustomerName)
	}

	// A foreign customerId 404s.
	foreignBody := saleBody(loc.ID, variant.ID, "1.000")
	foreignID := uuid.New()
	foreignBody.CustomerId = &foreignID
	_, err = createSale(cashierCtx, t, h, pool, q, foreignBody)
	if err == nil {
		t.Fatal("want 404 NOT_FOUND for a foreign customerId, got no error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 404 {
		t.Fatalf("error = %v, want 404 NOT_FOUND", err)
	}
}

func TestCreateSale_unitCostFrozenFromVariantCostOverride(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "sale-cost-override")
	owner := seedUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "cost-override-product", "100.00", productOpts{costPrice: strPtr("40.00")})
	variant := seedVariantWithCostOverride(ctx, t, q, shop.ID, product.ID, "65.00")
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	ownerCtx := ctxAs(shop.ID, owner)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "5.000")

	sale, err := createSale(ownerCtx, t, h, pool, q, saleBody(loc.ID, variant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createSale: %v", err)
	}
	if sale.Items[0].UnitCost == nil || *sale.Items[0].UnitCost != "65.00" {
		t.Fatalf("UnitCost = %v, want 65.00 (variant cost_override beats the product's cost_price)", sale.Items[0].UnitCost)
	}
}

func TestCreateSale_promoPriceAppliedWithinRangeNotOutside(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "sale-promo")
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	cashierCtx := ctxAs(shop.ID, cashier)

	shopRow, err := q.GetShop(ctx, shop.ID)
	if err != nil {
		t.Fatalf("GetShop: %v", err)
	}
	tz, err := time.LoadLocation(shopRow.Timezone)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", shopRow.Timezone, err)
	}
	now := time.Now().In(tz)
	todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, tz)

	// Case A: promo_from/promo_to both stored as today's midnight — a
	// single-day promo covering "today" — must be active right now no
	// matter what time of day "now" actually is (promo_to being midnight
	// at the *start* of its last day, not its end, is exactly the trap
	// this asserts against: a naive `now <= promo_to` instant comparison
	// would end the promo before that day even begins).
	promoProduct := seedProduct(ctx, t, q, shop.ID, unit.ID, "promo-active", "100.00", productOpts{
		promoPrice: strPtr("60.00"), promoFrom: &todayMidnight, promoTo: &todayMidnight,
	})
	promoVariant := seedVariant(ctx, t, q, shop.ID, promoProduct.ID)
	stockIn(ctx, t, pool, q, shop.ID, promoVariant.ID, loc.ID, "5.000")

	saleA, err := createSale(cashierCtx, t, h, pool, q, saleBody(loc.ID, promoVariant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createSale (promo active today): %v", err)
	}
	if saleA.Items[0].UnitPrice != "60.00" {
		t.Fatalf("UnitPrice = %q, want 60.00 (promo active today)", saleA.Items[0].UnitPrice)
	}

	// Case B: promo_from/promo_to both yesterday — not active today.
	yesterday := todayMidnight.AddDate(0, 0, -1)
	pastProduct := seedProduct(ctx, t, q, shop.ID, unit.ID, "promo-past", "100.00", productOpts{
		promoPrice: strPtr("60.00"), promoFrom: &yesterday, promoTo: &yesterday,
	})
	pastVariant := seedVariant(ctx, t, q, shop.ID, pastProduct.ID)
	stockIn(ctx, t, pool, q, shop.ID, pastVariant.ID, loc.ID, "5.000")

	saleB, err := createSale(cashierCtx, t, h, pool, q, saleBody(loc.ID, pastVariant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createSale (promo expired): %v", err)
	}
	if saleB.Items[0].UnitPrice != "100.00" {
		t.Fatalf("UnitPrice = %q, want 100.00 (promo expired yesterday, base price applies)", saleB.Items[0].UnitPrice)
	}
}

func strPtr(s string) *string { return &s }

func TestCreateSale_percentAndFixedDiscount(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "sale-discount")
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	percentProduct := seedProduct(ctx, t, q, shop.ID, unit.ID, "discount-product-percent", "100.00", productOpts{})
	fixedProduct := seedProduct(ctx, t, q, shop.ID, unit.ID, "discount-product-fixed", "100.00", productOpts{})
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	cashierCtx := ctxAs(shop.ID, cashier)

	// Percent: 10% of subtotal 300.00 = 30.00, total = 270.00.
	percentVariant := seedVariant(ctx, t, q, shop.ID, percentProduct.ID)
	stockIn(ctx, t, pool, q, shop.ID, percentVariant.ID, loc.ID, "10.000")
	percentBody := saleBody(loc.ID, percentVariant.ID, "3.000")
	percentBody.Discount = &gen.SaleDiscount{Type: gen.Percent, Value: "10"}
	saleP, err := createSale(cashierCtx, t, h, pool, q, percentBody)
	if err != nil {
		t.Fatalf("createSale (percent discount): %v", err)
	}
	if saleP.DiscountAmount != "30.00" {
		t.Fatalf("DiscountAmount = %q, want 30.00 (10%% of 300.00)", saleP.DiscountAmount)
	}
	if saleP.Total != "270.00" {
		t.Fatalf("Total = %q, want 270.00", saleP.Total)
	}

	// Fixed: 50.00 off a subtotal of 300.00, total = 250.00.
	fixedVariant := seedVariant(ctx, t, q, shop.ID, fixedProduct.ID)
	stockIn(ctx, t, pool, q, shop.ID, fixedVariant.ID, loc.ID, "10.000")
	fixedBody := saleBody(loc.ID, fixedVariant.ID, "3.000")
	fixedBody.Discount = &gen.SaleDiscount{Type: gen.Fixed, Value: "50.00"}
	saleF, err := createSale(cashierCtx, t, h, pool, q, fixedBody)
	if err != nil {
		t.Fatalf("createSale (fixed discount): %v", err)
	}
	if saleF.DiscountAmount != "50.00" {
		t.Fatalf("DiscountAmount = %q, want 50.00", saleF.DiscountAmount)
	}
	if saleF.Total != "250.00" {
		t.Fatalf("Total = %q, want 250.00", saleF.Total)
	}
}

func TestCreateSale_discountExceedsSubtotal409(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "sale-discount-exceeds")
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "exceeds-product", "100.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	cashierCtx := ctxAs(shop.ID, cashier)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "10.000")

	body := saleBody(loc.ID, variant.ID, "1.000") // subtotal 100.00
	body.Discount = &gen.SaleDiscount{Type: gen.Fixed, Value: "150.00"}

	_, err := createSale(cashierCtx, t, h, pool, q, body)
	if err == nil {
		t.Fatal("want 409 DISCOUNT_EXCEEDS_SUBTOTAL, got no error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 409 || apiErr.Code != gen.DISCOUNTEXCEEDSSUBTOTAL {
		t.Fatalf("error = %v, want 409 DISCOUNT_EXCEEDS_SUBTOTAL", err)
	}
	if got := countSales(ctx, t, pool, shop.ID); got != 0 {
		t.Fatalf("sales rows = %d, want 0 (the whole transaction must have rolled back)", got)
	}
}

func TestCreateSale_stockInsufficientLeavesNoSaleNoMovementAndReusesNumber(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "sale-insufficient")
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "insufficient-product", "100.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	cashierCtx := ctxAs(shop.ID, cashier)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "2.000")

	_, err := createSale(cashierCtx, t, h, pool, q, saleBody(loc.ID, variant.ID, "5.000"))
	if err == nil {
		t.Fatal("want 409 STOCK_INSUFFICIENT, got no error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 409 || apiErr.Code != gen.STOCKINSUFFICIENT {
		t.Fatalf("error = %v, want 409 STOCK_INSUFFICIENT", err)
	}
	if got := countSales(ctx, t, pool, shop.ID); got != 0 {
		t.Fatalf("sales rows = %d, want 0", got)
	}
	// Only the seed purchase_in — no sale_out was written.
	if got := countMovements(ctx, t, pool, shop.ID, variant.ID, loc.ID, db.StockMovementKindSaleOut); got != 0 {
		t.Fatalf("sale_out movements = %d, want 0", got)
	}
	level, exists := readLevel(ctx, t, pool, shop.ID, variant.ID, loc.ID)
	if !exists || !level.Equal(d(t, "2.000")) {
		t.Fatalf("level = (%s, exists=%v), want (2.000, true), unchanged", level, exists)
	}

	// The failed attempt must not have consumed a sale number: the next
	// successful sale still gets number 1.
	success, err := createSale(cashierCtx, t, h, pool, q, saleBody(loc.ID, variant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createSale after failed attempt: %v", err)
	}
	if success.Number != 1 {
		t.Fatalf("Number = %d, want 1 (the failed attempt's NextSaleNumber must have rolled back)", success.Number)
	}
}

func TestCreateSale_foreignVariantAndLocation404(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shopA := seedShop(ctx, t, q, "sale-foreign-a")
	shopB := seedShop(ctx, t, q, "sale-foreign-b")
	cashierA := seedUser(ctx, t, q, shopA.ID, "cashier-a", db.UserRoleCashier)
	unitA := seedUnit(ctx, t, q, shopA.ID, "pcs")
	unitB := seedUnit(ctx, t, q, shopB.ID, "pcs")
	productA := seedProduct(ctx, t, q, shopA.ID, unitA.ID, "foreign-a", "10.00", productOpts{})
	productB := seedProduct(ctx, t, q, shopB.ID, unitB.ID, "foreign-b", "10.00", productOpts{})
	variantA := seedVariant(ctx, t, q, shopA.ID, productA.ID)
	variantB := seedVariant(ctx, t, q, shopB.ID, productB.ID)
	locA := seedLocation(ctx, t, q, shopA.ID, "Main A")
	locB := seedLocation(ctx, t, q, shopB.ID, "Main B")
	cashierACtx := ctxAs(shopA.ID, cashierA)
	stockIn(ctx, t, pool, q, shopA.ID, variantA.ID, locA.ID, "5.000")
	stockIn(ctx, t, pool, q, shopB.ID, variantB.ID, locB.ID, "5.000")

	assert404 := func(t *testing.T, label string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: want 404 NOT_FOUND, got no error", label)
		}
		var apiErr *apierr.Error
		if !errors.As(err, &apiErr) || apiErr.Status != 404 {
			t.Fatalf("%s: error = %v, want 404 NOT_FOUND", label, err)
		}
	}

	// shop B's variant sold in shop A's context.
	_, err := createSale(cashierACtx, t, h, pool, q, saleBody(locA.ID, variantB.ID, "1.000"))
	assert404(t, "foreign variantId", err)

	// shop B's location used in shop A's context.
	_, err = createSale(cashierACtx, t, h, pool, q, saleBody(locB.ID, variantA.ID, "1.000"))
	assert404(t, "foreign locationId", err)
}

func TestCreateSale_inactiveProductAndInactiveVariant404(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "sale-inactive")
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	cashierCtx := ctxAs(shop.ID, cashier)

	assert404 := func(t *testing.T, label string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: want 404 NOT_FOUND, got no error", label)
		}
		var apiErr *apierr.Error
		if !errors.As(err, &apiErr) || apiErr.Status != 404 {
			t.Fatalf("%s: error = %v, want 404 NOT_FOUND", label, err)
		}
	}

	// Inactive product, active variant.
	inactiveFalse := false
	inactiveProduct := seedProduct(ctx, t, q, shop.ID, unit.ID, "inactive-product", "10.00", productOpts{isActive: &inactiveFalse})
	variantOnInactiveProduct := seedVariant(ctx, t, q, shop.ID, inactiveProduct.ID)
	stockIn(ctx, t, pool, q, shop.ID, variantOnInactiveProduct.ID, loc.ID, "5.000")
	_, err := createSale(cashierCtx, t, h, pool, q, saleBody(loc.ID, variantOnInactiveProduct.ID, "1.000"))
	assert404(t, "inactive product", err)

	// Active product, inactive variant.
	activeProduct := seedProduct(ctx, t, q, shop.ID, unit.ID, "active-product-inactive-variant", "10.00", productOpts{})
	inactiveVariant := seedInactiveVariant(ctx, t, q, shop.ID, activeProduct.ID)
	stockIn(ctx, t, pool, q, shop.ID, inactiveVariant.ID, loc.ID, "5.000")
	_, err = createSale(cashierCtx, t, h, pool, q, saleBody(loc.ID, inactiveVariant.ID, "1.000"))
	assert404(t, "inactive variant", err)
}

// TestCreateSale_stockInsufficientOnLineTwoLeavesNoMovementForLineOne is
// MINOR 7's own test: a three-line sale whose second line (by sorted
// variant_id order, not request order) cannot be satisfied must roll the
// whole transaction back — including the first line's otherwise-valid
// movement, since all three lines share one sale (all-or-nothing, the
// same invariant TestReceiveAndCancelPurchase_multiItem already proves
// for purchases). variant ids are minted and sorted explicitly (not left
// to seedVariant's own uuid.New(), a random v4 with no relationship to
// creation order) and the shortage is assigned to the middle one, so it
// deterministically sorts second — guaranteeing the first-sorted variant's
// Move has already succeeded by the time the shortage is hit, on every
// run, not "about two runs in three".
func TestCreateSale_stockInsufficientOnLineTwoLeavesNoMovementForLineOne(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "sale-insufficient-line-two")
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	productA := seedProduct(ctx, t, q, shop.ID, unit.ID, "insufficient-line-a", "10.00", productOpts{})
	productB := seedProduct(ctx, t, q, shop.ID, unit.ID, "insufficient-line-b", "10.00", productOpts{})
	productC := seedProduct(ctx, t, q, shop.ID, unit.ID, "insufficient-line-c", "10.00", productOpts{})

	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	// ids[0] sorts first (ample stock), ids[1] sorts second — the middle,
	// short one — ids[2] sorts last (ample stock, never reached).
	variantA := seedVariantWithID(ctx, t, q, shop.ID, productA.ID, ids[0])
	variantB := seedVariantWithID(ctx, t, q, shop.ID, productB.ID, ids[1])
	variantC := seedVariantWithID(ctx, t, q, shop.ID, productC.ID, ids[2])
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	cashierCtx := ctxAs(shop.ID, cashier)

	// Plenty of stock for A and C; only enough for B to fail once the
	// sale asks for more than it has.
	stockIn(ctx, t, pool, q, shop.ID, variantA.ID, loc.ID, "10.000")
	stockIn(ctx, t, pool, q, shop.ID, variantB.ID, loc.ID, "1.000")
	stockIn(ctx, t, pool, q, shop.ID, variantC.ID, loc.ID, "10.000")

	body := &gen.SaleCreate{
		LocationId: loc.ID,
		Items: []gen.SaleItemCreate{
			// Request order deliberately does not match sort order —
			// sortedSaleLines (create.go), not the client's own item
			// order, decides which line Move sees first.
			{VariantId: variantC.ID, Qty: "1.000"},
			{VariantId: variantA.ID, Qty: "1.000"},
			{VariantId: variantB.ID, Qty: "5.000"}, // only 1.000 in stock
		},
		Payment: gen.SalePaymentCreate{Method: gen.Cash},
	}
	_, err := createSale(cashierCtx, t, h, pool, q, body)
	if err == nil {
		t.Fatal("want 409 STOCK_INSUFFICIENT, got no error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 409 || apiErr.Code != gen.STOCKINSUFFICIENT {
		t.Fatalf("error = %v, want 409 STOCK_INSUFFICIENT", err)
	}

	if got := countSales(ctx, t, pool, shop.ID); got != 0 {
		t.Fatalf("sales rows = %d, want 0", got)
	}
	for _, v := range []struct {
		name string
		id   uuid.UUID
		want string
	}{
		{"A (sorts first, its Move must have already succeeded and then rolled back)", variantA.ID, "10.000"},
		{"B (the shortage, sorts second)", variantB.ID, "1.000"},
		{"C (sorts last, never reached)", variantC.ID, "10.000"},
	} {
		if got := countMovements(ctx, t, pool, shop.ID, v.id, loc.ID, db.StockMovementKindSaleOut); got != 0 {
			t.Fatalf("variant %s sale_out movements = %d, want 0 (whole sale rolled back)", v.name, got)
		}
		level, exists := readLevel(ctx, t, pool, shop.ID, v.id, loc.ID)
		if !exists || !level.Equal(d(t, v.want)) {
			t.Fatalf("variant %s level = (%s, exists=%v), want (%s, true), unchanged", v.name, level, exists, v.want)
		}
	}
}

func TestCreateSale_twoLineDifferentProductsWritesTwoMovementsAndLevelsDrop(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "sale-two-lines")
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	productA := seedProduct(ctx, t, q, shop.ID, unit.ID, "two-line-a", "50.00", productOpts{})
	productB := seedProduct(ctx, t, q, shop.ID, unit.ID, "two-line-b", "70.00", productOpts{})
	variantA := seedVariant(ctx, t, q, shop.ID, productA.ID)
	variantB := seedVariant(ctx, t, q, shop.ID, productB.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	cashierCtx := ctxAs(shop.ID, cashier)
	stockIn(ctx, t, pool, q, shop.ID, variantA.ID, loc.ID, "10.000")
	stockIn(ctx, t, pool, q, shop.ID, variantB.ID, loc.ID, "10.000")

	body := &gen.SaleCreate{
		LocationId: loc.ID,
		Items: []gen.SaleItemCreate{
			{VariantId: variantA.ID, Qty: "2.000"},
			{VariantId: variantB.ID, Qty: "3.000"},
		},
		Payment: gen.SalePaymentCreate{Method: gen.Cash},
	}
	sale, err := createSale(cashierCtx, t, h, pool, q, body)
	if err != nil {
		t.Fatalf("createSale: %v", err)
	}
	if sale.Subtotal != "310.00" { // 2*50 + 3*70
		t.Fatalf("Subtotal = %q, want 310.00", sale.Subtotal)
	}
	if len(sale.Items) != 2 {
		t.Fatalf("Items = %+v, want 2 lines", sale.Items)
	}

	if got := countMovements(ctx, t, pool, shop.ID, variantA.ID, loc.ID, db.StockMovementKindSaleOut); got != 1 {
		t.Fatalf("variant A sale_out movements = %d, want 1", got)
	}
	if got := countMovements(ctx, t, pool, shop.ID, variantB.ID, loc.ID, db.StockMovementKindSaleOut); got != 1 {
		t.Fatalf("variant B sale_out movements = %d, want 1", got)
	}
	levelA, existsA := readLevel(ctx, t, pool, shop.ID, variantA.ID, loc.ID)
	if !existsA || !levelA.Equal(d(t, "8.000")) {
		t.Fatalf("variant A level = (%s, exists=%v), want (8.000, true)", levelA, existsA)
	}
	levelB, existsB := readLevel(ctx, t, pool, shop.ID, variantB.ID, loc.ID)
	if !existsB || !levelB.Equal(d(t, "7.000")) {
		t.Fatalf("variant B level = (%s, exists=%v), want (7.000, true)", levelB, existsB)
	}
}

func TestCreateSale_duplicateVariantIsRejected(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "sale-duplicate")
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "duplicate-product", "100.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	cashierCtx := ctxAs(shop.ID, cashier)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "10.000")

	body := &gen.SaleCreate{
		LocationId: loc.ID,
		Items: []gen.SaleItemCreate{
			{VariantId: variant.ID, Qty: "1.000"},
			{VariantId: variant.ID, Qty: "2.000"},
		},
		Payment: gen.SalePaymentCreate{Method: gen.Cash},
	}
	_, err := createSale(cashierCtx, t, h, pool, q, body)
	if err == nil {
		t.Fatal("want 400 VALIDATION_FAILED for a duplicate variantId, got no error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 400 {
		t.Fatalf("error = %v, want 400 VALIDATION_FAILED", err)
	}
	if got := apiErr.Details["fields"].(map[string]string)["items"]; got != "invalid" {
		t.Fatalf("details.fields.items = %q, want invalid", got)
	}
}

func TestCreateSale_itemsFieldValidation(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "sale-items-validation")
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "items-validation-product", "10.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	cashierCtx := ctxAs(shop.ID, cashier)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "1000.000")

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
	_, err := createSale(cashierCtx, t, h, pool, q, &gen.SaleCreate{
		LocationId: loc.ID, Items: []gen.SaleItemCreate{}, Payment: gen.SalePaymentCreate{Method: gen.Cash},
	})
	assertValidation(t, "empty items", err, "items", "required")

	// More than 100 items (contracts/openapi.yaml SaleCreate.items maxItems).
	items := make([]gen.SaleItemCreate, 101)
	for i := range items {
		items[i] = gen.SaleItemCreate{VariantId: variant.ID, Qty: "1.000"}
	}
	_, err = createSale(cashierCtx, t, h, pool, q, &gen.SaleCreate{
		LocationId: loc.ID, Items: items[:1], Payment: gen.SalePaymentCreate{Method: gen.Cash},
	})
	if err != nil {
		t.Fatalf("sanity single-item create must succeed to isolate the too_long case: %v", err)
	}
	_, err = createSale(cashierCtx, t, h, pool, q, &gen.SaleCreate{
		LocationId: loc.ID, Items: items, Payment: gen.SalePaymentCreate{Method: gen.Cash},
	})
	assertValidation(t, "too many items", err, "items", "too_long")

	// qty zero/negative-shaped string.
	_, err = createSale(cashierCtx, t, h, pool, q, &gen.SaleCreate{
		LocationId: loc.ID, Items: []gen.SaleItemCreate{{VariantId: variant.ID, Qty: "0.000"}}, Payment: gen.SalePaymentCreate{Method: gen.Cash},
	})
	assertValidation(t, "qty zero", err, "items[0].qty", "invalid")

	// Unknown payment method.
	_, err = createSale(cashierCtx, t, h, pool, q, &gen.SaleCreate{
		LocationId: loc.ID, Items: []gen.SaleItemCreate{{VariantId: variant.ID, Qty: "1.000"}}, Payment: gen.SalePaymentCreate{Method: "bogus"},
	})
	assertValidation(t, "unknown payment method", err, "payment.method", "invalid")
}

func TestCreateSale_cashierResponseHasNoUnitCostManagerResponseHasIt(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "sale-cost-visibility")
	owner := seedUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "cost-visibility-product", "100.00", productOpts{costPrice: strPtr("40.00")})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	cashierCtx := ctxAs(shop.ID, cashier)
	ownerCtx := ctxAs(shop.ID, owner)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "10.000")

	// Created by the cashier — the response is still built with the
	// creating actor's own permissions (D-63): a cashier's own create
	// response has no unitCost.
	sale, err := createSale(cashierCtx, t, h, pool, q, saleBody(loc.ID, variant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createSale: %v", err)
	}
	if sale.Items[0].UnitCost != nil {
		t.Fatalf("cashier create response UnitCost = %v, want nil (absent)", sale.Items[0].UnitCost)
	}

	// The owner reading the same sale afterwards sees unitCost.
	resp, err := h.GetSale(ownerCtx, gen.GetSaleRequestObject{Id: sale.Id})
	if err != nil {
		t.Fatalf("GetSale (owner): %v", err)
	}
	got, ok := resp.(gen.GetSale200JSONResponse)
	if !ok {
		t.Fatalf("GetSale response type = %T", resp)
	}
	if got.Items[0].UnitCost == nil || *got.Items[0].UnitCost != "40.00" {
		t.Fatalf("owner GetSale UnitCost = %v, want 40.00", got.Items[0].UnitCost)
	}

	// The cashier reading it back also sees no unitCost.
	respCashier, err := h.GetSale(cashierCtx, gen.GetSaleRequestObject{Id: sale.Id})
	if err != nil {
		t.Fatalf("GetSale (cashier): %v", err)
	}
	gotCashier, ok := respCashier.(gen.GetSale200JSONResponse)
	if !ok {
		t.Fatalf("GetSale response type = %T", respCashier)
	}
	if gotCashier.Items[0].UnitCost != nil {
		t.Fatalf("cashier GetSale UnitCost = %v, want nil (absent)", gotCashier.Items[0].UnitCost)
	}
}

// Every role in auth.rolePermissions (owner, manager, cashier) has
// sales.create — there is no role that lacks it to assert a 403 against
// (docs/04-DATA-MODEL.md § 7's permission matrix has no fourth staff
// role), so that half of the acceptance check is not applicable and is
// skipped here rather than faked.
func TestCreateSale_everyStaffRoleHasSalesCreate(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "sale-roles")
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "roles-product", "10.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "30.000")

	for _, role := range []db.UserRole{db.UserRoleOwner, db.UserRoleManager, db.UserRoleCashier} {
		user := seedUser(ctx, t, q, shop.ID, "user-"+string(role), role)
		if _, err := createSale(ctxAs(shop.ID, user), t, h, pool, q, saleBody(loc.ID, variant.ID, "1.000")); err != nil {
			t.Fatalf("createSale as %s: %v", role, err)
		}
	}
}

func TestCreateSale_sequentialNumbersAcrossSales(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "sale-sequential")
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "sequential-product", "10.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	cashierCtx := ctxAs(shop.ID, cashier)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "10.000")

	for want := 1; want <= 3; want++ {
		sale, err := createSale(cashierCtx, t, h, pool, q, saleBody(loc.ID, variant.ID, "1.000"))
		if err != nil {
			t.Fatalf("createSale #%d: %v", want, err)
		}
		if sale.Number != want {
			t.Fatalf("Number = %d, want %d", sale.Number, want)
		}
	}
}

// saleStep is one goroutine's half of a deterministic two-transaction
// interleaving, mirroring stock's own moveStep (internal/stock/move_test.go):
// it begins its own transaction, runs CreateSaleTx — which blocks inside
// Move's GetLevelForUpdate (and, before that, NextSaleNumber's row lock on
// the shops row) if some other transaction already holds a row it needs —
// and, once CreateSaleTx returns, waits on commit before finishing. locked
// closes the instant CreateSaleTx returns, while the transaction (and
// whatever locks it took) is still open — the moment a second, concurrent
// call for the same last unit is genuinely blocked, not just "hasn't run
// yet". Never calls t.Fatalf itself (only the test's own goroutine may;
// see createSaleRaw's own doc comment in sales_test.go) — every outcome is
// reported on a channel instead.
type saleStep struct {
	result gen.Sale
	err    error
	locked chan struct{}
	commit chan struct{}
	done   chan error
}

// startCreateSaleHoldingLock begins a transaction, runs CreateSaleTx
// inside it, and blocks (holding whatever row locks it took) until the
// test sends on commit.
func startCreateSaleHoldingLock(ctx context.Context, pool *pgxpool.Pool, q *db.Queries, h *sales.Handler, body *gen.SaleCreate) *saleStep {
	s := &saleStep{locked: make(chan struct{}), commit: make(chan struct{}), done: make(chan error, 1)}
	go func() {
		tx, err := pool.Begin(ctx)
		if err != nil {
			s.err = err
			close(s.locked)
			s.done <- err
			return
		}
		s.result, s.err = h.CreateSaleTx(ctx, q.WithTx(tx), body)
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

// assertSaleStillBlocked asserts s.locked has not closed within timeout —
// mirrors stock.assertStillBlocked.
func assertSaleStillBlocked(t *testing.T, label string, s *saleStep, timeout time.Duration) {
	t.Helper()
	select {
	case <-s.locked:
		t.Fatalf("%s: CreateSaleTx returned before the blocking transaction committed, want it still blocked", label)
	case <-time.After(timeout):
	}
}

// waitSaleLocked waits for s.CreateSaleTx to have returned, then returns
// its error — mirrors stock.waitLocked.
func waitSaleLocked(t *testing.T, label string, s *saleStep, timeout time.Duration) error {
	t.Helper()
	select {
	case <-s.locked:
		return s.err
	case <-time.After(timeout):
		t.Fatalf("%s: CreateSaleTx did not return within %s", label, timeout)
		return nil
	}
}

// finishSale tells s to commit (or roll back, if CreateSaleTx itself
// failed) and waits for that to complete — mirrors stock.finish.
func finishSale(t *testing.T, label string, s *saleStep, timeout time.Duration) error {
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
	saleBlockedWait = 200 * time.Millisecond
	saleResultWait  = 5 * time.Second
)

// TestCreateSaleTx_concurrentLastUnitExactlyOneSucceeds is the
// correctness-critical race test (D-41): two concurrent sales for the
// last unit of the same variant/location, made deterministic (mirrors
// stock.TestMove_concurrentLastUnit_exactlyOneSucceeds) rather than a bare
// `go func` race — A locks and holds; B is asserted still blocked while A
// holds; A commits; B then unblocks and must fail with STOCK_INSUFFICIENT,
// having written no sale_out movement of its own.
func TestCreateSaleTx_concurrentLastUnitExactlyOneSucceeds(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "sale-race")
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "race-product", "10.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	cashierCtx := ctxAs(shop.ID, cashier)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "1.000")

	body := saleBody(loc.ID, variant.ID, "1.000")

	stepA := startCreateSaleHoldingLock(cashierCtx, pool, q, h, body)
	if err := waitSaleLocked(t, "A", stepA, saleResultWait); err != nil {
		t.Fatalf("A: want CreateSaleTx to succeed (first to the lock), got: %v", err)
	}

	stepB := startCreateSaleHoldingLock(cashierCtx, pool, q, h, body)
	assertSaleStillBlocked(t, "B", stepB, saleBlockedWait)

	if err := finishSale(t, "A", stepA, saleResultWait); err != nil {
		t.Fatalf("A: commit: %v", err)
	}

	bErr := waitSaleLocked(t, "B", stepB, saleResultWait)
	var apiErr *apierr.Error
	if !errors.As(bErr, &apiErr) || apiErr.Status != 409 || apiErr.Code != gen.STOCKINSUFFICIENT {
		t.Fatalf("B: want 409 STOCK_INSUFFICIENT once unblocked, got: %v", bErr)
	}
	if err := finishSale(t, "B", stepB, saleResultWait); err != nil {
		t.Fatalf("B: rollback: %v", err)
	}

	if count := countMovements(ctx, t, pool, shop.ID, variant.ID, loc.ID, db.StockMovementKindSaleOut); count != 1 {
		t.Fatalf("sale_out movements = %d, want 1 (B's must not have written a second one)", count)
	}
	level, exists := readLevel(ctx, t, pool, shop.ID, variant.ID, loc.ID)
	if !exists || !level.Equal(d(t, "0.000")) {
		t.Fatalf("level = (%s, exists=%v), want (0.000, true)", level, exists)
	}
	if got := countSales(ctx, t, pool, shop.ID); got != 1 {
		t.Fatalf("sales rows = %d, want 1 (only A's)", got)
	}
}
