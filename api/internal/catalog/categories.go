package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/oapi-codegen/nullable"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// maxCategoryDepth is docs/04-DATA-MODEL.md § 2's "Depth ≤ 3 enforced in
// service" — a root category is depth 1.
const maxCategoryDepth = 3

// categorySlugRetries is how many auto-generated slug candidates
// CreateCategory tries before giving up with a 409 (base, base-2, base-3).
const categorySlugRetries = 3

// toGenCategory maps a resolved category row onto the API schema.
// description and translations are always absent: no query in this
// codebase currently selects category_translations.description (only
// name is selected for the locale-fallback lookup), and there is no
// ListCategoryTranslations equivalent to ListProductTranslations to
// assemble a full per-locale map from — see this task's final report.
func toGenCategory(id uuid.UUID, parentID *uuid.UUID, slug string, sortOrder int32, isActive bool, imageID *uuid.UUID, name, localeUsed, requested string) gen.Category {
	return gen.Category{
		Id:                  id,
		ParentId:            nullableUUID(parentID),
		Slug:                slug,
		SortOrder:           int(sortOrder),
		IsActive:            isActive,
		ImageId:             nullableUUID(imageID),
		Name:                name,
		Description:         nullable.NewNullNullable[string](),
		Locale:              effectiveLocale(localeUsed, requested),
		TranslationFallback: translationFallback(localeUsed, requested),
	}
}

// ListCategories lists the shop's categories as a flat list. Any
// authenticated role; includeInactive is honoured only for a catalog.write
// caller (manager+) — other roles always see active categories only.
func (h *Handler) ListCategories(ctx context.Context, req gen.ListCategoriesRequestObject) (gen.ListCategoriesResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}

	includeInactive := req.Params.IncludeInactive != nil && *req.Params.IncludeInactive &&
		auth.Require(ctx, auth.PermCatalogWrite) == nil

	locale := h.svc.resolveLocale(ctx)
	rows, err := h.svc.q.ListCategories(ctx, db.ListCategoriesParams{
		Locale: locale, ShopID: authCtx.ShopID, IncludeInactive: includeInactive,
	})
	if err != nil {
		return nil, fmt.Errorf("catalog: list categories: %w", err)
	}

	items := make([]gen.Category, len(rows))
	for i, r := range rows {
		items[i] = toGenCategory(r.ID, r.ParentID, r.Slug, r.SortOrder, r.IsActive, r.ImageID, r.Name, r.LocaleUsed, locale)
	}
	return gen.ListCategories200JSONResponse(gen.CategoryList{Items: items}), nil
}

// GetCategory gets a category by id. Any authenticated role; 404 for
// another shop's id or a soft-deleted category.
func (h *Handler) GetCategory(ctx context.Context, req gen.GetCategoryRequestObject) (gen.GetCategoryResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}

	locale := h.svc.resolveLocale(ctx)
	row, err := h.svc.q.GetCategory(ctx, db.GetCategoryParams{Locale: locale, ShopID: authCtx.ShopID, ID: req.Id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("category")
		}
		return nil, fmt.Errorf("catalog: get category: %w", err)
	}
	return gen.GetCategory200JSONResponse(toGenCategory(row.ID, row.ParentID, row.Slug, row.SortOrder, row.IsActive, row.ImageID, row.Name, row.LocaleUsed, locale)), nil
}

// categoryDepthOK reports whether parentID (in shopID) exists and placing
// a category under it keeps depth <= maxCategoryDepth.
func (s *Service) categoryDepthOK(ctx context.Context, shopID, parentID uuid.UUID) (bool, error) {
	depth, err := s.q.GetCategoryDepth(ctx, db.GetCategoryDepthParams{CategoryID: parentID, ShopID: shopID})
	if err != nil {
		return false, err
	}
	if depth == 0 {
		// GetCategoryDepth's COALESCE(max(...), 0): 0 means parentID was
		// not found in this shop (every real category has depth >= 1).
		return false, nil
	}
	return depth+1 <= maxCategoryDepth, nil
}

