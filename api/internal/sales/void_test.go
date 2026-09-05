package sales_test

// Integration tests for VoidSaleTx (docs/04-DATA-MODEL.md § 4 Rules
// D-59/D-62/D-66) — mirrors create_test.go's own shape: newTestQueries
// per test, real Postgres via testcontainers, calling the handler method
// directly on a transaction this file opens and commits/rolls back
// itself (voidSale below), the same shape httpx.VoidSale gives
// VoidSaleTx in production (httpx/sales.go's own doc comment).

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/sales"
)

// voidSale runs VoidSaleTx on its own transaction, committing on success
// and rolling back on error — mirrors createSale (sales_test.go).
func voidSale(ctx context.Context, t *testing.T, h *sales.Handler, pool *pgxpool.Pool, q *db.Queries, id uuid.UUID, body *gen.SaleVoid) (gen.Sale, error) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("voidSale: begin: %v", err)
	}
	qtx := q.WithTx(tx)
	resp, err := h.VoidSaleTx(ctx, qtx, id, body)
	if err != nil {
		_ = tx.Rollback(ctx)
		return gen.Sale{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("voidSale: commit: %v", err)
	}
	return resp, nil
}

func assertAPIErr(t *testing.T, label string, err error, wantStatus int, wantCode gen.ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: want %d %s, got no error", label, wantStatus, wantCode)
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != wantStatus || apiErr.Code != wantCode {
		t.Fatalf("%s: error = %v, want %d %s", label, err, wantStatus, wantCode)
	}
}

// TestVoidSaleTx_restoresStockAndWritesOneMovementPerLine is the phase
// bar's own end-to-end sequence: sell two items with a 10% discount, void
// the sale, stock is restored exactly and one sale_void_in movement is
// written per line.
func TestVoidSaleTx_restoresStockAndWritesOneMovementPerLine(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "void-restores-stock")
	owner := seedUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	productA := seedProduct(ctx, t, q, shop.ID, unit.ID, "void-product-a", "100.00", productOpts{})
	productB := seedProduct(ctx, t, q, shop.ID, unit.ID, "void-product-b", "50.00", productOpts{})
	variantA := seedVariant(ctx, t, q, shop.ID, productA.ID)
	variantB := seedVariant(ctx, t, q, shop.ID, productB.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	ownerCtx := ctxAs(shop.ID, owner)
	stockIn(ctx, t, pool, q, shop.ID, variantA.ID, loc.ID, "5.000")
	stockIn(ctx, t, pool, q, shop.ID, variantB.ID, loc.ID, "5.000")

	body := &gen.SaleCreate{
		LocationId: loc.ID,
		Items: []gen.SaleItemCreate{
			{VariantId: variantA.ID, Qty: "2.000"},
			{VariantId: variantB.ID, Qty: "2.000"},
		},
		Payment:  gen.SalePaymentCreate{Method: gen.Cash},
		Discount: &gen.SaleDiscount{Type: gen.Percent, Value: "10"},
	}
	sale, err := createSale(ownerCtx, t, h, pool, q, body)
	if err != nil {
		t.Fatalf("createSale: %v", err)
	}
	if sale.DiscountAmount != "30.00" || sale.Total != "270.00" {
		t.Fatalf("DiscountAmount/Total = %s/%s, want 30.00/270.00", sale.DiscountAmount, sale.Total)
	}

	levelA, _ := readLevel(ctx, t, pool, shop.ID, variantA.ID, loc.ID)
	levelB, _ := readLevel(ctx, t, pool, shop.ID, variantB.ID, loc.ID)
	if !levelA.Equal(d(t, "3.000")) || !levelB.Equal(d(t, "3.000")) {
		t.Fatalf("post-sale levels = %s/%s, want 3.000/3.000", levelA, levelB)
	}

	voided, err := voidSale(ownerCtx, t, h, pool, q, sale.Id, &gen.SaleVoid{Reason: strPtr("wrong item")})
	if err != nil {
		t.Fatalf("voidSale: %v", err)
	}
	if voided.Status != gen.SaleStatus(db.SaleStatusVoided) {
		t.Fatalf("Status = %q, want voided", voided.Status)
	}
	if voided.VoidedAt.IsNull() || voided.VoidedBy.IsNull() {
		t.Fatalf("VoidedAt/VoidedBy not set: %+v", voided)
	}

	levelA, _ = readLevel(ctx, t, pool, shop.ID, variantA.ID, loc.ID)
	levelB, _ = readLevel(ctx, t, pool, shop.ID, variantB.ID, loc.ID)
	if !levelA.Equal(d(t, "5.000")) || !levelB.Equal(d(t, "5.000")) {
		t.Fatalf("post-void levels = %s/%s, want 5.000/5.000 (fully restored)", levelA, levelB)
	}
	if got := countMovements(ctx, t, pool, shop.ID, variantA.ID, loc.ID, db.StockMovementKindSaleVoidIn); got != 1 {
		t.Fatalf("sale_void_in movements (A) = %d, want 1", got)
	}
	if got := countMovements(ctx, t, pool, shop.ID, variantB.ID, loc.ID, db.StockMovementKindSaleVoidIn); got != 1 {
		t.Fatalf("sale_void_in movements (B) = %d, want 1", got)
	}
}

