package catalog_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/catalog"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

func mustCreateProduct(t *testing.T, h *catalog.Handler, shopID, unitID uuid.UUID, name, basePrice string) gen.Product {
	t.Helper()
	resp, err := h.CreateProduct(owner(shopID), gen.CreateProductRequestObject{
		Body: &gen.CreateProductJSONRequestBody{
			UnitId: unitID, BasePrice: basePrice, Translations: uzTranslations(name),
		},
	})
	if err != nil {
		t.Fatalf("CreateProduct(%q): %v", name, err)
	}
	created, ok := resp.(gen.CreateProduct201JSONResponse)
	if !ok {
		t.Fatalf("response type = %T, want CreateProduct201JSONResponse", resp)
	}
	return gen.Product(created)
}

func TestCreateProduct_implicitVariant(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")

	created := mustCreateProduct(t, h, shopRow.ID, unit.ID, "Cotton Shirt", "125000.00")

	if created.Variants == nil || len(*created.Variants) != 1 {
		t.Fatalf("Variants = %+v, want exactly one implicit variant", created.Variants)
	}
	if len((*created.Variants)[0].Attributes) != 0 {
		t.Fatalf("implicit variant attributes = %+v, want empty", (*created.Variants)[0].Attributes)
	}
}

func TestCreateProduct_basePriceRejectsMoreThanTwoDecimals(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")

	_, err := h.CreateProduct(owner(shopRow.ID), gen.CreateProductRequestObject{
		Body: &gen.CreateProductJSONRequestBody{UnitId: unit.ID, BasePrice: "12.345", Translations: uzTranslations("X")},
	})
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("err = %#v, want 400 VALIDATION_FAILED", err)
	}
	fields := apiErr.Details["fields"].(map[string]string)
	if fields["basePrice"] != "invalid" {
		t.Fatalf("fields = %+v, want basePrice=invalid", fields)
	}
}

func TestCreateProduct_promoDatesMustBeOrdered(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")

	from, err := time.Parse(time.RFC3339, "2026-02-01T00:00:00Z")
	if err != nil {
		t.Fatalf("parse from: %v", err)
	}
	to, err := time.Parse(time.RFC3339, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("parse to: %v", err)
	}

	_, err = h.CreateProduct(owner(shopRow.ID), gen.CreateProductRequestObject{
		Body: &gen.CreateProductJSONRequestBody{
			UnitId: unit.ID, BasePrice: "100.00", Translations: uzTranslations("X"),
			PromoFrom: &from, PromoTo: &to,
		},
	})
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("err = %#v, want 400 VALIDATION_FAILED", err)
	}
	fields := apiErr.Details["fields"].(map[string]string)
	if fields["promoTo"] != "invalid" {
		t.Fatalf("fields = %+v, want promoTo=invalid", fields)
	}
}

func TestCreateProduct_invalidUnitIsFieldError(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")

	_, err := h.CreateProduct(owner(shopRow.ID), gen.CreateProductRequestObject{
		Body: &gen.CreateProductJSONRequestBody{UnitId: uuid.New(), BasePrice: "100.00", Translations: uzTranslations("X")},
	})
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("err = %#v, want 400 VALIDATION_FAILED", err)
	}
	fields := apiErr.Details["fields"].(map[string]string)
	if fields["unitId"] != "invalid" {
		t.Fatalf("fields = %+v, want unitId=invalid", fields)
	}
}

func TestProducts_isolationBetweenShops(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopA := seedShop(ctx, t, q, "shop-a")
	shopB := seedShop(ctx, t, q, "shop-b")
	unitA := seedUnit(ctx, t, q, shopA.ID, "pcs")
	unitB := seedUnit(ctx, t, q, shopB.ID, "pcs")

	mustCreateProduct(t, h, shopA.ID, unitA.ID, "A Product", "100.00")
	productB := mustCreateProduct(t, h, shopB.ID, unitB.ID, "B Product", "200.00")

	// Shop A cannot see shop B's product via list...
	listResp, err := h.ListProducts(owner(shopA.ID), gen.ListProductsRequestObject{})
	if err != nil {
		t.Fatalf("ListProducts: %v", err)
	}
	list := listResp.(gen.ListProducts200JSONResponse)
	if len(list.Items) != 1 || list.Items[0].Name != "A Product" {
		t.Fatalf("items = %+v, want exactly shop A's own product", list.Items)
	}

	// ...nor via get (404, not leaked as some other error).
	_, err = h.GetProduct(owner(shopA.ID), gen.GetProductRequestObject{Id: productB.Id})
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Code != gen.NOTFOUND {
		t.Fatalf("err = %#v, want 404 NOT_FOUND", err)
	}
}

