package sales_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/sales"
)

func TestGetSale_notFoundForAnotherShop(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shopA := seedShop(ctx, t, q, "get-sale-a")
	shopB := seedShop(ctx, t, q, "get-sale-b")
	ownerA := seedUser(ctx, t, q, shopA.ID, "owner-a", db.UserRoleOwner)
	ownerB := seedUser(ctx, t, q, shopB.ID, "owner-b", db.UserRoleOwner)
	unit := seedUnit(ctx, t, q, shopA.ID, "pcs")
	product := seedProduct(ctx, t, q, shopA.ID, unit.ID, "get-sale-product", "10.00", productOpts{})
	variant := seedVariant(ctx, t, q, shopA.ID, product.ID)
	loc := seedLocation(ctx, t, q, shopA.ID, "Main")
	stockIn(ctx, t, pool, q, shopA.ID, variant.ID, loc.ID, "5.000")

	sale, err := createSale(ctxAs(shopA.ID, ownerA), t, h, pool, q, saleBody(loc.ID, variant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createSale: %v", err)
	}

	// Shop A's own owner can read it.
	if _, err := h.GetSale(ctxAs(shopA.ID, ownerA), gen.GetSaleRequestObject{Id: sale.Id}); err != nil {
		t.Fatalf("GetSale (own shop): %v", err)
	}

	// Shop B's owner gets 404 — the id exists, but not in this shop
	// (ADR-004: every query filters by the auth context's own shop_id).
	_, err = h.GetSale(ctxAs(shopB.ID, ownerB), gen.GetSaleRequestObject{Id: sale.Id})
	if err == nil {
		t.Fatal("want 404 NOT_FOUND for another shop's sale id, got no error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 404 {
		t.Fatalf("error = %v, want 404 NOT_FOUND", err)
	}

	// A random id in the same shop also 404s.
	_, err = h.GetSale(ctxAs(shopA.ID, ownerA), gen.GetSaleRequestObject{Id: uuid.New()})
	if err == nil {
		t.Fatal("want 404 NOT_FOUND for an unknown id, got no error")
	}
	if !errors.As(err, &apiErr) || apiErr.Status != 404 {
		t.Fatalf("error = %v, want 404 NOT_FOUND", err)
	}
}

func TestGetSale_hasReturnsFalseAndReturnedQtyZeroOnAFreshSale(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "get-sale-fresh")
	owner := seedUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "get-sale-fresh-product", "10.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	ownerCtx := ctxAs(shop.ID, owner)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "5.000")

	sale, err := createSale(ownerCtx, t, h, pool, q, saleBody(loc.ID, variant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createSale: %v", err)
	}

	resp, err := h.GetSale(ownerCtx, gen.GetSaleRequestObject{Id: sale.Id})
	if err != nil {
		t.Fatalf("GetSale: %v", err)
	}
	got, ok := resp.(gen.GetSale200JSONResponse)
	if !ok {
		t.Fatalf("GetSale response type = %T", resp)
	}
	if got.HasReturns {
		t.Fatal("HasReturns = true, want false on a fresh sale")
	}
	if len(got.Items) != 1 || got.Items[0].ReturnedQty != "0.000" {
		t.Fatalf("Items = %+v, want one line with returnedQty 0.000", got.Items)
	}
	if got.Kind != gen.SaleKind(db.SaleKindSale) || got.Status != gen.SaleStatus(db.SaleStatusCompleted) {
		t.Fatalf("Kind/Status = %s/%s, want sale/completed", got.Kind, got.Status)
	}
}
