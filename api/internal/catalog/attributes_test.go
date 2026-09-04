package catalog_test

import (
	"context"
	"testing"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
)

func TestCreateAttributeDefinition_codeFormatAndConflict(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")

	_, err := h.CreateAttributeDefinition(owner(shopRow.ID), gen.CreateAttributeDefinitionRequestObject{
		Body: &gen.CreateAttributeDefinitionJSONRequestBody{Code: "Size!", Translations: uzTranslations("Razmer")},
	})
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("err = %#v, want 400 VALIDATION_FAILED", err)
	}
	fields := apiErr.Details["fields"].(map[string]string)
	if fields["code"] != "invalid" {
		t.Fatalf("fields = %+v, want code=invalid", fields)
	}

	resp, err := h.CreateAttributeDefinition(owner(shopRow.ID), gen.CreateAttributeDefinitionRequestObject{
		Body: &gen.CreateAttributeDefinitionJSONRequestBody{Code: "size", Translations: uzTranslations("Razmer")},
	})
	if err != nil {
		t.Fatalf("CreateAttributeDefinition: %v", err)
	}
	created := resp.(gen.CreateAttributeDefinition201JSONResponse)
	if created.Code != "size" || created.Name != "Razmer" {
		t.Fatalf("created = %+v", created)
	}

	_, err = h.CreateAttributeDefinition(owner(shopRow.ID), gen.CreateAttributeDefinitionRequestObject{
		Body: &gen.CreateAttributeDefinitionJSONRequestBody{Code: "size", Translations: uzTranslations("Boshqa")},
	})
	apiErr, ok = err.(*apierr.Error)
	if !ok || apiErr.Code != gen.CONFLICT || apiErr.Details["field"] != "code" {
		t.Fatalf("err = %#v, want 409 CONFLICT field=code", err)
	}
}

func TestUpdateAttributeDefinition_patchesSortOrderAndTranslations(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")

	resp, err := h.CreateAttributeDefinition(owner(shopRow.ID), gen.CreateAttributeDefinitionRequestObject{
		Body: &gen.CreateAttributeDefinitionJSONRequestBody{Code: "color", Translations: uzTranslations("Rang")},
	})
	if err != nil {
		t.Fatalf("CreateAttributeDefinition: %v", err)
	}
	attr := gen.AttributeDefinition(resp.(gen.CreateAttributeDefinition201JSONResponse))

	newSort := 5
	updateResp, err := h.UpdateAttributeDefinition(owner(shopRow.ID), gen.UpdateAttributeDefinitionRequestObject{
		Id: attr.Id,
		Body: &gen.UpdateAttributeDefinitionJSONRequestBody{
			SortOrder:    &newSort,
			Translations: &gen.Translations{Ru: &gen.TranslationEntry{Name: "Цвет"}},
		},
	})
	if err != nil {
		t.Fatalf("UpdateAttributeDefinition: %v", err)
	}
	updated := gen.AttributeDefinition(updateResp.(gen.UpdateAttributeDefinition200JSONResponse))
	if updated.SortOrder != 5 {
		t.Fatalf("SortOrder = %d, want 5", updated.SortOrder)
	}
	if updated.Translations == nil || updated.Translations.Uz == nil || updated.Translations.Uz.Name != "Rang" {
		t.Fatalf("Translations.Uz = %+v, want the original uz entry preserved", updated.Translations)
	}
	if updated.Translations.Ru == nil || updated.Translations.Ru.Name != "Цвет" {
		t.Fatalf("Translations.Ru = %+v, want the newly patched ru entry", updated.Translations.Ru)
	}
}
