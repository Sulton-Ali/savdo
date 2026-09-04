package stock_test

import (
	"context"
	"errors"
	"testing"

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
