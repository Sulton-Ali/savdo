package stock_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/stock"
)

func transferReq(variantID, fromID, toID uuid.UUID, qty string) gen.CreateStockTransferRequestObject {
	return gen.CreateStockTransferRequestObject{
		Body: &gen.StockTransferCreate{VariantId: variantID, FromLocationId: fromID, ToLocationId: toID, Qty: qty},
	}
}

func TestCreateStockTransfer_sameLocationIs409(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := stock.NewHandler(stock.NewService(pool, q))

	shop := seedShop(ctx, t, q, "transfer-same-location")
	manager := seedUser(ctx, t, q, shop.ID, "manager1", db.UserRoleManager)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "transfer-product")
	variant := seedVariant(ctx, t, q, shop.ID, product.ID, "{}")
	loc := seedLocation(ctx, t, q, shop.ID, "Main")

	_, err := h.CreateStockTransfer(ctxAs(shop.ID, manager), transferReq(variant.ID, loc.ID, loc.ID, "1.000"))
	if err == nil {
		t.Fatal("want SAME_LOCATION, got no error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Code != gen.SAMELOCATION {
		t.Fatalf("error = %v, want SAME_LOCATION", err)
	}
}

func TestCreateStockTransfer_insufficientRollsBackBothLegs(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := stock.NewHandler(stock.NewService(pool, q))

	shop := seedShop(ctx, t, q, "transfer-insufficient")
	manager := seedUser(ctx, t, q, shop.ID, "manager1", db.UserRoleManager)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "transfer-insufficient-product")
	variant := seedVariant(ctx, t, q, shop.ID, product.ID, "{}")
	from := seedLocation(ctx, t, q, shop.ID, "From")
	to := seedLocation(ctx, t, q, shop.ID, "To")

	// No opening stock at "from": the transfer_out leg must fail, and the
	// transfer_in leg (which would otherwise succeed on its own) must not
	// be left committed either.
	_, err := h.CreateStockTransfer(ctxAs(shop.ID, manager), transferReq(variant.ID, from.ID, to.ID, "1.000"))
	if err == nil {
		t.Fatal("want STOCK_INSUFFICIENT, got no error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Code != gen.STOCKINSUFFICIENT {
		t.Fatalf("error = %v, want STOCK_INSUFFICIENT", err)
	}

	if _, exists := readLevel(ctx, t, pool, shop.ID, variant.ID, from.ID); exists {
		t.Fatal("want no stock_levels row at 'from' after a rolled-back transfer, found one")
	}
	if _, exists := readLevel(ctx, t, pool, shop.ID, variant.ID, to.ID); exists {
		t.Fatal("want no stock_levels row at 'to' after a rolled-back transfer, found one")
	}
	if got := countMovements(ctx, t, pool, shop.ID, variant.ID, from.ID, ""); got != 0 {
		t.Fatalf("movements at 'from' = %d, want 0", got)
	}
	if got := countMovements(ctx, t, pool, shop.ID, variant.ID, to.ID, ""); got != 0 {
		t.Fatalf("movements at 'to' = %d, want 0", got)
	}
}

func TestCreateStockTransfer_successWritesTwoMovementsSharingRefID(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	svc := stock.NewService(pool, q)
	h := stock.NewHandler(svc)

	shop := seedShop(ctx, t, q, "transfer-success")
	manager := seedUser(ctx, t, q, shop.ID, "manager1", db.UserRoleManager)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "transfer-success-product")
	variant := seedVariant(ctx, t, q, shop.ID, product.ID, "{}")
	from := seedLocation(ctx, t, q, shop.ID, "From")
	to := seedLocation(ctx, t, q, shop.ID, "To")

	mCtx := ctxAs(shop.ID, manager)
	if _, err := svc.MoveInTx(ctx, stock.MoveParams{
		ShopID: shop.ID, VariantID: variant.ID, LocationID: from.ID,
		Kind: db.StockMovementKindPurchaseIn, Qty: d(t, "5.000"),
	}); err != nil {
		t.Fatalf("seed opening stock: %v", err)
	}

	resp, err := h.CreateStockTransfer(mCtx, transferReq(variant.ID, from.ID, to.ID, "2.000"))
	if err != nil {
		t.Fatalf("CreateStockTransfer: %v", err)
	}
	result, ok := resp.(gen.CreateStockTransfer201JSONResponse)
	if !ok {
		t.Fatalf("response type = %T, want CreateStockTransfer201JSONResponse", resp)
	}
	if len(result.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(result.Items))
	}

	outMv, inMv := result.Items[0], result.Items[1]
	if outMv.Kind != gen.StockMovementKind(db.StockMovementKindTransferOut) {
		t.Fatalf("Items[0].Kind = %s, want transfer_out", outMv.Kind)
	}
	if inMv.Kind != gen.StockMovementKind(db.StockMovementKindTransferIn) {
		t.Fatalf("Items[1].Kind = %s, want transfer_in", inMv.Kind)
	}
	if !outMv.RefId.IsSpecified() || !inMv.RefId.IsSpecified() || outMv.RefId.MustGet() != inMv.RefId.MustGet() {
		t.Fatalf("Items do not share a ref_id: out=%+v in=%+v", outMv.RefId, inMv.RefId)
	}
	if outMv.Qty != "-2.000" {
		t.Fatalf("Items[0].Qty = %s, want -2.000", outMv.Qty)
	}
	if inMv.Qty != "2.000" {
		t.Fatalf("Items[1].Qty = %s, want 2.000", inMv.Qty)
	}

	fromQty, _ := readLevel(ctx, t, pool, shop.ID, variant.ID, from.ID)
	if !fromQty.Equal(d(t, "3.000")) {
		t.Fatalf("from level = %s, want 3.000", fromQty)
	}
	toQty, _ := readLevel(ctx, t, pool, shop.ID, variant.ID, to.ID)
	if !toQty.Equal(d(t, "2.000")) {
		t.Fatalf("to level = %s, want 2.000", toQty)
	}
}

