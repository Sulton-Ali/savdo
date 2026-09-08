package public_test

import (
	"context"
	"testing"

	"github.com/Sulton-Ali/savdo/api/gen"
)

func listPublicProducts(ctx context.Context, t *testing.T, h publicHandler, params gen.ListPublicProductsParams) gen.PublicProductList {
	t.Helper()
	resp, err := h.ListPublicProducts(ctx, gen.ListPublicProductsRequestObject{Params: params})
	if err != nil {
		t.Fatalf("ListPublicProducts: %v", err)
	}
	list, ok := resp.(gen.ListPublicProducts200JSONResponse)
	if !ok {
		t.Fatalf("ListPublicProducts response type = %T", resp)
	}
	return gen.PublicProductList(list)
}

func TestListPublicProducts_categoryFeaturedAndUncategorized(t *testing.T) {
	h, _, _, q, _ := newTestHandler(t, "shop-a")
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID)
	activeCat := seedCategory(ctx, t, q, shopRow.ID, "shirts", "Shirts", true)
	inactiveCat := seedCategory(ctx, t, q, shopRow.ID, "old", "Old", false)

	featured := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		CategoryID: &activeCat.ID, Slug: "red-shirt", Name: "Red Shirt", BasePrice: "100000.00", IsActive: true, IsFeatured: true,
	})
	plain := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		CategoryID: &activeCat.ID, Slug: "blue-shirt", Name: "Blue Shirt", BasePrice: "100000.00", IsActive: true,
	})
	uncategorized := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		Slug: "hat", Name: "Hat", BasePrice: "50000.00", IsActive: true,
	})
	seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		CategoryID: &inactiveCat.ID, Slug: "hidden", Name: "Hidden", BasePrice: "1.00", IsActive: true,
	})
	seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		CategoryID: &activeCat.ID, Slug: "inactive-product", Name: "Inactive", BasePrice: "1.00", IsActive: false,
	})

	// No filter: every visible product (uncategorized included, inactive
	// category and inactive product excluded), newest first (D-92).
	all := listPublicProducts(ctxWithAcceptLanguage("uz"), t, h, gen.ListPublicProductsParams{})
	if len(all.Items) != 3 {
		t.Fatalf("Items = %+v, want exactly 3 (red-shirt, blue-shirt, hat)", all.Items)
	}
	if all.Items[0].Id != uncategorized.ID {
		t.Errorf("Items[0] = %+v, want the most recently created (hat) first", all.Items[0])
	}
	var sawUncategorized bool
	for _, item := range all.Items {
		if item.Id == uncategorized.ID {
			sawUncategorized = true
			if item.CategorySlug.IsSpecified() && !item.CategorySlug.IsNull() {
				t.Errorf("uncategorized product CategorySlug = %+v, want null (O-22)", item.CategorySlug)
			}
		}
	}
	if !sawUncategorized {
		t.Fatal("uncategorized product not in the list at all — O-22 says it must be")
	}

	// category filter.
	slug := "shirts"
	byCategory := listPublicProducts(ctxWithAcceptLanguage("uz"), t, h, gen.ListPublicProductsParams{Category: &slug})
	if len(byCategory.Items) != 2 {
		t.Fatalf("byCategory.Items = %+v, want the 2 shirts products", byCategory.Items)
	}

	// featured filter.
	featuredOnly := true
	byFeatured := listPublicProducts(ctxWithAcceptLanguage("uz"), t, h, gen.ListPublicProductsParams{Featured: &featuredOnly})
	if len(byFeatured.Items) != 1 || byFeatured.Items[0].Id != featured.ID {
		t.Fatalf("byFeatured.Items = %+v, want only red-shirt", byFeatured.Items)
	}

	// q filter (name search).
	q2 := "Blue"
	byQ := listPublicProducts(ctxWithAcceptLanguage("uz"), t, h, gen.ListPublicProductsParams{Q: &q2})
	if len(byQ.Items) != 1 || byQ.Items[0].Id != plain.ID {
		t.Fatalf("byQ.Items = %+v, want only blue-shirt", byQ.Items)
	}
}

func TestListPublicProducts_cursorPaging(t *testing.T) {
	h, _, _, q, _ := newTestHandler(t, "shop-a")
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID)

	for i := 0; i < 3; i++ {
		seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
			Slug: "p" + string(rune('a'+i)), Name: "P" + string(rune('A'+i)), BasePrice: "1000.00", IsActive: true,
		})
	}

	limit := 1
	page1 := listPublicProducts(ctxWithAcceptLanguage("uz"), t, h, gen.ListPublicProductsParams{Limit: &limit})
	if len(page1.Items) != 1 || !page1.NextCursor.IsSpecified() || page1.NextCursor.IsNull() {
		t.Fatalf("page1 = %+v, want 1 item and a nextCursor (3 products exist)", page1)
	}
	cursor := page1.NextCursor.MustGet()

	page2 := listPublicProducts(ctxWithAcceptLanguage("uz"), t, h, gen.ListPublicProductsParams{Limit: &limit, Cursor: &cursor})
	if len(page2.Items) != 1 || page2.Items[0].Id == page1.Items[0].Id {
		t.Fatalf("page2 = %+v, want a different item than page1 (%+v)", page2, page1)
	}
}

