package public_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/db"
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

// TestListPublicProducts_qSpecialCharactersEscapedNotWildcards pins T3
// review round 2, MINOR 9: `%`, `_` and `\` in `?q=` must be treated as
// literal characters to search for (escapeLikePattern, convert.go), never
// as ILIKE wildcards — a search for a literal "%" must not match every
// product.
func TestListPublicProducts_qSpecialCharactersEscapedNotWildcards(t *testing.T) {
	h, _, _, q, _ := newTestHandler(t, "shop-a")
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID)

	percent := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{Slug: "percent", Name: "50% off", BasePrice: "1.00", IsActive: true})
	seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{Slug: "unrelated", Name: "Unrelated Item", BasePrice: "1.00", IsActive: true})

	qv := "50%"
	byPercent := listPublicProducts(ctxWithAcceptLanguage("uz"), t, h, gen.ListPublicProductsParams{Q: &qv})
	if len(byPercent.Items) != 1 || byPercent.Items[0].Id != percent.ID {
		t.Fatalf("q=%q Items = %+v, want only %q — a literal %% must not act as an ILIKE wildcard matching everything", qv, byPercent.Items, percent.Slug)
	}

	// _, \ must not error/500 either, whether or not anything matches.
	for _, qv := range []string{"under_score", `back\slash`, "50%_\\mixed"} {
		qCopy := qv
		if _, err := h.ListPublicProducts(ctxWithAcceptLanguage("uz"), gen.ListPublicProductsRequestObject{
			Params: gen.ListPublicProductsParams{Q: &qCopy},
		}); err != nil {
			t.Errorf("q=%q: %v, want no error", qv, err)
		}
	}
}

// TestListPublicProducts_qOverlyLong_cappedNotRejected pins MINOR 9: a
// pathologically long `?q=` (searchParam/maxSearchLength, convert.go)
// must be capped, never a 500 or an unbounded query.
func TestListPublicProducts_qOverlyLong_cappedNotRejected(t *testing.T) {
	h, _, _, q, _ := newTestHandler(t, "shop-a")
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID)
	seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{Slug: "p", Name: "P", BasePrice: "1.00", IsActive: true})

	huge := strings.Repeat("a", 10000)
	if _, err := h.ListPublicProducts(ctxWithAcceptLanguage("uz"), gen.ListPublicProductsRequestObject{
		Params: gen.ListPublicProductsParams{Q: &huge},
	}); err != nil {
		t.Fatalf("q=<10000 chars>: %v, want no error (capped, not rejected)", err)
	}
}

// TestListPublicProducts_limitEdgeCases pins MINOR 9's limit=0/-1/9999
// cases: clampLimit (pagination.go) must never let any of these panic or
// 500 — 0 and -1 fall back to the default, 9999 is capped to maxLimit,
// and every one of them still returns however many products actually
// exist when that is fewer than the effective limit.
func TestListPublicProducts_limitEdgeCases(t *testing.T) {
	h, _, _, q, _ := newTestHandler(t, "shop-a")
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID)
	for i := 0; i < 3; i++ {
		seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
			Slug: fmt.Sprintf("limit-edge-%d", i), Name: fmt.Sprintf("Limit Edge %d", i), BasePrice: "1.00", IsActive: true,
		})
	}

	for _, limit := range []int{0, -1, 9999} {
		l := limit
		out, err := h.ListPublicProducts(ctxWithAcceptLanguage("uz"), gen.ListPublicProductsRequestObject{
			Params: gen.ListPublicProductsParams{Limit: &l},
		})
		if err != nil {
			t.Fatalf("limit=%d: %v, want no error", limit, err)
		}
		items := gen.PublicProductList(out.(gen.ListPublicProducts200JSONResponse)).Items
		if len(items) != 3 {
			t.Errorf("limit=%d: Items = %+v, want all 3 products (fewer than any effective clamp)", limit, items)
		}
	}
}

