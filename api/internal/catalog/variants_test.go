package catalog_test

import (
	"context"
	"testing"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
)

func TestCreateVariant_replacesImplicitAndSetsHasVariants(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")
	seedAttribute(ctx, t, q, shopRow.ID, "size")

	product := mustCreateProduct(t, h, shopRow.ID, unit.ID, "Cotton Shirt", "125000.00")
	if product.Variants == nil || len(*product.Variants) != 1 {
		t.Fatalf("initial variants = %+v, want exactly the implicit one", product.Variants)
	}
	implicitID := (*product.Variants)[0].Id

	resp, err := h.CreateVariant(owner(shopRow.ID), gen.CreateVariantRequestObject{
		Id:   product.Id,
		Body: &gen.CreateVariantJSONRequestBody{Attributes: gen.AttributeValues{"size": "L"}},
	})
	if err != nil {
		t.Fatalf("CreateVariant: %v", err)
	}
	created := resp.(gen.CreateVariant201JSONResponse)
	if created.Attributes["size"] != "L" {
		t.Fatalf("created.Attributes = %+v, want size=L", created.Attributes)
	}

	listResp, err := h.ListVariants(owner(shopRow.ID), gen.ListVariantsRequestObject{Id: product.Id})
	if err != nil {
		t.Fatalf("ListVariants: %v", err)
	}
	items := listResp.(gen.ListVariants200JSONResponse).Items
	if len(items) != 1 {
		t.Fatalf("variants after create = %+v, want exactly 1 (implicit replaced)", items)
	}
	if items[0].Id == implicitID {
		t.Fatalf("the implicit variant is still present, want it soft-deleted")
	}
}

func TestCreateVariant_duplicateAttributesConflict(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")
	seedAttribute(ctx, t, q, shopRow.ID, "size")

	product := mustCreateProduct(t, h, shopRow.ID, unit.ID, "Cotton Shirt", "125000.00")

	if _, err := h.CreateVariant(owner(shopRow.ID), gen.CreateVariantRequestObject{
		Id: product.Id, Body: &gen.CreateVariantJSONRequestBody{Attributes: gen.AttributeValues{"size": "L"}},
	}); err != nil {
		t.Fatalf("CreateVariant (1st): %v", err)
	}

	_, err := h.CreateVariant(owner(shopRow.ID), gen.CreateVariantRequestObject{
		Id: product.Id, Body: &gen.CreateVariantJSONRequestBody{Attributes: gen.AttributeValues{"size": "L"}},
	})
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Code != gen.CONFLICT || apiErr.Details["field"] != "attributes" {
		t.Fatalf("err = %#v, want 409 CONFLICT field=attributes", err)
	}
}

func TestCreateVariant_invalidAttributeCode(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")
	// No attribute definitions seeded — "size" is not a known code.

	product := mustCreateProduct(t, h, shopRow.ID, unit.ID, "Cotton Shirt", "125000.00")

	_, err := h.CreateVariant(owner(shopRow.ID), gen.CreateVariantRequestObject{
		Id: product.Id, Body: &gen.CreateVariantJSONRequestBody{Attributes: gen.AttributeValues{"size": "L"}},
	})
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("err = %#v, want 400 VALIDATION_FAILED", err)
	}
	fields := apiErr.Details["fields"].(map[string]string)
	if fields["attributes"] != "invalid" {
		t.Fatalf("fields = %+v, want attributes=invalid", fields)
	}
}

func TestDeleteVariant_onlyActiveVariantRejected(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")
	seedAttribute(ctx, t, q, shopRow.ID, "size")

	product := mustCreateProduct(t, h, shopRow.ID, unit.ID, "Cotton Shirt", "125000.00")
	implicitID := (*product.Variants)[0].Id

	// With only the implicit variant, deleting it must be rejected.
	_, err := h.DeleteVariant(owner(shopRow.ID), gen.DeleteVariantRequestObject{Id: implicitID})
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("err = %#v, want 400 VALIDATION_FAILED", err)
	}
	fields := apiErr.Details["fields"].(map[string]string)
	if fields["id"] != "invalid" {
		t.Fatalf("fields = %+v, want id=invalid", fields)
	}

	// Add a second (real) variant, replacing the implicit one — now
	// exactly one active variant exists again, and it must still be
	// undeletable.
	resp, err := h.CreateVariant(owner(shopRow.ID), gen.CreateVariantRequestObject{
		Id: product.Id, Body: &gen.CreateVariantJSONRequestBody{Attributes: gen.AttributeValues{"size": "L"}},
	})
	if err != nil {
		t.Fatalf("CreateVariant: %v", err)
	}
	onlyVariant := resp.(gen.CreateVariant201JSONResponse)

	_, err = h.DeleteVariant(owner(shopRow.ID), gen.DeleteVariantRequestObject{Id: onlyVariant.Id})
	apiErr, ok = err.(*apierr.Error)
	if !ok || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("err = %#v, want 400 VALIDATION_FAILED", err)
	}

	// A second real variant makes the first one deletable.
	if _, err := h.CreateVariant(owner(shopRow.ID), gen.CreateVariantRequestObject{
		Id: product.Id, Body: &gen.CreateVariantJSONRequestBody{Attributes: gen.AttributeValues{"size": "M"}},
	}); err != nil {
		t.Fatalf("CreateVariant (2nd real): %v", err)
	}
	if _, err := h.DeleteVariant(owner(shopRow.ID), gen.DeleteVariantRequestObject{Id: onlyVariant.Id}); err != nil {
		t.Fatalf("DeleteVariant should now succeed: %v", err)
	}
}

