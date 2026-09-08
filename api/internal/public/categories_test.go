package public_test

import (
	"context"
	"testing"

	"github.com/Sulton-Ali/savdo/api/gen"
)

func TestListPublicCategories_activeOnlyWithProductCounts(t *testing.T) {
	h, _, _, q, _ := newTestHandler(t, "shop-a")
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID)

	activeCat := seedCategory(ctx, t, q, shopRow.ID, "shirts", "Shirts", true)
	inactiveCat := seedCategory(ctx, t, q, shopRow.ID, "old", "Old", false)

	seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		CategoryID: &activeCat.ID, Slug: "shirt-1", Name: "Shirt 1", BasePrice: "100000.00", IsActive: true,
	})
	seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		CategoryID: &activeCat.ID, Slug: "shirt-2", Name: "Shirt 2", BasePrice: "100000.00", IsActive: false, // inactive: not counted
	})
	seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		CategoryID: &inactiveCat.ID, Slug: "old-1", Name: "Old 1", BasePrice: "100000.00", IsActive: true,
	})

	resp, err := h.ListPublicCategories(ctxWithAcceptLanguage("uz"), gen.ListPublicCategoriesRequestObject{})
	if err != nil {
		t.Fatalf("ListPublicCategories: %v", err)
	}
	list := resp.(gen.ListPublicCategories200JSONResponse)
	if len(list.Items) != 1 {
		t.Fatalf("Items = %+v, want exactly the active category", list.Items)
	}
	if list.Items[0].Slug != "shirts" {
		t.Errorf("Items[0].Slug = %q, want shirts", list.Items[0].Slug)
	}
	if list.Items[0].ProductCount != 1 {
		t.Errorf("Items[0].ProductCount = %d, want 1 (only the active product)", list.Items[0].ProductCount)
	}
}