func TestCreateStockTransfer_variantFromAnotherShop404(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := stock.NewHandler(stock.NewService(pool, q))

	shop := seedShop(ctx, t, q, "transfer-other-shop")
	manager := seedUser(ctx, t, q, shop.ID, "manager1", db.UserRoleManager)
	loc1 := seedLocation(ctx, t, q, shop.ID, "L1")
	loc2 := seedLocation(ctx, t, q, shop.ID, "L2")

	otherShop := seedShop(ctx, t, q, "transfer-other-shop-b")
	otherUnit := seedUnit(ctx, t, q, otherShop.ID, "pcs")
	otherProduct := seedProduct(ctx, t, q, otherShop.ID, otherUnit.ID, "other-shop-product")
	otherVariant := seedVariant(ctx, t, q, otherShop.ID, otherProduct.ID, "{}")

	_, err := h.CreateStockTransfer(ctxAs(shop.ID, manager), transferReq(otherVariant.ID, loc1.ID, loc2.ID, "1.000"))
	if err == nil {
		t.Fatal("want a 404, got none")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 404 {
		t.Fatalf("error = %v, want 404", err)
	}
}

// TestCreateStockTransfer_opposingConcurrentTransfersDoNotDeadlock is
// MINOR 4's own reproduction: many concurrent A->B and B->A transfers of
// the same variant, run together so some of them genuinely overlap.
// Before sortedTransferLegs (transfers.go), each direction locked its two
// rows out-then-in — A->B locks A then B, B->A locks B then A — the
// classic opposite-order pattern Postgres detects as SQLSTATE 40P01 and
// kills one side of; some runs of this exact test, unfixed, surfaced that
// as a raw error from CreateStockTransfer. With rows locked in a
// consistent (locationID) order regardless of direction, every transfer
// queues on whichever row sorts first instead of deadlocking, so every
// call here must succeed (allow_negative_stock is on, so none can fail on
// insufficient stock either — the only thing this test wants to rule out
// is the deadlock).
func TestCreateStockTransfer_opposingConcurrentTransfersDoNotDeadlock(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	svc := stock.NewService(pool, q)
	h := stock.NewHandler(svc)

	shop := seedShopAllowNegative(ctx, t, q, "transfer-opposing-race")
	manager := seedUser(ctx, t, q, shop.ID, "manager1", db.UserRoleManager)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "transfer-opposing-race-product")
	variant := seedVariant(ctx, t, q, shop.ID, product.ID, "{}")
	locA := seedLocation(ctx, t, q, shop.ID, "A")
	locB := seedLocation(ctx, t, q, shop.ID, "B")
	mCtx := ctxAs(shop.ID, manager)

	const pairs = 15
	errs := make([]error, pairs*2)
	var wg sync.WaitGroup
	wg.Add(pairs * 2)
	for i := 0; i < pairs; i++ {
		go func(i int) {
			defer wg.Done()
			_, err := h.CreateStockTransfer(mCtx, transferReq(variant.ID, locA.ID, locB.ID, "1.000"))
			errs[2*i] = err
		}(i)
		go func(i int) {
			defer wg.Done()
			_, err := h.CreateStockTransfer(mCtx, transferReq(variant.ID, locB.ID, locA.ID, "1.000"))
			errs[2*i+1] = err
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("opposing concurrent transfers did not complete within 20s — likely deadlocked")
	}

	for i, err := range errs {
		if err != nil {
			t.Fatalf("transfer %d: want success (or a clean retry), got: %v", i, err)
		}
	}

	// Net effect: pairs A->B and pairs B->A cancel out.
	aQty, _ := readLevel(ctx, t, pool, shop.ID, variant.ID, locA.ID)
	if !aQty.Equal(d(t, "0.000")) {
		t.Fatalf("location A level = %s, want 0.000 (equal traffic both ways)", aQty)
	}
	bQty, _ := readLevel(ctx, t, pool, shop.ID, variant.ID, locB.ID)
	if !bQty.Equal(d(t, "0.000")) {
		t.Fatalf("location B level = %s, want 0.000 (equal traffic both ways)", bQty)
	}
}