// TestVoidSaleTx_cashierForbidden proves sales.void is owner/manager only
// (docs/04-DATA-MODEL.md § 7): a cashier who can create a sale cannot
// void it.
func TestVoidSaleTx_cashierForbidden(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "void-cashier-forbidden")
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "void-forbidden-product", "10.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	cashierCtx := ctxAs(shop.ID, cashier)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "5.000")

	sale, err := createSale(cashierCtx, t, h, pool, q, saleBody(loc.ID, variant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createSale: %v", err)
	}

	_, err = voidSale(cashierCtx, t, h, pool, q, sale.Id, nil)
	assertAPIErr(t, "cashier void", err, 403, gen.FORBIDDEN)
}

// TestVoidSaleTx_foreignSaleIs404 proves the lock query filters by the
// actor's own shop_id (ADR-004/hard rule 1), not the path id alone.
func TestVoidSaleTx_foreignSaleIs404(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shopA := seedShop(ctx, t, q, "void-foreign-a")
	shopB := seedShop(ctx, t, q, "void-foreign-b")
	ownerA := seedUser(ctx, t, q, shopA.ID, "owner-a", db.UserRoleOwner)
	ownerB := seedUser(ctx, t, q, shopB.ID, "owner-b", db.UserRoleOwner)
	unit := seedUnit(ctx, t, q, shopA.ID, "pcs")
	product := seedProduct(ctx, t, q, shopA.ID, unit.ID, "void-foreign-product", "10.00", productOpts{})
	variant := seedVariant(ctx, t, q, shopA.ID, product.ID)
	loc := seedLocation(ctx, t, q, shopA.ID, "Main")
	stockIn(ctx, t, pool, q, shopA.ID, variant.ID, loc.ID, "5.000")

	sale, err := createSale(ctxAs(shopA.ID, ownerA), t, h, pool, q, saleBody(loc.ID, variant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createSale: %v", err)
	}

	_, err = voidSale(ctxAs(shopB.ID, ownerB), t, h, pool, q, sale.Id, nil)
	assertAPIErr(t, "foreign shop void", err, 404, gen.NOTFOUND)
}

// TestVoidSaleTx_windowClosedYesterday is D-59: a sale completed on an
// earlier calendar day, in the shop's own timezone, can no longer be
// voided — seeded via a raw INSERT with an explicit completed_at
// (seedSaleRow), the only way to give a sale a completed_at other than
// "now" (sales_immutable only blocks UPDATE/DELETE, never a plain
// INSERT).
func TestVoidSaleTx_windowClosedYesterday(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "void-window-yesterday")
	owner := seedUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	ownerCtx := ctxAs(shop.ID, owner)

	yesterday := mustNow(t, q, shop.ID).AddDate(0, 0, -1)
	saleID := seedSaleRow(ctx, t, pool, shop.ID, loc.ID, owner.ID, 1, yesterday)

	_, err := voidSale(ownerCtx, t, h, pool, q, saleID, nil)
	assertAPIErr(t, "void yesterday's sale", err, 409, gen.SALEVOIDWINDOWCLOSED)
}

