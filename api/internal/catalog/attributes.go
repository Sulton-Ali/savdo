package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// attributeCodeRE is the shape an attribute definition's `code` must have:
// a stable key `product_variants.attributes` values are keyed by (e.g.
// `size`, `color`), per the task spec.
var attributeCodeRE = regexp.MustCompile(`^[a-z][a-z0-9_]{1,31}$`)

// attributeEntriesFromAgg parses ListAttributeDefinitionsRow.Translations
// — a `{locale: name}` JSON object built by `jsonb_object_agg` in SQL —
// into the same translationEntry map shape the rest of this package uses.
// attribute_definition_translations has no description column, so every
// entry's Description is nil.
func attributeEntriesFromAgg(raw json.RawMessage) map[string]translationEntry {
	var names map[string]string
	if err := json.Unmarshal(raw, &names); err != nil || names == nil {
		return map[string]translationEntry{}
	}
	out := make(map[string]translationEntry, len(names))
	for locale, name := range names {
		if supportedLocales[locale] {
			out[locale] = translationEntry{Name: name}
		}
	}
	return out
}

// resolveDisplay picks the (name, localeUsed) pair entries reports for
// requested, mirroring the SQL locale-fallback order (requested -> 'uz' ->
// any) so an in-memory reconstruction right after a write agrees with
// what a subsequent read would compute.
func resolveDisplay(entries map[string]translationEntry, requested, fallback string) (name, localeUsed string) {
	if e, ok := entries[requested]; ok {
		return e.Name, requested
	}
	if e, ok := entries[fallback]; ok {
		return e.Name, fallback
	}
	for loc, e := range entries {
		return e.Name, loc
	}
	return "", ""
}

func toGenAttributeDefinition(id uuid.UUID, code string, sortOrder int32, entries map[string]translationEntry, requested, fallback string) gen.AttributeDefinition {
	name, localeUsed := resolveDisplay(entries, requested, fallback)
	return gen.AttributeDefinition{
		Id:                  id,
		Code:                code,
		SortOrder:           int(sortOrder),
		Name:                name,
		Locale:              effectiveLocale(localeUsed, requested),
		TranslationFallback: translationFallback(localeUsed, requested),
		Translations:        buildTranslations(entries),
	}
}

// ListAttributeDefinitions lists the shop's attribute definitions. Any
// authenticated role; not cursor-paginated (a handful of entries, D-32).
func (h *Handler) ListAttributeDefinitions(ctx context.Context, _ gen.ListAttributeDefinitionsRequestObject) (gen.ListAttributeDefinitionsResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}

	locale := h.svc.resolveLocale(ctx)
	rows, err := h.svc.q.ListAttributeDefinitions(ctx, db.ListAttributeDefinitionsParams{Locale: locale, ShopID: authCtx.ShopID})
	if err != nil {
		return nil, fmt.Errorf("catalog: list attribute definitions: %w", err)
	}

	items := make([]gen.AttributeDefinition, len(rows))
	for i, r := range rows {
		items[i] = toGenAttributeDefinition(r.ID, r.Code, r.SortOrder, attributeEntriesFromAgg(r.Translations), locale, h.svc.defaultLocale)
	}
	return gen.ListAttributeDefinitions200JSONResponse(gen.AttributeDefinitionList{Items: items}), nil
}

