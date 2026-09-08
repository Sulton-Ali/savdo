package public_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// forbiddenSubstrings are JSON keys (or key fragments) that must never
// appear anywhere in a public response — cost/margin/quantity (hard rule
// 5, O-20) and staff credentials (hard rule 9). "qty"/"threshold" are
// checked as whole words via a JSON-key shape (`"qty"`) rather than a
// bare substring, since "quantity" itself never appears in these
// schemas but this guards the wire key precisely the way a reviewer
// would grep for it.
var forbiddenSubstrings = []string{
	`"cost`, `"unitCost"`, `"margin"`, `"threshold"`, `"qty"`, `"email"`, `"passwordHash"`,
}

func assertNoForbiddenFields(t *testing.T, label string, body []byte) {
	t.Helper()
	s := string(body)
	for _, forbidden := range forbiddenSubstrings {
		if strings.Contains(s, forbidden) {
			t.Errorf("%s: raw JSON contains %q — hard rule 5/9 violation:\n%s", label, forbidden, s)
		}
	}
}

func TestPublicResponses_rawJSON_neverLeaksPrivateFields(t *testing.T) {
	h, _, contentSvc, q, pool := newTestHandler(t, "shop-a")
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID)
	user := seedUser(ctx, t, q, shopRow.ID, "owner")
	cat := seedCategory(ctx, t, q, shopRow.ID, "shirts", "Shirts", true)
	loc := seedLocation(ctx, t, q, shopRow.ID, "Main", true)

	if _, err := contentSvc.Upsert(ctx, shopRow.ID, gen.Hero, gen.LocaleUz, map[string]interface{}{"title": "Hero"}, user.ID); err != nil {
		t.Fatalf("Upsert(hero): %v", err)
	}

	product := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		CategoryID: &cat.ID, Slug: "shirt", Name: "Shirt", BasePrice: "100000.00", IsActive: true,
	})
	variant := seedVariant(ctx, t, q, shopRow.ID, product.ID, variantSpec{IsActive: true})
	stockIn(ctx, t, pool, q, shopRow.ID, variant.ID, loc.ID, "5")

	media := seedMedia(ctx, t, q, shopRow.ID, "shop-a/shirt")
	if _, err := q.AddProductImage(ctx, db.AddProductImageParams{
		ID: uuid.New(), ShopID: shopRow.ID, ProductID: product.ID, MediaID: media.ID, SortOrder: 0, IsCover: true,
	}); err != nil {
		t.Fatalf("AddProductImage: %v", err)
	}

	ctxUZ := ctxWithAcceptLanguage("uz")

	shopResp, err := h.GetPublicShop(ctxUZ, gen.GetPublicShopRequestObject{})
	if err != nil {
		t.Fatalf("GetPublicShop: %v", err)
	}
	assertNoForbiddenFields(t, "GetPublicShop", jsonBytes(t, gen.PublicShop(shopResp.(gen.GetPublicShop200JSONResponse))))

	catResp, err := h.ListPublicCategories(ctxUZ, gen.ListPublicCategoriesRequestObject{})
	if err != nil {
		t.Fatalf("ListPublicCategories: %v", err)
	}
	assertNoForbiddenFields(t, "ListPublicCategories", jsonBytes(t, gen.PublicCategoryList(catResp.(gen.ListPublicCategories200JSONResponse))))

	listResp, err := h.ListPublicProducts(ctxUZ, gen.ListPublicProductsRequestObject{})
	if err != nil {
		t.Fatalf("ListPublicProducts: %v", err)
	}
	assertNoForbiddenFields(t, "ListPublicProducts", jsonBytes(t, gen.PublicProductList(listResp.(gen.ListPublicProducts200JSONResponse))))

	getResp, err := h.GetPublicProductBySlug(ctxUZ, gen.GetPublicProductBySlugRequestObject{Slug: "shirt"})
	if err != nil {
		t.Fatalf("GetPublicProductBySlug: %v", err)
	}
	assertNoForbiddenFields(t, "GetPublicProductBySlug", jsonBytes(t, gen.ProductPublic(getResp.(gen.GetPublicProductBySlug200JSONResponse))))
}