func TestListPublicProducts_availabilityThresholds(t *testing.T) {
	h, _, _, q, pool := newTestHandler(t, "shop-a")
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID)
	loc := seedLocation(ctx, t, q, shopRow.ID, "Main", true)
	inactiveLoc := seedLocation(ctx, t, q, shopRow.ID, "Closed", false)

	// Shop default threshold is 2 (D-44 migration default).
	// No stockIn call at all: a variant with no stock_levels row is qty 0
	// (SumVariantQtyForProducts' own COALESCE), the same "never stocked"
	// case ListLow's doc comment describes — a zero-qty movement is not a
	// real movement and stock_movements rejects it (its own qty <> 0
	// check constraint).
	outOfStock := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{Slug: "out", Name: "Out", BasePrice: "1.00", IsActive: true})
	seedVariant(ctx, t, q, shopRow.ID, outOfStock.ID, variantSpec{IsActive: true})

	low := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{Slug: "low", Name: "Low", BasePrice: "1.00", IsActive: true})
	lowVariant := seedVariant(ctx, t, q, shopRow.ID, low.ID, variantSpec{IsActive: true})
	stockIn(ctx, t, pool, q, shopRow.ID, lowVariant.ID, loc.ID, "2") // at the shop default threshold

	inStock := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{Slug: "in", Name: "In", BasePrice: "1.00", IsActive: true})
	inVariant := seedVariant(ctx, t, q, shopRow.ID, inStock.ID, variantSpec{IsActive: true})
	stockIn(ctx, t, pool, q, shopRow.ID, inVariant.ID, loc.ID, "5")

	// Override: shop default is 2, but this product overrides to 5, so
	// qty 4 is still "low" (D-44: product override wins over the shop
	// default).
	override := 5
	overriddenLowThreshold := int32(override)
	overrideProduct := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		Slug: "override", Name: "Override", BasePrice: "1.00", IsActive: true, LowStockThreshold: &overriddenLowThreshold,
	})
	overrideVariant := seedVariant(ctx, t, q, shopRow.ID, overrideProduct.ID, variantSpec{IsActive: true})
	stockIn(ctx, t, pool, q, shopRow.ID, overrideVariant.ID, loc.ID, "4")

	// Inactive-location stock must never count (O-20).
	ignoredLocation := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{Slug: "ignored-loc", Name: "IgnoredLoc", BasePrice: "1.00", IsActive: true})
	ignoredLocationVariant := seedVariant(ctx, t, q, shopRow.ID, ignoredLocation.ID, variantSpec{IsActive: true})
	stockIn(ctx, t, pool, q, shopRow.ID, ignoredLocationVariant.ID, inactiveLoc.ID, "50")

	list := listPublicProducts(ctxWithAcceptLanguage("uz"), t, h, gen.ListPublicProductsParams{})
	byID := map[string]gen.PublicProductListItem{}
	for _, item := range list.Items {
		byID[item.Id.String()] = item
	}

	check := func(label, id string, want gen.Availability) {
		t.Helper()
		got, ok := byID[id]
		if !ok {
			t.Fatalf("%s: product not in list at all", label)
		}
		if got.Availability != want {
			t.Errorf("%s: Availability = %q, want %q", label, got.Availability, want)
		}
	}
	check("qty 0", outOfStock.ID.String(), gen.OutOfStock)
	check("qty == shop default threshold (2)", low.ID.String(), gen.Low)
	check("qty above shop default threshold", inStock.ID.String(), gen.InStock)
	check("qty 4 under a product override threshold of 5", overrideProduct.ID.String(), gen.Low)
	check("stock only at an inactive location", ignoredLocation.ID.String(), gen.OutOfStock)
}

// TestListPublicProducts_availabilityIgnoresInactiveVariants pins the T3
// fix: SumVariantQtyForProducts (stock.sql) now filters pv.is_active, so a
// product whose only stocked variant has since been deactivated — or that
// has no active variant at all — must never read as available on the list
// (O-20: "best of its active variants"), matching
// GET /public/products/{slug}, whose `variants` array already excludes
// inactive variants (handler.go's publicVariants).
func TestListPublicProducts_availabilityIgnoresInactiveVariants(t *testing.T) {
	h, _, _, q, pool := newTestHandler(t, "shop-a")
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID)
	loc := seedLocation(ctx, t, q, shopRow.ID, "Main", true)

	// Only variant is inactive but well stocked: must not read as
	// available.
	onlyInactiveVariant := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		Slug: "only-inactive-variant", Name: "OnlyInactiveVariant", BasePrice: "1.00", IsActive: true,
	})
	inactiveVariant := seedVariant(ctx, t, q, shopRow.ID, onlyInactiveVariant.ID, variantSpec{IsActive: false})
	stockIn(ctx, t, pool, q, shopRow.ID, inactiveVariant.ID, loc.ID, "50")

	// No variant at all.
	noVariants := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		Slug: "no-variants", Name: "NoVariants", BasePrice: "1.00", IsActive: true,
	})

	list := listPublicProducts(ctxWithAcceptLanguage("uz"), t, h, gen.ListPublicProductsParams{})
	byID := map[string]gen.PublicProductListItem{}
	for _, item := range list.Items {
		byID[item.Id.String()] = item
	}

	check := func(label, id string, want gen.Availability) {
		t.Helper()
		got, ok := byID[id]
		if !ok {
			t.Fatalf("%s: product not in list at all", label)
		}
		if got.Availability != want {
			t.Errorf("%s: Availability = %q, want %q", label, got.Availability, want)
		}
	}
	check("stock only on an inactive variant", onlyInactiveVariant.ID.String(), gen.OutOfStock)
	check("no variant at all", noVariants.ID.String(), gen.OutOfStock)
}

// publicHandler is the *public.Handler type alias this file's helpers use
// — declared once here since it is the only file that needs it as a
// parameter type (categories_test.go/shop_test.go call methods directly
// on the concrete type instead).
type publicHandler = interface {
	ListPublicProducts(context.Context, gen.ListPublicProductsRequestObject) (gen.ListPublicProductsResponseObject, error)
}