// TestListPublicProducts_tamperedCursor_400NotPanic pins MINOR 9: a
// client-tampered cursor is a 400 VALIDATION_FAILED (pagination.Decode's
// own contract), never a panic or a 500.
func TestListPublicProducts_tamperedCursor_400NotPanic(t *testing.T) {
	h, _, _, q, _ := newTestHandler(t, "shop-a")
	seedShop(context.Background(), t, q, "shop-a")

	bogus := "not-a-valid-cursor!!!"
	_, err := h.ListPublicProducts(ctxWithAcceptLanguage("uz"), gen.ListPublicProductsRequestObject{
		Params: gen.ListPublicProductsParams{Cursor: &bogus},
	})
	if err == nil {
		t.Fatal("want an error for a tampered cursor, got none")
	}
	code, status := errCodeStatus(err)
	if status != http.StatusBadRequest || code != string(gen.VALIDATIONFAILED) {
		t.Fatalf("error = %v (code=%s, status=%d), want 400 VALIDATION_FAILED", err, code, status)
	}
}

// TestListPublicProducts_negativeAndFractionalQtyAndZeroThreshold pins
// MINOR 9's remaining availability edge cases: a negative qty (the shop
// allows going negative — shops.allow_negative_stock) still reads as
// out_of_stock, not some undefined fourth state; a fractional qty just
// above/below the shop's integer default threshold (2) still classifies
// correctly; and a product-level threshold override of exactly 0 means
// "low" never applies — only out_of_stock (qty <= 0) or in_stock.
func TestListPublicProducts_negativeAndFractionalQtyAndZeroThreshold(t *testing.T) {
	h, _, _, q, pool := newTestHandler(t, "shop-a")
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID)
	loc := seedLocation(ctx, t, q, shopRow.ID, "Main", true)

	allow := true
	if _, err := q.UpdateShop(ctx, db.UpdateShopParams{ID: shopRow.ID, AllowNegativeStock: &allow}); err != nil {
		t.Fatalf("UpdateShop(allow_negative_stock): %v", err)
	}

	negative := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{Slug: "negative-qty", Name: "NegativeQty", BasePrice: "1.00", IsActive: true})
	negativeVariant := seedVariant(ctx, t, q, shopRow.ID, negative.ID, variantSpec{IsActive: true})
	stockIn(ctx, t, pool, q, shopRow.ID, negativeVariant.ID, loc.ID, "2")
	stockIn(ctx, t, pool, q, shopRow.ID, negativeVariant.ID, loc.ID, "-5") // net -3

	fractionalLow := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{Slug: "fractional-low", Name: "FractionalLow", BasePrice: "1.00", IsActive: true})
	fractionalLowVariant := seedVariant(ctx, t, q, shopRow.ID, fractionalLow.ID, variantSpec{IsActive: true})
	stockIn(ctx, t, pool, q, shopRow.ID, fractionalLowVariant.ID, loc.ID, "1.999") // just under the shop default threshold (2)

	fractionalIn := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{Slug: "fractional-in", Name: "FractionalIn", BasePrice: "1.00", IsActive: true})
	fractionalInVariant := seedVariant(ctx, t, q, shopRow.ID, fractionalIn.ID, variantSpec{IsActive: true})
	stockIn(ctx, t, pool, q, shopRow.ID, fractionalInVariant.ID, loc.ID, "2.001") // just over

	zeroThreshold := int32(0)
	zeroThresholdProduct := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		Slug: "zero-threshold", Name: "ZeroThreshold", BasePrice: "1.00", IsActive: true, LowStockThreshold: &zeroThreshold,
	})
	zeroThresholdVariant := seedVariant(ctx, t, q, shopRow.ID, zeroThresholdProduct.ID, variantSpec{IsActive: true})
	stockIn(ctx, t, pool, q, shopRow.ID, zeroThresholdVariant.ID, loc.ID, "1") // > 0 == threshold, so in_stock, never low

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
	check("net negative qty", negative.ID.String(), gen.OutOfStock)
	check("qty just under the shop default threshold", fractionalLow.ID.String(), gen.Low)
	check("qty just over the shop default threshold", fractionalIn.ID.String(), gen.InStock)
	check("qty 1 with a threshold override of 0", zeroThresholdProduct.ID.String(), gen.InStock)
}

// publicHandler is the *public.Handler type alias this file's helpers use
// — declared once here since it is the only file that needs it as a
// parameter type (categories_test.go/shop_test.go call methods directly
// on the concrete type instead).
type publicHandler = interface {
	ListPublicProducts(context.Context, gen.ListPublicProductsRequestObject) (gen.ListPublicProductsResponseObject, error)
}