func TestProducts_cashierResponseHasNoCostOrTranslationsKeys(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")

	resp, err := h.CreateProduct(owner(shopRow.ID), gen.CreateProductRequestObject{
		Body: &gen.CreateProductJSONRequestBody{
			UnitId: unit.ID, BasePrice: "125000.00", CostPrice: strPtr("80000.00"), Translations: uzTranslations("Cotton Shirt"),
		},
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	productID := gen.Product(resp.(gen.CreateProduct201JSONResponse)).Id

	getResp, err := h.GetProduct(cashier(shopRow.ID), gen.GetProductRequestObject{Id: productID})
	if err != nil {
		t.Fatalf("GetProduct as cashier: %v", err)
	}
	body, err := json.Marshal(gen.Product(getResp.(gen.GetProduct200JSONResponse)))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, forbidden := range []string{"costPrice", "translations"} {
		if _, present := raw[forbidden]; present {
			t.Errorf("cashier product JSON has key %q, want it absent: %s", forbidden, body)
		}
	}

	// Variant costOverride must also be absent for the cashier.
	variants, _ := raw["variants"].([]any)
	if len(variants) != 1 {
		t.Fatalf("variants = %+v, want exactly one", variants)
	}
	v := variants[0].(map[string]any)
	if _, present := v["costOverride"]; present {
		t.Errorf("cashier variant JSON has key %q, want it absent: %s", "costOverride", body)
	}

	// A manager/owner sees both.
	getResp2, err := h.GetProduct(owner(shopRow.ID), gen.GetProductRequestObject{Id: productID})
	if err != nil {
		t.Fatalf("GetProduct as owner: %v", err)
	}
	ownerProduct := gen.Product(getResp2.(gen.GetProduct200JSONResponse))
	if ownerProduct.CostPrice == nil || *ownerProduct.CostPrice != "80000.00" {
		t.Fatalf("owner CostPrice = %v, want 80000.00", ownerProduct.CostPrice)
	}
	if ownerProduct.Translations == nil || ownerProduct.Translations.Uz == nil || ownerProduct.Translations.Uz.Name != "Cotton Shirt" {
		t.Fatalf("owner Translations = %+v", ownerProduct.Translations)
	}
}

func TestListProducts_searchAndPaginationAndFallback(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")

	// Product with a ru translation, searched by ru name while
	// Accept-Language resolves to uz (the shop default, no header set) —
	// exercises the trigram search and translationFallback together.
	if _, err := h.CreateProduct(owner(shopRow.ID), gen.CreateProductRequestObject{
		Body: &gen.CreateProductJSONRequestBody{
			UnitId: unit.ID, BasePrice: "50000.00",
			Translations: gen.Translations{Uz: &gen.TranslationEntry{Name: "Kepka"}, Ru: &gen.TranslationEntry{Name: "Kepka Ru Name"}},
		},
	}); err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}

	for i := 0; i < 3; i++ {
		mustCreateProduct(t, h, shopRow.ID, unit.ID, "Filler "+string(rune('A'+i)), "1000.00")
	}

	q2 := "Kepka Ru"
	listResp, err := h.ListProducts(manager(shopRow.ID), gen.ListProductsRequestObject{Params: gen.ListProductsParams{Q: &q2}})
	if err != nil {
		t.Fatalf("ListProducts search: %v", err)
	}
	items := listResp.(gen.ListProducts200JSONResponse).Items
	if len(items) != 1 || items[0].Name != "Kepka" {
		t.Fatalf("search results = %+v, want exactly the Kepka product (found by its ru name)", items)
	}
	if items[0].TranslationFallback {
		t.Errorf("TranslationFallback = true, want false (a real uz translation exists)")
	}

	// Pagination: limit 1, expect a nextCursor, then a second page with a
	// different item.
	limit := 1
	page1Resp, err := h.ListProducts(manager(shopRow.ID), gen.ListProductsRequestObject{Params: gen.ListProductsParams{Limit: &limit}})
	if err != nil {
		t.Fatalf("ListProducts page1: %v", err)
	}
	page1 := page1Resp.(gen.ListProducts200JSONResponse)
	if len(page1.Items) != 1 {
		t.Fatalf("page1 items = %+v, want 1", page1.Items)
	}
	if !page1.NextCursor.IsSpecified() || page1.NextCursor.IsNull() {
		t.Fatalf("page1 NextCursor = %+v, want a cursor (4 products created)", page1.NextCursor)
	}
	cursor := page1.NextCursor.MustGet()

	page2Resp, err := h.ListProducts(manager(shopRow.ID), gen.ListProductsRequestObject{Params: gen.ListProductsParams{Limit: &limit, Cursor: &cursor}})
	if err != nil {
		t.Fatalf("ListProducts page2: %v", err)
	}
	page2 := page2Resp.(gen.ListProducts200JSONResponse)
	if len(page2.Items) != 1 || page2.Items[0].Id == page1.Items[0].Id {
		t.Fatalf("page2 items = %+v, want a different item than page1", page2.Items)
	}
}

// TestGetProduct_translationFallback proves a product with only a ru
// translation (no uz) reports translationFallback: true and locale: "ru"
// when the caller's resolved locale is uz (the shop default) — the SQL
// fallback order requested -> uz -> any picked "any" here.
func TestGetProduct_translationFallback(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")

	// CreateProduct itself requires a uz translation (default locale), so
	// this fallback scenario is built directly through sqlc: a product
	// whose only translation is ru.
	p, err := q.CreateProduct(ctx, db.CreateProductParams{
		ID: uuid.New(), ShopID: shopRow.ID, UnitID: unit.ID, Slug: "ru-only", BasePrice: numeric(t, "10000.00"), IsActive: true,
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	if err := q.UpsertProductTranslation(ctx, db.UpsertProductTranslationParams{
		ProductID: p.ID, Locale: "ru", Name: "Только по-русски",
	}); err != nil {
		t.Fatalf("UpsertProductTranslation: %v", err)
	}

	resp, err := h.GetProduct(manager(shopRow.ID), gen.GetProductRequestObject{Id: p.ID})
	if err != nil {
		t.Fatalf("GetProduct: %v", err)
	}
	got := gen.Product(resp.(gen.GetProduct200JSONResponse))
	if !got.TranslationFallback {
		t.Errorf("TranslationFallback = false, want true (no uz translation exists)")
	}
	if string(got.Locale) != "ru" {
		t.Errorf("Locale = %q, want %q", got.Locale, "ru")
	}
	if got.Name != "Только по-русски" {
		t.Errorf("Name = %q, want the ru name", got.Name)
	}
}

func TestCreateProduct_categoryFromAnotherShopIsRejected(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopA := seedShop(ctx, t, q, "shop-a")
	shopB := seedShop(ctx, t, q, "shop-b")
	unitA := seedUnit(ctx, t, q, shopA.ID, "pcs")

	otherShopCategory := mustCreateCategory(t, h, shopB.ID, "B Category", nil)

	_, err := h.CreateProduct(owner(shopA.ID), gen.CreateProductRequestObject{
		Body: &gen.CreateProductJSONRequestBody{
			UnitId: unitA.ID, CategoryId: &otherShopCategory.Id, BasePrice: "100.00", Translations: uzTranslations("X"),
		},
	})
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("err = %#v, want 400 VALIDATION_FAILED", err)
	}
	fields := apiErr.Details["fields"].(map[string]string)
	if fields["categoryId"] != "invalid" {
		t.Fatalf("fields = %+v, want categoryId=invalid", fields)
	}
}

func TestCreateProduct_unitFromAnotherShopIsRejected(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopA := seedShop(ctx, t, q, "shop-a")
	shopB := seedShop(ctx, t, q, "shop-b")
	otherShopUnit := seedUnit(ctx, t, q, shopB.ID, "kg")

	_, err := h.CreateProduct(owner(shopA.ID), gen.CreateProductRequestObject{
		Body: &gen.CreateProductJSONRequestBody{UnitId: otherShopUnit.ID, BasePrice: "100.00", Translations: uzTranslations("X")},
	})
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("err = %#v, want 400 VALIDATION_FAILED", err)
	}
	fields := apiErr.Details["fields"].(map[string]string)
	if fields["unitId"] != "invalid" {
		t.Fatalf("fields = %+v, want unitId=invalid", fields)
	}
}

func TestCreateProduct_deletedCategoryIsRejected(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")

	cat := mustCreateCategory(t, h, shopRow.ID, "Kiyimlar", nil)
	if _, err := h.DeleteCategory(owner(shopRow.ID), gen.DeleteCategoryRequestObject{Id: cat.Id}); err != nil {
		t.Fatalf("DeleteCategory: %v", err)
	}

	_, err := h.CreateProduct(owner(shopRow.ID), gen.CreateProductRequestObject{
		Body: &gen.CreateProductJSONRequestBody{UnitId: unit.ID, CategoryId: &cat.Id, BasePrice: "100.00", Translations: uzTranslations("X")},
	})
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("err = %#v, want 400 VALIDATION_FAILED", err)
	}
	fields := apiErr.Details["fields"].(map[string]string)
	if fields["categoryId"] != "invalid" {
		t.Fatalf("fields = %+v, want categoryId=invalid", fields)
	}
}

