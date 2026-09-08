package public_test

import (
	"context"
	"testing"
	"time"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

func getPublicProduct(ctx context.Context, t *testing.T, h interface {
	GetPublicProductBySlug(context.Context, gen.GetPublicProductBySlugRequestObject) (gen.GetPublicProductBySlugResponseObject, error)
}, slug string) (gen.ProductPublic, error) {
	t.Helper()
	resp, err := h.GetPublicProductBySlug(ctx, gen.GetPublicProductBySlugRequestObject{Slug: slug})
	if err != nil {
		return gen.ProductPublic{}, err
	}
	product, ok := resp.(gen.GetPublicProductBySlug200JSONResponse)
	if !ok {
		t.Fatalf("GetPublicProductBySlug response type = %T", resp)
	}
	return gen.ProductPublic(product), nil
}

func TestGetPublicProductBySlug_notFoundCases(t *testing.T) {
	h, _, _, q, _ := newTestHandler(t, "shop-a")
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID)
	inactiveCat := seedCategory(ctx, t, q, shopRow.ID, "old", "Old", false)

	seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{Slug: "inactive", Name: "Inactive", BasePrice: "1.00", IsActive: false})
	seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{CategoryID: &inactiveCat.ID, Slug: "hidden-cat", Name: "HiddenCat", BasePrice: "1.00", IsActive: true})

	for _, slug := range []string{"no-such-slug", "inactive", "hidden-cat"} {
		_, err := getPublicProduct(ctxWithAcceptLanguage("uz"), t, h, slug)
		assertNotFound(t, "GetPublicProductBySlug("+slug+")", err)
	}
}

func TestGetPublicProductBySlug_variantsExcludeInactiveAndCarryPrice(t *testing.T) {
	h, _, _, q, pool := newTestHandler(t, "shop-a")
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID)
	cat := seedCategory(ctx, t, q, shopRow.ID, "shirts", "Shirts", true)
	loc := seedLocation(ctx, t, q, shopRow.ID, "Main", true)

	product := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		CategoryID: &cat.ID, Slug: "shirt", Name: "Shirt", BasePrice: "100000.00", IsActive: true,
	})
	activeVariant := seedVariant(ctx, t, q, shopRow.ID, product.ID, variantSpec{Attributes: `{"size":"M"}`, PriceOverride: "120000.00", IsActive: true})
	stockIn(ctx, t, pool, q, shopRow.ID, activeVariant.ID, loc.ID, "10")
	inactiveVariant := seedVariant(ctx, t, q, shopRow.ID, product.ID, variantSpec{Attributes: `{"size":"L"}`, IsActive: false})
	stockIn(ctx, t, pool, q, shopRow.ID, inactiveVariant.ID, loc.ID, "10")

	out, err := getPublicProduct(ctxWithAcceptLanguage("uz"), t, h, "shirt")
	if err != nil {
		t.Fatalf("GetPublicProductBySlug: %v", err)
	}
	if !out.CategoryId.IsSpecified() || out.CategoryId.MustGet() != cat.ID {
		t.Errorf("CategoryId = %+v, want %v", out.CategoryId, cat.ID)
	}
	if !out.CategorySlug.IsSpecified() || out.CategorySlug.MustGet() != "shirts" {
		t.Errorf("CategorySlug = %+v, want shirts", out.CategorySlug)
	}
	if !out.CategoryName.IsSpecified() || out.CategoryName.MustGet() != "Shirts" {
		t.Errorf("CategoryName = %+v, want Shirts", out.CategoryName)
	}
	if out.Variants == nil || len(*out.Variants) != 1 {
		t.Fatalf("Variants = %+v, want exactly 1 (the inactive one excluded, O-20)", out.Variants)
	}
	v := (*out.Variants)[0]
	if v.Id != activeVariant.ID {
		t.Errorf("Variants[0].Id = %v, want the active variant", v.Id)
	}
	if v.Availability != gen.InStock {
		t.Errorf("Variants[0].Availability = %q, want in_stock", v.Availability)
	}
	if v.Price.Regular != "120000.00" || v.Price.Current != "120000.00" || v.Price.PromoActive {
		t.Errorf("Variants[0].Price = %+v, want regular=current=120000.00, promoActive=false", v.Price)
	}
}