func TestUpdateVariant_attributesRewrittenInPlaceKeepingID(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")
	seedAttribute(ctx, t, q, shopRow.ID, "size")

	product := mustCreateProduct(t, h, shopRow.ID, unit.ID, "Cotton Shirt", "125000.00")
	created, err := h.CreateVariant(owner(shopRow.ID), gen.CreateVariantRequestObject{
		Id: product.Id, Body: &gen.CreateVariantJSONRequestBody{Attributes: gen.AttributeValues{"size": "L"}},
	})
	if err != nil {
		t.Fatalf("CreateVariant: %v", err)
	}
	variant := created.(gen.CreateVariant201JSONResponse)

	newAttrs := gen.AttributeValues{"size": "XL"}
	resp, err := h.UpdateVariant(owner(shopRow.ID), gen.UpdateVariantRequestObject{
		Id: variant.Id, Body: &gen.UpdateVariantJSONRequestBody{Attributes: &newAttrs},
	})
	if err != nil {
		t.Fatalf("UpdateVariant: %v", err)
	}
	updated := gen.Variant(resp.(gen.UpdateVariant200JSONResponse))

	if updated.Id != variant.Id {
		t.Fatalf("Id = %v, want unchanged %v (stock/sales references must survive)", updated.Id, variant.Id)
	}
	if updated.Attributes["size"] != "XL" {
		t.Fatalf("Attributes = %+v, want size=XL", updated.Attributes)
	}

	// Persisted, not just in the response: a fresh list shows the same id
	// with the new attributes.
	listResp, err := h.ListVariants(owner(shopRow.ID), gen.ListVariantsRequestObject{Id: product.Id})
	if err != nil {
		t.Fatalf("ListVariants: %v", err)
	}
	items := listResp.(gen.ListVariants200JSONResponse).Items
	if len(items) != 1 || items[0].Id != variant.Id || items[0].Attributes["size"] != "XL" {
		t.Fatalf("items = %+v, want the same variant with size=XL", items)
	}
}

func TestUpdateVariant_attributesValidatedLikeCreate(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")
	seedAttribute(ctx, t, q, shopRow.ID, "size")

	product := mustCreateProduct(t, h, shopRow.ID, unit.ID, "Cotton Shirt", "125000.00")
	created, err := h.CreateVariant(owner(shopRow.ID), gen.CreateVariantRequestObject{
		Id: product.Id, Body: &gen.CreateVariantJSONRequestBody{Attributes: gen.AttributeValues{"size": "L"}},
	})
	if err != nil {
		t.Fatalf("CreateVariant: %v", err)
	}
	variant := created.(gen.CreateVariant201JSONResponse)

	invalidAttrs := gen.AttributeValues{"color": "blue"} // "color" is not a known attribute code
	_, err = h.UpdateVariant(owner(shopRow.ID), gen.UpdateVariantRequestObject{
		Id: variant.Id, Body: &gen.UpdateVariantJSONRequestBody{Attributes: &invalidAttrs},
	})
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("err = %#v, want 400 VALIDATION_FAILED", err)
	}
	fields := apiErr.Details["fields"].(map[string]string)
	if fields["attributes"] != "invalid" {
		t.Fatalf("fields = %+v, want attributes=invalid", fields)
	}
}

func TestUpdateVariant_attributesConflictOnDuplicateCombination(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")
	seedAttribute(ctx, t, q, shopRow.ID, "size")

	product := mustCreateProduct(t, h, shopRow.ID, unit.ID, "Cotton Shirt", "125000.00")
	if _, err := h.CreateVariant(owner(shopRow.ID), gen.CreateVariantRequestObject{
		Id: product.Id, Body: &gen.CreateVariantJSONRequestBody{Attributes: gen.AttributeValues{"size": "L"}},
	}); err != nil {
		t.Fatalf("CreateVariant (L): %v", err)
	}
	createdM, err := h.CreateVariant(owner(shopRow.ID), gen.CreateVariantRequestObject{
		Id: product.Id, Body: &gen.CreateVariantJSONRequestBody{Attributes: gen.AttributeValues{"size": "M"}},
	})
	if err != nil {
		t.Fatalf("CreateVariant (M): %v", err)
	}
	variantM := createdM.(gen.CreateVariant201JSONResponse)

	dupAttrs := gen.AttributeValues{"size": "L"}
	_, err = h.UpdateVariant(owner(shopRow.ID), gen.UpdateVariantRequestObject{
		Id: variantM.Id, Body: &gen.UpdateVariantJSONRequestBody{Attributes: &dupAttrs},
	})
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Code != gen.CONFLICT || apiErr.Details["field"] != "attributes" {
		t.Fatalf("err = %#v, want 409 CONFLICT field=attributes", err)
	}
}