func TestUpdateProduct_categoryAndUnitMustBelongToShop(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopA := seedShop(ctx, t, q, "shop-a")
	shopB := seedShop(ctx, t, q, "shop-b")
	unitA := seedUnit(ctx, t, q, shopA.ID, "pcs")
	unitB := seedUnit(ctx, t, q, shopB.ID, "kg")
	otherShopCategory := mustCreateCategory(t, h, shopB.ID, "B Category", nil)

	product := mustCreateProduct(t, h, shopA.ID, unitA.ID, "Cotton Shirt", "125000.00")

	_, err := h.UpdateProduct(owner(shopA.ID), gen.UpdateProductRequestObject{
		Id:   product.Id,
		Body: &gen.UpdateProductJSONRequestBody{CategoryId: nullable.NewNullableWithValue(otherShopCategory.Id)},
	})
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("err = %#v, want 400 VALIDATION_FAILED", err)
	}
	fields := apiErr.Details["fields"].(map[string]string)
	if fields["categoryId"] != "invalid" {
		t.Fatalf("fields = %+v, want categoryId=invalid", fields)
	}

	_, err = h.UpdateProduct(owner(shopA.ID), gen.UpdateProductRequestObject{
		Id: product.Id, Body: &gen.UpdateProductJSONRequestBody{UnitId: &unitB.ID},
	})
	apiErr, ok = err.(*apierr.Error)
	if !ok || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("err = %#v, want 400 VALIDATION_FAILED", err)
	}
	fields = apiErr.Details["fields"].(map[string]string)
	if fields["unitId"] != "invalid" {
		t.Fatalf("fields = %+v, want unitId=invalid", fields)
	}
}