// CreateAttributeDefinition creates an attribute definition. Requires
// catalog.write (manager+).
func (h *Handler) CreateAttributeDefinition(ctx context.Context, req gen.CreateAttributeDefinitionRequestObject) (gen.CreateAttributeDefinitionResponseObject, error) {
	if _, ok := auth.FromContext(ctx); !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermCatalogWrite); err != nil {
		return nil, err
	}
	authCtx, _ := auth.FromContext(ctx)

	body := req.Body
	fields := map[string]string{}

	if !attributeCodeRE.MatchString(body.Code) {
		fields["code"] = "invalid"
	}

	entries := translationsToMap(body.Translations)
	if !validateTranslationNames(entries) {
		fields["translations"] = "invalid"
	} else if !hasNonEmptyName(entries, h.svc.defaultLocale) {
		fields["translations"] = "required"
	}

	var sortOrder int32
	if body.SortOrder != nil {
		sortOrder = int32Field("sortOrder", *body.SortOrder, fields)
	}

	if len(fields) > 0 {
		return nil, apierr.Validation(fields)
	}

	tx, err := h.svc.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("catalog: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := h.svc.q.WithTx(tx)

	created, err := qtx.CreateAttributeDefinition(ctx, db.CreateAttributeDefinitionParams{
		ID: newID(), ShopID: authCtx.ShopID, Code: body.Code, SortOrder: sortOrder,
	})
	if err != nil {
		if field, ok := conflictField(err); ok {
			return nil, apierr.Conflict(field)
		}
		return nil, fmt.Errorf("catalog: create attribute definition: %w", err)
	}

	for locale, e := range entries {
		if err := qtx.UpsertAttributeDefinitionTranslation(ctx, db.UpsertAttributeDefinitionTranslationParams{
			AttributeDefinitionID: created.ID, Locale: locale, Name: e.Name,
		}); err != nil {
			return nil, fmt.Errorf("catalog: upsert attribute definition translation: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("catalog: commit create attribute definition: %w", err)
	}

	locale := h.svc.resolveLocale(ctx)
	resp := toGenAttributeDefinition(created.ID, created.Code, created.SortOrder, entries, locale, h.svc.defaultLocale)
	return gen.CreateAttributeDefinition201JSONResponse(resp), nil
}

// UpdateAttributeDefinition updates an attribute definition's sortOrder
// and/or translations. Requires catalog.write (manager+). `code` is not
// patchable.
func (h *Handler) UpdateAttributeDefinition(ctx context.Context, req gen.UpdateAttributeDefinitionRequestObject) (gen.UpdateAttributeDefinitionResponseObject, error) {
	if _, ok := auth.FromContext(ctx); !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermCatalogWrite); err != nil {
		return nil, err
	}
	authCtx, _ := auth.FromContext(ctx)

	// The aggregated translations column does not depend on the locale
	// parameter, only the resolved name/localeUsed columns do — any
	// locale value works to fetch the current row and its full
	// translations map.
	current, ok, err := h.svc.findAttributeDefinition(ctx, authCtx.ShopID, req.Id, h.svc.defaultLocale)
	if err != nil {
		return nil, fmt.Errorf("catalog: find attribute definition: %w", err)
	}
	if !ok {
		return nil, apierr.NotFound("attributeDefinition")
	}

	body := req.Body
	entries := attributeEntriesFromAgg(current.Translations)

	if body.Translations != nil {
		patch := translationsToMap(*body.Translations)
		if !validateTranslationNames(patch) {
			return nil, apierr.Validation(map[string]string{"translations": "invalid"})
		}
		for locale, e := range patch {
			entries[locale] = e
		}
	}

	var sortOrder *int32
	if body.SortOrder != nil {
		fields := map[string]string{}
		v := int32Field("sortOrder", *body.SortOrder, fields)
		if len(fields) > 0 {
			return nil, apierr.Validation(fields)
		}
		sortOrder = &v
	}

	tx, err := h.svc.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("catalog: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := h.svc.q.WithTx(tx)

	updated, err := qtx.UpdateAttributeDefinition(ctx, db.UpdateAttributeDefinitionParams{
		SortOrder: sortOrder, ShopID: authCtx.ShopID, ID: req.Id,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("attributeDefinition")
		}
		return nil, fmt.Errorf("catalog: update attribute definition: %w", err)
	}

	if body.Translations != nil {
		patch := translationsToMap(*body.Translations)
		for locale, e := range patch {
			if err := qtx.UpsertAttributeDefinitionTranslation(ctx, db.UpsertAttributeDefinitionTranslationParams{
				AttributeDefinitionID: updated.ID, Locale: locale, Name: e.Name,
			}); err != nil {
				return nil, fmt.Errorf("catalog: upsert attribute definition translation: %w", err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("catalog: commit update attribute definition: %w", err)
	}

	locale := h.svc.resolveLocale(ctx)
	resp := toGenAttributeDefinition(updated.ID, updated.Code, updated.SortOrder, entries, locale, h.svc.defaultLocale)
	return gen.UpdateAttributeDefinition200JSONResponse(resp), nil
}

// findAttributeDefinition looks up one attribute definition by id within
// shopID, reusing ListAttributeDefinitions (there is no per-id query that
// also returns the aggregated translations map) filtered to req.Id.
func (s *Service) findAttributeDefinition(ctx context.Context, shopID, id uuid.UUID, locale string) (db.ListAttributeDefinitionsRow, bool, error) {
	rows, err := s.q.ListAttributeDefinitions(ctx, db.ListAttributeDefinitionsParams{Locale: locale, ShopID: shopID})
	if err != nil {
		return db.ListAttributeDefinitionsRow{}, false, err
	}
	for _, r := range rows {
		if r.ID == id {
			return r, true, nil
		}
	}
	return db.ListAttributeDefinitionsRow{}, false, nil
}

// attributeDefinitionCodes returns the shop's attribute definition codes,
// for validating a variant's `attributes` keys (catalog/variants.go).
func (s *Service) attributeDefinitionCodes(ctx context.Context, shopID uuid.UUID) (map[string]bool, error) {
	rows, err := s.q.ListAttributeDefinitions(ctx, db.ListAttributeDefinitionsParams{Locale: s.defaultLocale, ShopID: shopID})
	if err != nil {
		return nil, err
	}
	codes := make(map[string]bool, len(rows))
	for _, r := range rows {
		codes[r.Code] = true
	}
	return codes, nil
}
