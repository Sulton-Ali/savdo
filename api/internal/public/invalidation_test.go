package public_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/catalog"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// TestPublicCache_catalogProductDelete_invalidatesSoNextListReflectsChange
// pins T3 review round 2, MINOR 9's "a catalog write (product update)
// invalidates via catalog.Service.SetInvalidator" case — the cmd/api/
// main.go wiring (catalogSvc.SetInvalidator(publicSvc)) actually reaches
// the public cache, mirroring TestPublicCache_contentPUT_invalidates...
// (router_test.go) for content.Service. DeleteProduct is the cheapest
// catalog write to drive here: no translations/units/attributes payload
// to build, unlike Create/Update.
func TestPublicCache_catalogProductDelete_invalidatesSoNextListReflectsChange(t *testing.T) {
	h, svc, _, q, pool := newTestHandler(t, "shop-a")
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID)

	catalogSvc := catalog.NewService(pool, q, "uz", "/media")
	catalogSvc.SetInvalidator(svc)
	catalogHandler := catalog.NewHandler(catalogSvc)

	product := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		Slug: "goner", Name: "Goner", BasePrice: "1.00", IsActive: true,
	})

	before := listPublicProducts(ctxWithAcceptLanguage("uz"), t, h, gen.ListPublicProductsParams{})
	found := false
	for _, item := range before.Items {
		if item.Id == product.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("product not in the list before the delete — test fixture is broken")
	}

	ownerCtx := auth.WithContext(context.Background(), auth.Context{
		ShopID: shopRow.ID, UserID: uuid.New(), Role: db.UserRoleOwner, SessionID: uuid.New(), Client: db.SessionClientWeb,
	})
	if _, err := catalogHandler.DeleteProduct(ownerCtx, gen.DeleteProductRequestObject{Id: product.ID}); err != nil {
		t.Fatalf("DeleteProduct: %v", err)
	}

	after := listPublicProducts(ctxWithAcceptLanguage("uz"), t, h, gen.ListPublicProductsParams{})
	for _, item := range after.Items {
		if item.Id == product.ID {
			t.Fatal("the deleted product is still in the list right after the catalog write — public cache was not invalidated")
		}
	}
}