func TestUpdateProduct_promoCrossCheckAgainstStoredValue(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")

	from, err := time.Parse(time.RFC3339, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("parse from: %v", err)
	}
	to, err := time.Parse(time.RFC3339, "2026-02-01T00:00:00Z")
	if err != nil {
		t.Fatalf("parse to: %v", err)
	}

	resp, err := h.CreateProduct(owner(shopRow.ID), gen.CreateProductRequestObject{
		Body: &gen.CreateProductJSONRequestBody{
			UnitId: unit.ID, BasePrice: "100.00", Translations: uzTranslations("X"),
			PromoFrom: &from, PromoTo: &to,
		},
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	product := gen.Product(resp.(gen.CreateProduct201JSONResponse))

	// Patching only promoTo to a date before the stored promoFrom must be
	// rejected against the stored value, not accepted because promoFrom
	// was not repeated in this request.
	earlyTo := from.Add(-24 * time.Hour)
	_, err = h.UpdateProduct(owner(shopRow.ID), gen.UpdateProductRequestObject{
		Id: product.Id, Body: &gen.UpdateProductJSONRequestBody{PromoTo: nullable.NewNullableWithValue(earlyTo)},
	})
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("err (patch promoTo) = %#v, want 400 VALIDATION_FAILED", err)
	}
	fields := apiErr.Details["fields"].(map[string]string)
	if fields["promoTo"] != "invalid" {
		t.Fatalf("fields = %+v, want promoTo=invalid", fields)
	}

	// Patching only promoFrom to a date after the stored promoTo must
	// blame promoFrom (the field actually patched), not promoTo.
	lateFrom := to.Add(24 * time.Hour)
	_, err = h.UpdateProduct(owner(shopRow.ID), gen.UpdateProductRequestObject{
		Id: product.Id, Body: &gen.UpdateProductJSONRequestBody{PromoFrom: nullable.NewNullableWithValue(lateFrom)},
	})
	apiErr, ok = err.(*apierr.Error)
	if !ok || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("err (patch promoFrom) = %#v, want 400 VALIDATION_FAILED", err)
	}
	fields = apiErr.Details["fields"].(map[string]string)
	if fields["promoFrom"] != "invalid" {
		t.Fatalf("fields = %+v, want promoFrom=invalid", fields)
	}
}

