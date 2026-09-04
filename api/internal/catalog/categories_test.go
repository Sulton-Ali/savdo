package catalog_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

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

func TestCreateCategory_populatesDescriptionAndTranslations(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")

	desc := "Erkaklar va ayollar kiyimlari"
	resp, err := h.CreateCategory(owner(shopRow.ID), gen.CreateCategoryRequestObject{
		Body: &gen.CreateCategoryJSONRequestBody{
			Translations: gen.Translations{
				Uz: &gen.TranslationEntry{Name: "Kiyimlar", Description: &desc},
				Ru: &gen.TranslationEntry{Name: "Odezhda"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	created := gen.Category(resp.(gen.CreateCategory201JSONResponse))

	if !created.Description.IsSpecified() || created.Description.IsNull() || created.Description.MustGet() != desc {
		t.Fatalf("Description = %+v, want %q", created.Description, desc)
	}
	if created.Translations == nil || created.Translations.Uz == nil || created.Translations.Uz.Name != "Kiyimlar" {
		t.Fatalf("Translations.Uz = %+v", created.Translations)
	}
	if created.Translations.Ru == nil || created.Translations.Ru.Name != "Odezhda" {
		t.Fatalf("Translations.Ru = %+v, want the ru entry too", created.Translations.Ru)
	}
}

func TestGetCategory_translationsGatedByCatalogWrite(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")

	created := mustCreateCategory(t, h, shopRow.ID, "Kiyimlar", nil)

	cashierResp, err := h.GetCategory(cashier(shopRow.ID), gen.GetCategoryRequestObject{Id: created.Id})
	if err != nil {
		t.Fatalf("GetCategory as cashier: %v", err)
	}
	cashierCat := gen.Category(cashierResp.(gen.GetCategory200JSONResponse))
	if cashierCat.Translations != nil {
		t.Fatalf("cashier Translations = %+v, want nil (no catalog.write)", cashierCat.Translations)
	}

	managerResp, err := h.GetCategory(manager(shopRow.ID), gen.GetCategoryRequestObject{Id: created.Id})
	if err != nil {
		t.Fatalf("GetCategory as manager: %v", err)
	}
	managerCat := gen.Category(managerResp.(gen.GetCategory200JSONResponse))
	if managerCat.Translations == nil || managerCat.Translations.Uz == nil || managerCat.Translations.Uz.Name != "Kiyimlar" {
		t.Fatalf("manager Translations = %+v, want the uz entry", managerCat.Translations)
	}
}

func TestUpdateCategory_nullParentMovesToRootAndNullImageClears(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	media := seedMedia(ctx, t, q, shopRow.ID, "shop/cat-image")

	parent := mustCreateCategory(t, h, shopRow.ID, "Parent", nil)
	resp, err := h.CreateCategory(owner(shopRow.ID), gen.CreateCategoryRequestObject{
		Body: &gen.CreateCategoryJSONRequestBody{ParentId: &parent.Id, ImageId: &media.ID, Translations: uzTranslations("Child")},
	})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	child := gen.Category(resp.(gen.CreateCategory201JSONResponse))
	if !child.ParentId.IsSpecified() || child.ParentId.IsNull() {
		t.Fatalf("child.ParentId = %+v, want the parent id set", child.ParentId)
	}
	if !child.ImageId.IsSpecified() || child.ImageId.IsNull() {
		t.Fatalf("child.ImageId = %+v, want the media id set", child.ImageId)
	}

	var nullParent nullable.Nullable[uuid.UUID]
	nullParent.SetNull()
	var nullImage nullable.Nullable[uuid.UUID]
	nullImage.SetNull()

	updateResp, err := h.UpdateCategory(owner(shopRow.ID), gen.UpdateCategoryRequestObject{
		Id:   child.Id,
		Body: &gen.UpdateCategoryJSONRequestBody{ParentId: nullParent, ImageId: nullImage},
	})
	if err != nil {
		t.Fatalf("UpdateCategory: %v", err)
	}
	updated := gen.Category(updateResp.(gen.UpdateCategory200JSONResponse))
	if !updated.ParentId.IsSpecified() || !updated.ParentId.IsNull() {
		t.Fatalf("updated.ParentId = %+v, want explicit null (moved to root)", updated.ParentId)
	}
	if !updated.ImageId.IsSpecified() || !updated.ImageId.IsNull() {
		t.Fatalf("updated.ImageId = %+v, want explicit null (cleared)", updated.ImageId)
	}
}

func TestUpdateCategory_imageIdMustExistInShop(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")

	created := mustCreateCategory(t, h, shopRow.ID, "Kiyimlar", nil)

	var badImage nullable.Nullable[uuid.UUID]
	badImage.Set(uuid.New())

	_, err := h.UpdateCategory(owner(shopRow.ID), gen.UpdateCategoryRequestObject{
		Id: created.Id, Body: &gen.UpdateCategoryJSONRequestBody{ImageId: badImage},
	})
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("err = %#v, want 400 VALIDATION_FAILED", err)
	}
	fields := apiErr.Details["fields"].(map[string]string)
	if fields["imageId"] != "invalid" {
		t.Fatalf("fields = %+v, want imageId=invalid", fields)
	}
}

func TestCreateCategory_softDeletedParentIsRejected(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")

	parent := mustCreateCategory(t, h, shopRow.ID, "Parent", nil)
	if _, err := h.DeleteCategory(owner(shopRow.ID), gen.DeleteCategoryRequestObject{Id: parent.Id}); err != nil {
		t.Fatalf("DeleteCategory: %v", err)
	}

	_, err := h.CreateCategory(owner(shopRow.ID), gen.CreateCategoryRequestObject{
		Body: &gen.CreateCategoryJSONRequestBody{ParentId: &parent.Id, Translations: uzTranslations("Child")},
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

func TestUpdateCategory_cannotReparentUnderOwnDescendant(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")

	root := mustCreateCategory(t, h, shopRow.ID, "Root", nil)
	child := mustCreateCategory(t, h, shopRow.ID, "Child", &root.Id)
	grandchild := mustCreateCategory(t, h, shopRow.ID, "Grandchild", &child.Id)

	// root cannot become a child of its own grandchild.
	_, err := h.UpdateCategory(owner(shopRow.ID), gen.UpdateCategoryRequestObject{
		Id:   root.Id,
		Body: &gen.UpdateCategoryJSONRequestBody{ParentId: nullable.NewNullableWithValue(grandchild.Id)},
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

func TestUpdateCategory_reparentRejectsWhenSubtreeWouldExceedDepth(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")

	// A 2-level subtree: movable -> leaf (movable has height 2).
	movable := mustCreateCategory(t, h, shopRow.ID, "Movable", nil)
	_ = mustCreateCategory(t, h, shopRow.ID, "Leaf", &movable.Id)

	// A destination at depth 2 (root -> destination): placing movable's
	// height-2 subtree under it would put "Leaf" at depth 2+2=4 > 3, even
	// though movable itself would only be at depth 3.
	destRoot := mustCreateCategory(t, h, shopRow.ID, "DestRoot", nil)
	dest := mustCreateCategory(t, h, shopRow.ID, "Dest", &destRoot.Id)

	_, err := h.UpdateCategory(owner(shopRow.ID), gen.UpdateCategoryRequestObject{
		Id:   movable.Id,
		Body: &gen.UpdateCategoryJSONRequestBody{ParentId: nullable.NewNullableWithValue(dest.Id)},
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

func TestCreateCategory_translationNameTooLong(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")

	tooLong := strings.Repeat("a", 201)
	_, err := h.CreateCategory(owner(shopRow.ID), gen.CreateCategoryRequestObject{
		Body: &gen.CreateCategoryJSONRequestBody{Translations: uzTranslations(tooLong)},
	})
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("err = %#v, want 400 VALIDATION_FAILED", err)
	}
	fields := apiErr.Details["fields"].(map[string]string)
	if fields["translations"] != "too_long" {
		t.Fatalf("fields = %+v, want translations=too_long", fields)
	}
}
