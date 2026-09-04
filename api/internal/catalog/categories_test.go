package catalog_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/catalog"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// mustCreateCategory creates a category as owner and fails the test on
// error, for tests that only care about a valid category existing (e.g.
// to build a parent chain), not about CreateCategory's own behaviour.
func mustCreateCategory(t *testing.T, h *catalog.Handler, shopID uuid.UUID, name string, parentID *uuid.UUID) gen.Category {
	t.Helper()
	resp, err := h.CreateCategory(owner(shopID), gen.CreateCategoryRequestObject{
		Body: &gen.CreateCategoryJSONRequestBody{ParentId: parentID, Translations: uzTranslations(name)},
	})
	if err != nil {
		t.Fatalf("CreateCategory(%q): %v", name, err)
	}
	created, ok := resp.(gen.CreateCategory201JSONResponse)
	if !ok {
		t.Fatalf("response type = %T, want CreateCategory201JSONResponse", resp)
	}
	return gen.Category(created)
}

func TestCreateCategory_slugGeneratedAndConflict(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")

	created := mustCreateCategory(t, h, shopRow.ID, "Kiyimlar", nil)
	if created.Slug != "kiyimlar" {
		t.Fatalf("Slug = %q, want %q", created.Slug, "kiyimlar")
	}

	// A second category with the same uz name should get a suffixed slug,
	// not a 409 (auto-generated slugs retry).
	created2 := mustCreateCategory(t, h, shopRow.ID, "Kiyimlar", nil)
	if created2.Slug != "kiyimlar-2" {
		t.Fatalf("Slug (2nd) = %q, want %q", created2.Slug, "kiyimlar-2")
	}

	// An explicit, already-taken slug is a hard 409 — no retry.
	_, err := h.CreateCategory(owner(shopRow.ID), gen.CreateCategoryRequestObject{
		Body: &gen.CreateCategoryJSONRequestBody{Slug: strPtr("kiyimlar"), Translations: uzTranslations("Boshqa")},
	})
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Code != gen.CONFLICT || apiErr.Details["field"] != "slug" {
		t.Fatalf("err = %#v, want 409 CONFLICT field=slug", err)
	}
}

func TestCreateCategory_translationsRequired(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")

	_, err := h.CreateCategory(owner(shopRow.ID), gen.CreateCategoryRequestObject{
		Body: &gen.CreateCategoryJSONRequestBody{},
	})
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("err = %#v, want 400 VALIDATION_FAILED", err)
	}
	fields := apiErr.Details["fields"].(map[string]string)
	if fields["translations"] != "required" {
		t.Fatalf("fields = %+v, want translations=required", fields)
	}
}

func TestCreateCategory_depthCappedAtThree(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")

	root := mustCreateCategory(t, h, shopRow.ID, "Root", nil)
	child := mustCreateCategory(t, h, shopRow.ID, "Child", &root.Id)
	grandchild := mustCreateCategory(t, h, shopRow.ID, "Grandchild", &child.Id)

	// A 4th level (great-grandchild) must be rejected.
	_, err := h.CreateCategory(owner(shopRow.ID), gen.CreateCategoryRequestObject{
		Body: &gen.CreateCategoryJSONRequestBody{ParentId: &grandchild.Id, Translations: uzTranslations("TooDeep")},
	})
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("err = %#v, want 400 VALIDATION_FAILED", err)
	}
	fields := apiErr.Details["fields"].(map[string]string)
	if fields["parentId"] != "invalid" {
		t.Fatalf("fields = %+v, want parentId=invalid", fields)
	}
}

func TestDeleteCategory_conflictWithProducts(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")

	cat := mustCreateCategory(t, h, shopRow.ID, "Kiyimlar", nil)

	// A product referencing the category, created directly via sqlc — this
	// test only needs a product to exist under the category, not to
	// exercise CreateProduct itself (see products_test.go for that).
	if _, err := q.CreateProduct(ctx, db.CreateProductParams{
		ID: uuid.New(), ShopID: shopRow.ID, UnitID: unit.ID, CategoryID: &cat.Id, Slug: "shirt",
		BasePrice: numeric(t, "10000.00"), IsActive: true,
	}); err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}

	_, err := h.DeleteCategory(owner(shopRow.ID), gen.DeleteCategoryRequestObject{Id: cat.Id})
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Code != gen.CONFLICT || apiErr.Details["field"] != "products" {
		t.Fatalf("err = %#v, want 409 CONFLICT field=products", err)
	}
}

func TestDeleteCategory_conflictWithActiveChildren(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")

	parent := mustCreateCategory(t, h, shopRow.ID, "Parent", nil)
	mustCreateCategory(t, h, shopRow.ID, "Child", &parent.Id)

	_, err := h.DeleteCategory(owner(shopRow.ID), gen.DeleteCategoryRequestObject{Id: parent.Id})
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("err = %#v, want 400 VALIDATION_FAILED", err)
	}
	fields := apiErr.Details["fields"].(map[string]string)
	if fields["id"] != "invalid" {
		t.Fatalf("fields = %+v, want id=invalid", fields)
	}
}

func TestListCategories_isolationAndIncludeInactive(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopA := seedShop(ctx, t, q, "shop-a")
	shopB := seedShop(ctx, t, q, "shop-b")

	mustCreateCategory(t, h, shopA.ID, "A Category", nil)
	mustCreateCategory(t, h, shopB.ID, "B Category", nil)

	resp, err := h.ListCategories(owner(shopA.ID), gen.ListCategoriesRequestObject{})
	if err != nil {
		t.Fatalf("ListCategories: %v", err)
	}
	list := resp.(gen.ListCategories200JSONResponse)
	if len(list.Items) != 1 || list.Items[0].Name != "A Category" {
		t.Fatalf("items = %+v, want exactly shop A's own category", list.Items)
	}
}