// isDescendantOrSelf reports whether candidate is ancestorID itself or a
// descendant of it is not what this checks — it walks UP from candidate
// via parent_id looking for selfID, i.e. "would making selfID's category a
// child of candidate create a cycle" (selfID appears in candidate's own
// ancestor chain, meaning selfID is candidate's ancestor already). Bounded
// to maxCategoryDepth+1 hops: a valid tree can never need more, and a
// bound means a corrupted chain can never spin forever.
func (s *Service) createsCycle(ctx context.Context, shopID, selfID, candidateParentID uuid.UUID) (bool, error) {
	current := candidateParentID
	for range maxCategoryDepth + 1 {
		if current == selfID {
			return true, nil
		}
		row, err := s.q.GetCategory(ctx, db.GetCategoryParams{Locale: s.defaultLocale, ShopID: shopID, ID: current})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			}
			return false, err
		}
		if row.ParentID == nil {
			return false, nil
		}
		current = *row.ParentID
	}
	return false, nil
}

// CreateCategory creates a category. Requires catalog.write (manager+).
func (h *Handler) CreateCategory(ctx context.Context, req gen.CreateCategoryRequestObject) (gen.CreateCategoryResponseObject, error) {
	if _, ok := auth.FromContext(ctx); !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermCatalogWrite); err != nil {
		return nil, err
	}
	authCtx, _ := auth.FromContext(ctx)
	body := req.Body

	fields := map[string]string{}

	entries := translationsToMap(body.Translations)
	if !validateTranslationNames(entries) {
		fields["translations"] = "invalid"
	} else if !hasNonEmptyName(entries, h.svc.defaultLocale) {
		fields["translations"] = "required"
	}

	if body.ParentId != nil {
		ok, err := h.svc.categoryDepthOK(ctx, authCtx.ShopID, *body.ParentId)
		if err != nil {
			return nil, fmt.Errorf("catalog: check category depth: %w", err)
		}
		if !ok {
			fields["parentId"] = "invalid"
		}
	}

	if body.ImageId != nil {
		if _, err := h.svc.q.GetMediaFile(ctx, db.GetMediaFileParams{ShopID: authCtx.ShopID, ID: *body.ImageId}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				fields["imageId"] = "invalid"
			} else {
				return nil, fmt.Errorf("catalog: get media file: %w", err)
			}
		}
	}

	var candidates []string
	if body.Slug != nil {
		if !validSlug(*body.Slug) {
			fields["slug"] = "invalid"
		}
		candidates = []string{*body.Slug}
	} else {
		candidates = slugCandidates(entries[h.svc.defaultLocale].Name, categorySlugRetries)
	}

	var sortOrder int32
	if body.SortOrder != nil {
		sortOrder = int32Field("sortOrder", *body.SortOrder, fields)
	}

	if len(fields) > 0 {
		return nil, apierr.Validation(fields)
	}

	isActive := true
	if body.IsActive != nil {
		isActive = *body.IsActive
	}

	// Each candidate slug is tried in its own transaction (category insert
	// + its translations, one multi-write unit): a unique-violation aborts
	// that transaction outright, so retrying with the next candidate needs
	// a fresh one, not a continuation of the failed statement.
	var created db.Category
	found := false
	for i, slug := range candidates {
		tx, err := h.svc.pool.Begin(ctx)
		if err != nil {
			return nil, fmt.Errorf("catalog: begin tx: %w", err)
		}
		qtx := h.svc.q.WithTx(tx)

		c, err := qtx.CreateCategory(ctx, db.CreateCategoryParams{
			ID: newID(), ShopID: authCtx.ShopID, ParentID: body.ParentId, Slug: slug,
			SortOrder: sortOrder, IsActive: isActive, ImageID: body.ImageId,
		})
		if err != nil {
			_ = tx.Rollback(ctx)
			field, ok := conflictField(err)
			if !ok || field != "slug" {
				if fkField, fkOK := invalidFKField(err); fkOK {
					return nil, apierr.Validation(map[string]string{fkField: "invalid"})
				}
				return nil, fmt.Errorf("catalog: create category: %w", err)
			}
			if body.Slug != nil || i == len(candidates)-1 {
				return nil, apierr.Conflict("slug")
			}
			continue // auto-generated slug collided: retry with the next candidate
		}

		translationErr := error(nil)
		for locale, e := range entries {
			if err := qtx.UpsertCategoryTranslation(ctx, db.UpsertCategoryTranslationParams{
				CategoryID: c.ID, Locale: locale, Name: e.Name, Description: e.Description,
			}); err != nil {
				translationErr = err
				break
			}
		}
		if translationErr != nil {
			_ = tx.Rollback(ctx)
			return nil, fmt.Errorf("catalog: upsert category translation: %w", translationErr)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("catalog: commit create category: %w", err)
		}
		created, found = c, true
		break
	}
	if !found {
		return nil, apierr.Conflict("slug")
	}

	locale := h.svc.resolveLocale(ctx)
	name, localeUsed := resolveDisplay(entries, locale, h.svc.defaultLocale)
	resp := toGenCategory(created.ID, created.ParentID, created.Slug, created.SortOrder, created.IsActive, created.ImageID, name, localeUsed, locale)
	return gen.CreateCategory201JSONResponse(resp), nil
}