// TestGetPublicProductBySlug_emptyVariantsWhenNoneActive is the detail
// half of TestListPublicProducts_availabilityIgnoresInactiveVariants
// (products_test.go): the same two products must show an empty `variants`
// array here — the list's out_of_stock and the detail's empty array are
// the two ways this contract expresses "nothing sellable", and T3 keeps
// them in agreement (there is no product-level availability field in
// ProductPublic; O-20's product-level rule applies to the list response
// only, gen.PublicProductListItem.Availability).
func TestGetPublicProductBySlug_emptyVariantsWhenNoneActive(t *testing.T) {
	h, _, _, q, pool := newTestHandler(t, "shop-a")
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID)
	loc := seedLocation(ctx, t, q, shopRow.ID, "Main", true)

	onlyInactiveVariant := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		Slug: "detail-only-inactive-variant", Name: "OnlyInactiveVariant", BasePrice: "1.00", IsActive: true,
	})
	inactiveVariant := seedVariant(ctx, t, q, shopRow.ID, onlyInactiveVariant.ID, variantSpec{IsActive: false})
	stockIn(ctx, t, pool, q, shopRow.ID, inactiveVariant.ID, loc.ID, "50")

	noVariants := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		Slug: "detail-no-variants", Name: "NoVariants", BasePrice: "1.00", IsActive: true,
	})

	for _, slug := range []string{onlyInactiveVariant.Slug, noVariants.Slug} {
		out, err := getPublicProduct(ctxWithAcceptLanguage("uz"), t, h, slug)
		if err != nil {
			t.Fatalf("GetPublicProductBySlug(%s): %v", slug, err)
		}
		if out.Variants == nil || len(*out.Variants) != 0 {
			t.Errorf("GetPublicProductBySlug(%s).Variants = %+v, want an empty slice", slug, out.Variants)
		}
	}
}

func TestGetPublicProductBySlug_promoAcrossCalendarDayBoundary(t *testing.T) {
	h, _, _, q, _ := newTestHandler(t, "shop-a")
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID)

	loc, err := time.LoadLocation("Asia/Tashkent") // shops.timezone's own default (0001_shops.sql)
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	todayStart := time.Now().In(loc).Truncate(24 * time.Hour)
	yesterday := todayStart.Add(-24 * time.Hour)

	// Active promo: from yesterday to today (inclusive, D-68) — active
	// right now regardless of the time of day within today.
	activeFrom, activeTo := yesterday, todayStart
	activeProduct := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		Slug: "on-promo", Name: "OnPromo", BasePrice: "100000.00", PromoPrice: "80000.00",
		PromoFrom: &activeFrom, PromoTo: &activeTo, IsActive: true,
	})
	seedVariant(ctx, t, q, shopRow.ID, activeProduct.ID, variantSpec{IsActive: true})

	// Expired promo: ended yesterday — never active today.
	expiredFrom := yesterday.Add(-24 * time.Hour)
	expiredTo := yesterday
	expiredProduct := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		Slug: "expired-promo", Name: "ExpiredPromo", BasePrice: "100000.00", PromoPrice: "80000.00",
		PromoFrom: &expiredFrom, PromoTo: &expiredTo, IsActive: true,
	})
	seedVariant(ctx, t, q, shopRow.ID, expiredProduct.ID, variantSpec{IsActive: true})

	onPromo, err := getPublicProduct(ctxWithAcceptLanguage("uz"), t, h, "on-promo")
	if err != nil {
		t.Fatalf("GetPublicProductBySlug(on-promo): %v", err)
	}
	v := (*onPromo.Variants)[0]
	if !v.Price.PromoActive || v.Price.Current != "80000.00" || v.Price.Regular != "100000.00" {
		t.Errorf("on-promo price = %+v, want current=80000.00 (active), regular=100000.00", v.Price)
	}

	expired, err := getPublicProduct(ctxWithAcceptLanguage("uz"), t, h, "expired-promo")
	if err != nil {
		t.Fatalf("GetPublicProductBySlug(expired-promo): %v", err)
	}
	ev := (*expired.Variants)[0]
	if ev.Price.PromoActive || ev.Price.Current != "100000.00" {
		t.Errorf("expired-promo price = %+v, want current=regular=100000.00, promoActive=false", ev.Price)
	}
}

func TestGetPublicProductBySlug_descriptionTranslationFallback(t *testing.T) {
	h, _, _, q, _ := newTestHandler(t, "shop-a")
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID)

	product := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{Slug: "desc", Name: "Uz Name", BasePrice: "1.00", IsActive: true})
	body := "Uz description"
	if err := q.UpsertProductTranslation(ctx, db.UpsertProductTranslationParams{ProductID: product.ID, Locale: "uz", Name: "Uz Name", Description: &body}); err != nil {
		t.Fatalf("UpsertProductTranslation: %v", err)
	}
	seedVariant(ctx, t, q, shopRow.ID, product.ID, variantSpec{IsActive: true})

	out, err := getPublicProduct(ctxWithAcceptLanguage("ru"), t, h, "desc")
	if err != nil {
		t.Fatalf("GetPublicProductBySlug: %v", err)
	}
	if out.Name != "Uz Name" {
		t.Errorf("Name = %q, want the uz fallback name", out.Name)
	}
	if !out.Description.IsSpecified() || out.Description.MustGet() != "Uz description" {
		t.Errorf("Description = %+v, want the uz fallback description", out.Description)
	}
	if !out.TranslationFallback {
		t.Errorf("TranslationFallback = false, want true (no ru translation)")
	}
	if out.Locale != gen.LocaleRu {
		t.Errorf("Locale = %v, want ru (the requested locale)", out.Locale)
	}
}
