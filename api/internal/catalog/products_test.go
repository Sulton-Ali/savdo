package catalog_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

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