// UpdateCategory updates a category. Requires catalog.write (manager+).
//
// KNOWN GAP (see this task's final report): CategoryPatch documents
// explicit `null` for parentId/imageId as clearing the field, but
// UpdateCategoryParams (api/internal/db/categories.sql.go) has no clear
// flag for either column — only COALESCE-style "set or leave unchanged".
// An explicit `null` for either field is rejected as 400 invalid rather
// than silently doing nothing or being mis-applied.
func (h *Handler) UpdateCategory(ctx context.Context, req gen.UpdateCategoryRequestObject) (gen.UpdateCategoryResponseObject, error) {
	if _, ok := auth.FromContext(ctx); !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermCatalogWrite); err != nil {
		return nil, err
	}
	authCtx, _ := auth.FromContext(ctx)
	body := req.Body

	fields := map[string]string{}

	var parentID *uuid.UUID
	if pp := optionalUUID(body.ParentId); pp != nil {
		if *pp == nil {
			fields["parentId"] = "invalid" // explicit null: unsupported, see doc comment above
		} else {
			ok, err := h.svc.categoryDepthOK(ctx, authCtx.ShopID, **pp)
			if err != nil {
				return nil, fmt.Errorf("catalog: check category depth: %w", err)
			}
			if !ok {
				fields["parentId"] = "invalid"
			} else {
				cycle, err := h.svc.createsCycle(ctx, authCtx.ShopID, req.Id, **pp)
				if err != nil {
					return nil, fmt.Errorf("catalog: check category cycle: %w", err)
				}
				if cycle {
					fields["parentId"] = "invalid"
				} else {
					parentID = *pp
				}
			}
		}
	}

	var imageID *uuid.UUID
	if ip := optionalUUID(body.ImageId); ip != nil {
		if *ip == nil {
			fields["imageId"] = "invalid" // explicit null: unsupported, see doc comment above
		} else if _, err := h.svc.q.GetMediaFile(ctx, db.GetMediaFileParams{ShopID: authCtx.ShopID, ID: **ip}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				fields["imageId"] = "invalid"
			} else {
				return nil, fmt.Errorf("catalog: get media file: %w", err)
			}
		} else {
			imageID = *ip
		}
	}

	if body.Slug != nil && !validSlug(*body.Slug) {
		fields["slug"] = "invalid"
	}

	var patchEntries map[string]translationEntry
	if body.Translations != nil {
		patchEntries = translationsToMap(*body.Translations)
		if !validateTranslationNames(patchEntries) {
			fields["translations"] = "invalid"
		}
	}

	var sortOrder *int32
	if body.SortOrder != nil {
		v := int32Field("sortOrder", *body.SortOrder, fields)
		sortOrder = &v
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

	updated, err := qtx.UpdateCategory(ctx, db.UpdateCategoryParams{
		ParentID: parentID, Slug: body.Slug, SortOrder: sortOrder, IsActive: body.IsActive,
		ImageID: imageID, ShopID: authCtx.ShopID, ID: req.Id,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("category")
		}
		if field, ok := conflictField(err); ok {
			return nil, apierr.Conflict(field)
		}
		if fkField, ok := invalidFKField(err); ok {
			return nil, apierr.Validation(map[string]string{fkField: "invalid"})
		}
		return nil, fmt.Errorf("catalog: update category: %w", err)
	}

	for locale, e := range patchEntries {
		if err := qtx.UpsertCategoryTranslation(ctx, db.UpsertCategoryTranslationParams{
			CategoryID: updated.ID, Locale: locale, Name: e.Name, Description: e.Description,
		}); err != nil {
			return nil, fmt.Errorf("catalog: upsert category translation: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("catalog: commit update category: %w", err)
	}

	locale := h.svc.resolveLocale(ctx)
	fresh, err := h.svc.q.GetCategory(ctx, db.GetCategoryParams{Locale: locale, ShopID: authCtx.ShopID, ID: updated.ID})
	if err != nil {
		return nil, fmt.Errorf("catalog: get category after update: %w", err)
	}
	return gen.UpdateCategory200JSONResponse(toGenCategory(fresh.ID, fresh.ParentID, fresh.Slug, fresh.SortOrder, fresh.IsActive, fresh.ImageID, fresh.Name, fresh.LocaleUsed, locale)), nil
}

// DeleteCategory soft-deletes a category. Requires catalog.write
// (manager+). 409 CONFLICT details.field: products when products still
// reference this category (O-14); 400 fields.id: invalid when it has
// active child categories.
func (h *Handler) DeleteCategory(ctx context.Context, req gen.DeleteCategoryRequestObject) (gen.DeleteCategoryResponseObject, error) {
	if _, ok := auth.FromContext(ctx); !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermCatalogWrite); err != nil {
		return nil, err
	}
	authCtx, _ := auth.FromContext(ctx)

	if _, err := h.svc.q.GetCategory(ctx, db.GetCategoryParams{Locale: h.svc.defaultLocale, ShopID: authCtx.ShopID, ID: req.Id}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("category")
		}
		return nil, fmt.Errorf("catalog: get category: %w", err)
	}

	count, err := h.svc.q.CountProductsInCategory(ctx, db.CountProductsInCategoryParams{ShopID: authCtx.ShopID, CategoryID: &req.Id})
	if err != nil {
		return nil, fmt.Errorf("catalog: count products in category: %w", err)
	}
	if count > 0 {
		return nil, apierr.Conflict("products")
	}

	children, err := h.svc.q.ListCategories(ctx, db.ListCategoriesParams{Locale: h.svc.defaultLocale, ShopID: authCtx.ShopID, IncludeInactive: true})
	if err != nil {
		return nil, fmt.Errorf("catalog: list categories: %w", err)
	}
	for _, c := range children {
		if c.ParentID != nil && *c.ParentID == req.Id && c.IsActive {
			return nil, apierr.Validation(map[string]string{"id": "invalid"})
		}
	}

	if err := h.svc.q.SoftDeleteCategory(ctx, db.SoftDeleteCategoryParams{ShopID: authCtx.ShopID, ID: req.Id}); err != nil {
		return nil, fmt.Errorf("catalog: soft delete category: %w", err)
	}
	return gen.DeleteCategory204Response{}, nil
}
