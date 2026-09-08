package public_test

import (
	"context"
	"testing"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// TestPublic_isolatesShopBFromShopA pins T3 review round 2, MINOR 9's
// cross-shop cases: the public surface resolves exactly one shop, from
// PUBLIC_SHOP_SLUG (D-105), never from the request — hard rule 1 (every
// business query filters by shop_id from server-resolved state, never
// client input) applied to the one place this package has no auth
// context to read shop_id from at all. A second shop's products,
// categories and content must never leak into shop-a's responses no
// matter what a caller asks for.
func TestPublic_isolatesShopBFromShopA(t *testing.T) {
	h, _, contentSvc, q, _ := newTestHandler(t, "shop-a")
	ctx := context.Background()
	shopA := seedShop(ctx, t, q, "shop-a")
	shopB := seedShop(ctx, t, q, "shop-b")
	unitA := seedUnit(ctx, t, q, shopA.ID)
	unitB := seedUnit(ctx, t, q, shopB.ID)

	catB := seedCategory(ctx, t, q, shopB.ID, "shop-b-category", "Shop B Category", true)
	seedProduct(ctx, t, q, shopB.ID, unitB.ID, productSpec{
		CategoryID: &catB.ID, Slug: "shop-b-product", Name: "Shop B Product", BasePrice: "1.00", IsActive: true,
	})
	user := seedUser(ctx, t, q, shopB.ID, "owner-b")
	if _, err := contentSvc.Upsert(ctx, shopB.ID, gen.Hero, gen.LocaleUz, map[string]interface{}{"title": "Shop B Hero"}, user.ID); err != nil {
		t.Fatalf("Upsert(shop B hero): %v", err)
	}

	// Shop A's own product, for a sanity baseline the isolation checks
	// below can tell apart from "the endpoint is just broken".
	seedProduct(ctx, t, q, shopA.ID, unitA.ID, productSpec{Slug: "shop-a-product", Name: "Shop A Product", BasePrice: "1.00", IsActive: true})

	ctxUZ := ctxWithAcceptLanguage("uz")

	shopResp, err := h.GetPublicShop(ctxUZ, gen.GetPublicShopRequestObject{})
	if err != nil {
		t.Fatalf("GetPublicShop: %v", err)
	}
	out := gen.PublicShop(shopResp.(gen.GetPublicShop200JSONResponse))
	if out.Slug != "shop-a" {
		t.Fatalf("GetPublicShop.Slug = %q, want shop-a", out.Slug)
	}
	if out.Blocks.Hero != nil {
		t.Errorf("Blocks.Hero = %+v, want absent — shop A never saved a hero, shop B's must not leak in", out.Blocks.Hero)
	}

	catResp, err := h.ListPublicCategories(ctxUZ, gen.ListPublicCategoriesRequestObject{})
	if err != nil {
		t.Fatalf("ListPublicCategories: %v", err)
	}
	for _, c := range gen.PublicCategoryList(catResp.(gen.ListPublicCategories200JSONResponse)).Items {
		if c.Slug == catB.Slug {
			t.Errorf("shop B's category %q appeared in shop A's category list", catB.Slug)
		}
	}

	listResp, err := h.ListPublicProducts(ctxUZ, gen.ListPublicProductsRequestObject{})
	if err != nil {
		t.Fatalf("ListPublicProducts: %v", err)
	}
	var sawShopAProduct bool
	for _, item := range gen.PublicProductList(listResp.(gen.ListPublicProducts200JSONResponse)).Items {
		if item.Slug == "shop-b-product" {
			t.Fatal("shop B's product appeared in shop A's product list")
		}
		if item.Slug == "shop-a-product" {
			sawShopAProduct = true
		}
	}
	// T3 review round 3, MINOR 5: without this, a handler that (bug)
	// returned every product filtered to nothing would also pass the
	// "shop-b-product absent" check above for the wrong reason.
	if !sawShopAProduct {
		t.Fatal("shop A's own product is missing from shop A's product list")
	}

	// Shop B's own slug must 404 through shop A's resolved handler — it
	// simply does not exist in shop A's tenant.
	_, err = h.GetPublicProductBySlug(ctxUZ, gen.GetPublicProductBySlugRequestObject{Slug: "shop-b-product"})
	assertNotFound(t, "GetPublicProductBySlug(shop-b-product) via shop-a", err)

	// Filtering by shop B's category slug must match nothing, not error —
	// the category simply does not exist for shop A's shop_id, the same
	// way an unknown slug filters to empty rather than 404ing (O-22's own
	// "category filter" semantics apply per-shop).
	byCategory, err := h.ListPublicProducts(ctxUZ, gen.ListPublicProductsRequestObject{
		Params: gen.ListPublicProductsParams{Category: &catB.Slug},
	})
	if err != nil {
		t.Fatalf("ListPublicProducts(category=shop-b-category): %v", err)
	}
	if items := gen.PublicProductList(byCategory.(gen.ListPublicProducts200JSONResponse)).Items; len(items) != 0 {
		t.Errorf("ListPublicProducts(category=%s) = %+v, want empty (shop B's category slug matches nothing in shop A)", catB.Slug, items)
	}
}

// TestGetPublicShop_foreignShopMedia_imageAbsent pins the same isolation
// rule for GetPublicShop's hero image resolution specifically: a hero
// block saved with an imageMediaId that names a real media_files row —
// just one that belongs to a *different* shop — must resolve to no image
// at all, the same as a dangling/nonexistent id (GetMediaFile already
// filters by ShopID, so this is really the same code path as
// TestGetPublicShop_heroImage_resolvesAndToleratesMissingMedia's second
// case; this test pins that the shop_id filter, not just the id, is what
// makes it absent).
func TestGetPublicShop_foreignShopMedia_imageAbsent(t *testing.T) {
	h, _, contentSvc, q, _ := newTestHandler(t, "shop-a")
	ctx := context.Background()
	shopA := seedShop(ctx, t, q, "shop-a")
	shopB := seedShop(ctx, t, q, "shop-b")
	user := seedUser(ctx, t, q, shopA.ID, "owner-a")
	foreignMedia := seedMedia(ctx, t, q, shopB.ID, "shop-b/hero")

	if _, err := contentSvc.Upsert(ctx, shopA.ID, gen.Hero, gen.LocaleUz, map[string]interface{}{
		"title": "Hero", "imageMediaId": foreignMedia.ID.String(),
	}, user.ID); err != nil {
		t.Fatalf("Upsert(hero, foreign media id): %v", err)
	}

	out := mustGetPublicShop(ctxWithAcceptLanguage("uz"), t, h)
	if out.Blocks.Hero == nil {
		t.Fatal("Blocks.Hero = nil, want the block present (just without an image)")
	}
	if out.Blocks.Hero.Image != nil {
		t.Errorf("Blocks.Hero.Image = %+v, want absent — the media belongs to a different shop", out.Blocks.Hero.Image)
	}
}