// TestVoidSaleTx_windowEdgeAroundLocalMidnight is D-59's own named edge
// case: 23:30 local time on the previous calendar day is closed, while
// 00:30 local time on today's own calendar day (an instant that, in UTC,
// may itself fall on the previous UTC day for a positive-offset
// timezone such as the shop default Asia/Tashkent, +05:00) is still
// voidable — proving the comparison is done in the shop's own timezone,
// not UTC (mirrors TestPromoActive_endsTodayStillAppliesLateInTheDay's
// own reasoning, pricing_test.go). "Now" is pinned via sales.WithNow
// (service.go) rather than read from the real wall clock: a real-time
// version of this test would flip if the run happened to straddle local
// midnight — a fixed instant instead of mustNow(...) removes that flake
// entirely, the same way TestPromoActive_endsTodayStillAppliesLateInTheDay
// pins a controlled `now` for promoActive.
func TestVoidSaleTx_windowEdgeAroundLocalMidnight(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	tashkent, err := time.LoadLocation("Asia/Tashkent")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	pinnedNow := time.Date(2026, 6, 15, 10, 0, 0, 0, tashkent)
	h := sales.NewHandler(sales.NewService(q, sales.WithNow(func() time.Time { return pinnedNow })))

	shop := seedShop(ctx, t, q, "void-window-edge")
	owner := seedUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	ownerCtx := ctxAs(shop.ID, owner)

	lateYesterday := time.Date(2026, 6, 14, 23, 30, 0, 0, tashkent)
	earlyToday := time.Date(2026, 6, 15, 0, 30, 0, 0, tashkent)

	closedSaleID := seedSaleRow(ctx, t, pool, shop.ID, loc.ID, owner.ID, 1, lateYesterday)
	_, err = voidSale(ownerCtx, t, h, pool, q, closedSaleID, nil)
	assertAPIErr(t, "23:30 local yesterday", err, 409, gen.SALEVOIDWINDOWCLOSED)

	openSaleID := seedSaleRow(ctx, t, pool, shop.ID, loc.ID, owner.ID, 2, earlyToday)
	voided, err := voidSale(ownerCtx, t, h, pool, q, openSaleID, nil)
	if err != nil {
		t.Fatalf("void 00:30 local today: %v", err)
	}
	if voided.Status != gen.SaleStatus(db.SaleStatusVoided) {
		t.Fatalf("Status = %q, want voided", voided.Status)
	}
}

// TestVoidSaleTx_saleWithReturnHasReturns is D-62: a sale that already
// has a completed return referencing it cannot be voided.
func TestVoidSaleTx_saleWithReturnHasReturns(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "void-has-returns")
	owner := seedUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "void-has-returns-product", "10.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	ownerCtx := ctxAs(shop.ID, owner)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "5.000")

	sale, err := createSale(ownerCtx, t, h, pool, q, saleBody(loc.ID, variant.ID, "2.000"))
	if err != nil {
		t.Fatalf("createSale: %v", err)
	}

	items := []gen.SaleReturnItemCreate{{SaleItemId: sale.Items[0].Id, Qty: "1.000"}}
	if _, err := createSaleReturn(ownerCtx, t, h, pool, q, sale.Id, &gen.SaleReturnCreate{Items: items}); err != nil {
		t.Fatalf("createSaleReturn: %v", err)
	}

	_, err = voidSale(ownerCtx, t, h, pool, q, sale.Id, nil)
	assertAPIErr(t, "void a sale with a return", err, 409, gen.SALEHASRETURNS)
}

// TestVoidSaleTx_returnIsNotVoidable is D-66: a return-kind sale can
// never be voided; the correction is to sell the item again.
func TestVoidSaleTx_returnIsNotVoidable(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "void-return-not-voidable")
	owner := seedUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "void-return-product", "10.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	ownerCtx := ctxAs(shop.ID, owner)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "5.000")

	sale, err := createSale(ownerCtx, t, h, pool, q, saleBody(loc.ID, variant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createSale: %v", err)
	}
	items := []gen.SaleReturnItemCreate{{SaleItemId: sale.Items[0].Id, Qty: "1.000"}}
	ret, err := createSaleReturn(ownerCtx, t, h, pool, q, sale.Id, &gen.SaleReturnCreate{Items: items})
	if err != nil {
		t.Fatalf("createSaleReturn: %v", err)
	}

	_, err = voidSale(ownerCtx, t, h, pool, q, ret.Id, nil)
	assertAPIErr(t, "void a return", err, 409, gen.SALENOTVOIDABLE)
}

// TestVoidSaleTx_secondVoidIsAlreadyVoided proves a replayed void 409s
// SALE_ALREADY_VOIDED rather than restoring stock twice — there is no
// Idempotency-Key on this operation (contracts/openapi.yaml), so this is
// the only safety net a client retry gets.
func TestVoidSaleTx_secondVoidIsAlreadyVoided(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "void-twice")
	owner := seedUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "void-twice-product", "10.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	ownerCtx := ctxAs(shop.ID, owner)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "5.000")

	sale, err := createSale(ownerCtx, t, h, pool, q, saleBody(loc.ID, variant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createSale: %v", err)
	}
	if _, err := voidSale(ownerCtx, t, h, pool, q, sale.Id, nil); err != nil {
		t.Fatalf("first void: %v", err)
	}

	_, err = voidSale(ownerCtx, t, h, pool, q, sale.Id, nil)
	assertAPIErr(t, "second void", err, 409, gen.SALEALREADYVOIDED)

	if got := countMovements(ctx, t, pool, shop.ID, variant.ID, loc.ID, db.StockMovementKindSaleVoidIn); got != 1 {
		t.Fatalf("sale_void_in movements = %d, want 1 (the second void must not have written a second one)", got)
	}
}
