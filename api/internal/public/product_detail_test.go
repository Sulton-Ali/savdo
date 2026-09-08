package public_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

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

// TestGetPublicProductBySlug_imageVariantIdNulledForInactiveVariant pins
// T3 review round 2, MINOR 5: an image tagged to a variant that has since
// gone inactive must not name that variant in the response — variantId
// is nulled (the chosen fix, over dropping the image outright), so the
// product's photo for that variant still shows in the gallery, just no
// longer attributed to a variant this response otherwise never mentions.
func TestGetPublicProductBySlug_imageVariantIdNulledForInactiveVariant(t *testing.T) {
	h, _, _, q, _ := newTestHandler(t, "shop-a")
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID)

	product := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		Slug: "image-variant", Name: "ImageVariant", BasePrice: "1.00", IsActive: true,
	})
	activeVariant := seedVariant(ctx, t, q, shopRow.ID, product.ID, variantSpec{Attributes: `{"size":"M"}`, IsActive: true})
	inactiveVariant := seedVariant(ctx, t, q, shopRow.ID, product.ID, variantSpec{Attributes: `{"size":"L"}`, IsActive: false})

	media := seedMedia(ctx, t, q, shopRow.ID, "shop-a/image-variant")
	if _, err := q.AddProductImage(ctx, db.AddProductImageParams{
		ID: uuid.New(), ShopID: shopRow.ID, ProductID: product.ID, VariantID: &inactiveVariant.ID,
		MediaID: media.ID, SortOrder: 0, IsCover: true,
	}); err != nil {
		t.Fatalf("AddProductImage(inactive variant): %v", err)
	}
	activeMedia := seedMedia(ctx, t, q, shopRow.ID, "shop-a/image-variant-active")
	if _, err := q.AddProductImage(ctx, db.AddProductImageParams{
		ID: uuid.New(), ShopID: shopRow.ID, ProductID: product.ID, VariantID: &activeVariant.ID,
		MediaID: activeMedia.ID, SortOrder: 1, IsCover: false,
	}); err != nil {
		t.Fatalf("AddProductImage(active variant): %v", err)
	}

	out, err := getPublicProduct(ctxWithAcceptLanguage("uz"), t, h, "image-variant")
	if err != nil {
		t.Fatalf("GetPublicProductBySlug: %v", err)
	}
	if out.Images == nil || len(*out.Images) != 2 {
		t.Fatalf("Images = %+v, want both images present (nulling variantId never drops the image)", out.Images)
	}
	byMediaID := map[uuid.UUID]gen.ProductImage{}
	for _, img := range *out.Images {
		byMediaID[img.MediaId] = img
	}
	inactiveImg, ok := byMediaID[media.ID]
	if !ok {
		t.Fatal("the inactive-variant image is missing entirely")
	}
	if inactiveImg.VariantId.IsSpecified() && !inactiveImg.VariantId.IsNull() {
		t.Errorf("inactive-variant image VariantId = %+v, want null", inactiveImg.VariantId)
	}
	activeImg, ok := byMediaID[activeMedia.ID]
	if !ok {
		t.Fatal("the active-variant image is missing entirely")
	}
	if !activeImg.VariantId.IsSpecified() || activeImg.VariantId.IsNull() || activeImg.VariantId.MustGet() != activeVariant.ID {
		t.Errorf("active-variant image VariantId = %+v, want %v", activeImg.VariantId, activeVariant.ID)
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
	// T3 review round 2, MAJOR 4: build today's local midnight with
	// time.Date, never time.Now().Truncate(24*time.Hour) — Truncate rounds
	// to a multiple of its duration since the Unix epoch (UTC), not since
	// this loc's own midnight, so for Asia/Tashkent (UTC+5) that "today"
	// would actually be 05:00 local, not 00:00: running this test between
	// local 00:00 and 05:00 would see "todayStart" in the future and the
	// promo windows below shift by a day, flaking exactly in that window.
	now := time.Now().In(loc)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	yesterday := todayStart.AddDate(0, 0, -1)
	tomorrow := todayStart.AddDate(0, 0, 1)

	// Active promo: from yesterday to today (inclusive, D-68) — active
	// right now regardless of the time of day within today.
	activeFrom, activeTo := yesterday, todayStart
	activeProduct := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		Slug: "on-promo", Name: "OnPromo", BasePrice: "100000.00", PromoPrice: "80000.00",
		PromoFrom: &activeFrom, PromoTo: &activeTo, IsActive: true,
	})
	seedVariant(ctx, t, q, shopRow.ID, activeProduct.ID, variantSpec{IsActive: true})

	// Expired promo: ended yesterday — never active today.
	expiredFrom := yesterday.AddDate(0, 0, -1)
	expiredTo := yesterday
	expiredProduct := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		Slug: "expired-promo", Name: "ExpiredPromo", BasePrice: "100000.00", PromoPrice: "80000.00",
		PromoFrom: &expiredFrom, PromoTo: &expiredTo, IsActive: true,
	})
	seedVariant(ctx, t, q, shopRow.ID, expiredProduct.ID, variantSpec{IsActive: true})

	// Future promo: starts tomorrow — not active yet, even though it will
	// be tomorrow (T3 review round 2, MAJOR 4's "add the promo-starts-
	// tomorrow case").
	futureFrom := tomorrow
	futureTo := tomorrow.AddDate(0, 0, 1)
	futureProduct := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		Slug: "future-promo", Name: "FuturePromo", BasePrice: "100000.00", PromoPrice: "80000.00",
		PromoFrom: &futureFrom, PromoTo: &futureTo, IsActive: true,
	})
	seedVariant(ctx, t, q, shopRow.ID, futureProduct.ID, variantSpec{IsActive: true})

	onPromo, err := getPublicProduct(ctxWithAcceptLanguage("uz"), t, h, "on-promo")
	if err != nil {
		t.Fatalf("GetPublicProductBySlug(on-promo): %v", err)
	}
	v := (*onPromo.Variants)[0]
	if !v.Price.PromoActive || v.Price.Current != "80000.00" || v.Price.Regular != "100000.00" {
		t.Errorf("on-promo price = %+v, want current=80000.00 (active), regular=100000.00", v.Price)
	}
	// D-109: promoPrice/promoFrom/promoTo are present while the promo is
	// active.
	if !onPromo.PromoPrice.IsSpecified() || onPromo.PromoPrice.IsNull() || onPromo.PromoPrice.MustGet() != "80000.00" {
		t.Errorf("on-promo PromoPrice = %+v, want 80000.00 (promo is active)", onPromo.PromoPrice)
	}
	if !onPromo.PromoFrom.IsSpecified() || onPromo.PromoFrom.IsNull() {
		t.Errorf("on-promo PromoFrom = %+v, want present (promo is active)", onPromo.PromoFrom)
	}
	if !onPromo.PromoTo.IsSpecified() || onPromo.PromoTo.IsNull() {
		t.Errorf("on-promo PromoTo = %+v, want present (promo is active)", onPromo.PromoTo)
	}

	expired, err := getPublicProduct(ctxWithAcceptLanguage("uz"), t, h, "expired-promo")
	if err != nil {
		t.Fatalf("GetPublicProductBySlug(expired-promo): %v", err)
	}
	ev := (*expired.Variants)[0]
	if ev.Price.PromoActive || ev.Price.Current != "100000.00" {
		t.Errorf("expired-promo price = %+v, want current=regular=100000.00, promoActive=false", ev.Price)
	}
	// D-109: an expired promo's fields are hidden too — not just a future
	// one — since the rule is "only while active", not "only before it
	// starts".
	if expired.PromoPrice.IsSpecified() && !expired.PromoPrice.IsNull() {
		t.Errorf("expired-promo PromoPrice = %+v, want null (promo is no longer active)", expired.PromoPrice)
	}
	if expired.PromoFrom.IsSpecified() && !expired.PromoFrom.IsNull() {
		t.Errorf("expired-promo PromoFrom = %+v, want null", expired.PromoFrom)
	}
	if expired.PromoTo.IsSpecified() && !expired.PromoTo.IsNull() {
		t.Errorf("expired-promo PromoTo = %+v, want null", expired.PromoTo)
	}

	future, err := getPublicProduct(ctxWithAcceptLanguage("uz"), t, h, "future-promo")
	if err != nil {
		t.Fatalf("GetPublicProductBySlug(future-promo): %v", err)
	}
	fv := (*future.Variants)[0]
	if fv.Price.PromoActive || fv.Price.Current != "100000.00" {
		t.Errorf("future-promo price = %+v, want current=regular=100000.00, promoActive=false (starts tomorrow)", fv.Price)
	}
	// D-109 (owner ruling): a promo that has not started yet must be
	// entirely invisible on the public site, not just inactive in price —
	// promoPrice/promoFrom/promoTo are null, never a preview of tomorrow's
	// promo window.
	if future.PromoPrice.IsSpecified() && !future.PromoPrice.IsNull() {
		t.Errorf("future-promo PromoPrice = %+v, want null (promo has not started yet)", future.PromoPrice)
	}
	if future.PromoFrom.IsSpecified() && !future.PromoFrom.IsNull() {
		t.Errorf("future-promo PromoFrom = %+v, want null", future.PromoFrom)
	}
	if future.PromoTo.IsSpecified() && !future.PromoTo.IsNull() {
		t.Errorf("future-promo PromoTo = %+v, want null", future.PromoTo)
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
	// T3 review round 2, MAJOR 3: locale reports where the data actually
	// came from (effectiveLocale), not the raw requested locale — a ru
	// request answered from a uz-only translation reports uz, consistent
	// with catalog's own products/categories/units.
	if out.Locale != gen.LocaleUz {
		t.Errorf("Locale = %v, want uz (the locale the name/description actually came from)", out.Locale)
	}
}