func TestGetProduct_cashierGetsNotFoundForInactiveProduct(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")

	product := mustCreateProduct(t, h, shopRow.ID, unit.ID, "Cotton Shirt", "125000.00")
	inactive := false
	if _, err := h.UpdateProduct(owner(shopRow.ID), gen.UpdateProductRequestObject{
		Id: product.Id, Body: &gen.UpdateProductJSONRequestBody{IsActive: &inactive},
	}); err != nil {
		t.Fatalf("UpdateProduct (deactivate): %v", err)
	}

	// A cashier gets 404, same as it would from the list.
	_, err := h.GetProduct(cashier(shopRow.ID), gen.GetProductRequestObject{Id: product.Id})
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Code != gen.NOTFOUND {
		t.Fatalf("err = %#v, want 404 NOT_FOUND", err)
	}

	// An owner/manager can still fetch it directly (editing an inactive
	// product is a normal admin operation).
	if _, err := h.GetProduct(owner(shopRow.ID), gen.GetProductRequestObject{Id: product.Id}); err != nil {
		t.Fatalf("GetProduct as owner: %v", err)
	}
}

func TestProducts_variantCostGatedOnPermissionNotOnProductCostValue(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")
	seedAttribute(ctx, t, q, shopRow.ID, "size")

	// No costPrice set at all on the product — buildFullProduct's
	// includeCost must still come from the caller's permission, not from
	// whether the product happens to have a cost_price on file.
	resp, err := h.CreateProduct(owner(shopRow.ID), gen.CreateProductRequestObject{
		Body: &gen.CreateProductJSONRequestBody{
			UnitId: unit.ID, BasePrice: "125000.00", Translations: uzTranslations("Cotton Shirt"),
			Variants: &[]gen.VariantCreate{{Attributes: gen.AttributeValues{"size": "L"}, CostOverride: strPtr("50000.00")}},
		},
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	created := gen.Product(resp.(gen.CreateProduct201JSONResponse))
	if created.CostPrice != nil {
		t.Fatalf("CostPrice = %v, want nil (never set)", created.CostPrice)
	}
	if created.Variants == nil || len(*created.Variants) != 1 {
		t.Fatalf("Variants = %+v, want exactly 1", created.Variants)
	}
	v := (*created.Variants)[0]
	if !v.CostOverride.IsSpecified() || v.CostOverride.IsNull() || v.CostOverride.MustGet() != "50000.00" {
		t.Fatalf("owner variant CostOverride = %+v, want 50000.00 present (permission-gated, not value-gated)", v.CostOverride)
	}

	// A manager (also cost.read) reading the product back via GetProduct
	// must see the same thing.
	getResp, err := h.GetProduct(manager(shopRow.ID), gen.GetProductRequestObject{Id: created.Id})
	if err != nil {
		t.Fatalf("GetProduct as manager: %v", err)
	}
	got := gen.Product(getResp.(gen.GetProduct200JSONResponse))
	gv := (*got.Variants)[0]
	if !gv.CostOverride.IsSpecified() || gv.CostOverride.IsNull() || gv.CostOverride.MustGet() != "50000.00" {
		t.Fatalf("manager variant CostOverride = %+v, want 50000.00 present", gv.CostOverride)
	}
}

func TestListProducts_searchEscapedWildcardDoesNotMatchAsSQLWildcard(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")

	// If "%" in the search term reached ILIKE unescaped, it would act as a
	// SQL wildcard and match anything containing "50", then any
	// characters, then " Off" — exactly what this filler name contains,
	// while being unrelated enough (long, no shared words) that pg_trgm
	// similarity does not accidentally match it either. Escaping closes
	// both the ILIKE-wildcard route and leaves only a real substring/
	// trigram match to find the intended product.
	mustCreateProduct(t, h, shopRow.ID, unit.ID, "50% Off Deal", "1000.00")
	mustCreateProduct(t, h, shopRow.ID, unit.ID,
		"50 mumkin qadar uzoq va butunlay bogliq bolmagan sozlar toplami keyin Off", "1000.00")

	q1 := "50% Off"
	resp, err := h.ListProducts(owner(shopRow.ID), gen.ListProductsRequestObject{Params: gen.ListProductsParams{Q: &q1}})
	if err != nil {
		t.Fatalf("ListProducts: %v", err)
	}
	items := resp.(gen.ListProducts200JSONResponse).Items
	if len(items) != 1 || items[0].Name != "50% Off Deal" {
		t.Fatalf("items = %+v, want exactly the literal \"50%%\" match", items)
	}

	// A query longer than the search cap (100 runes) does not error — it
	// is simply truncated before reaching the database.
	tooLong := strings.Repeat("a", 150)
	if _, err := h.ListProducts(owner(shopRow.ID), gen.ListProductsRequestObject{Params: gen.ListProductsParams{Q: &tooLong}}); err != nil {
		t.Fatalf("ListProducts with an over-long q: %v", err)
	}
}
