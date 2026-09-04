package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oapi-codegen/nullable"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/money"
)

// minSearchLength is the `?q=` minimum before it is honoured — docs/05-
// API.md § Conventions / the task spec: "trimmed, min 2 chars else
// ignored".
const minSearchLength = 2

// productSlugRetries mirrors categorySlugRetries for CreateProduct's
// auto-generated slug.
const productSlugRetries = 3

// productCommon normalizes the fields shared by every "one product" row
// shape sqlc generates (Get/List x For{Staff,Cashier}, plus the plain
// db.Product CreateProduct/UpdateProduct return).
type productCommon struct {
	ID         uuid.UUID
	CategoryID *uuid.UUID
	UnitID     uuid.UUID
	Slug       string
	Sku        *string
	BasePrice  pgtype.Numeric
	PromoPrice pgtype.Numeric
	PromoFrom  *time.Time
	PromoTo    *time.Time
	IsActive   bool
	IsFeatured bool
	Name       string
	LocaleUsed string
}

func commonFromStaffRow(r db.GetProductForStaffRow) productCommon {
	return productCommon{
		ID: r.ID, CategoryID: r.CategoryID, UnitID: r.UnitID, Slug: r.Slug, Sku: r.Sku,
		BasePrice: r.BasePrice, PromoPrice: r.PromoPrice, PromoFrom: r.PromoFrom, PromoTo: r.PromoTo,
		IsActive: r.IsActive, IsFeatured: r.IsFeatured, Name: r.Name, LocaleUsed: r.LocaleUsed,
	}
}

func commonFromCashierRow(r db.GetProductForCashierRow) productCommon {
	return productCommon{
		ID: r.ID, CategoryID: r.CategoryID, UnitID: r.UnitID, Slug: r.Slug, Sku: r.Sku,
		BasePrice: r.BasePrice, PromoPrice: r.PromoPrice, PromoFrom: r.PromoFrom, PromoTo: r.PromoTo,
		IsActive: r.IsActive, IsFeatured: r.IsFeatured, Name: r.Name, LocaleUsed: r.LocaleUsed,
	}
}

func commonFromListStaffRow(r db.ListProductsForStaffRow) productCommon {
	return productCommon{
		ID: r.ID, CategoryID: r.CategoryID, UnitID: r.UnitID, Slug: r.Slug, Sku: r.Sku,
		BasePrice: r.BasePrice, PromoPrice: r.PromoPrice, PromoFrom: r.PromoFrom, PromoTo: r.PromoTo,
		IsActive: r.IsActive, IsFeatured: r.IsFeatured, Name: r.Name, LocaleUsed: r.LocaleUsed,
	}
}

func commonFromListCashierRow(r db.ListProductsForCashierRow) productCommon {
	return productCommon{
		ID: r.ID, CategoryID: r.CategoryID, UnitID: r.UnitID, Slug: r.Slug, Sku: r.Sku,
		BasePrice: r.BasePrice, PromoPrice: r.PromoPrice, PromoFrom: r.PromoFrom, PromoTo: r.PromoTo,
		IsActive: r.IsActive, IsFeatured: r.IsFeatured, Name: r.Name, LocaleUsed: r.LocaleUsed,
	}
}

// toGenProductBase maps pc onto gen.Product, excluding costPrice,
// translations, variants and images — the handler adds those, since which
// of them apply (role, single-item vs. list) differs per endpoint.
// description is left null: no product query in this codebase selects
// product_translations.description for the resolved locale (see this
// task's final report) except ListProductTranslations, which
// buildFullProduct uses for single-item responses.
func toGenProductBase(pc productCommon, requested string) (gen.Product, error) {
	basePrice, err := money.FromNumeric(pc.BasePrice)
	if err != nil {
		return gen.Product{}, fmt.Errorf("catalog: product base price: %w", err)
	}

	g := gen.Product{
		Id:                  pc.ID,
		CategoryId:          nullableUUID(pc.CategoryID),
		Slug:                pc.Slug,
		Sku:                 nullableString(pc.Sku),
		UnitId:              pc.UnitID,
		BasePrice:           money.String(basePrice),
		Description:         nullable.NewNullNullable[string](),
		IsActive:            pc.IsActive,
		IsFeatured:          pc.IsFeatured,
		Name:                pc.Name,
		Locale:              effectiveLocale(pc.LocaleUsed, requested),
		TranslationFallback: translationFallback(pc.LocaleUsed, requested),
		PromoFrom:           nullableTime(pc.PromoFrom),
		PromoTo:             nullableTime(pc.PromoTo),
	}

	if pc.PromoPrice.Valid {
		d, err := money.FromNumeric(pc.PromoPrice)
		if err != nil {
			return gen.Product{}, fmt.Errorf("catalog: product promo price: %w", err)
		}
		g.PromoPrice = nullable.NewNullableWithValue(money.String(d))
	} else {
		g.PromoPrice = nullable.NewNullNullable[string]()
	}

	return g, nil
}

// buildFullProduct assembles the complete response for a single-item
// endpoint (Get/Create/Update): base fields, the resolved description (via
// ListProductTranslations — the only query that selects it), costPrice
// when costPrice is non-nil and Valid, the full translations map when
// includeTranslations, and variants/images when includeVariantsImages.
func (s *Service) buildFullProduct(ctx context.Context, shopID uuid.UUID, pc productCommon, costPrice *pgtype.Numeric, requested string, includeTranslations, includeVariantsImages bool) (gen.Product, error) {
	g, err := toGenProductBase(pc, requested)
	if err != nil {
		return gen.Product{}, err
	}

	entries, err := s.productTranslationEntries(ctx, pc.ID)
	if err != nil {
		return gen.Product{}, fmt.Errorf("catalog: list product translations: %w", err)
	}
	if e, ok := entries[pc.LocaleUsed]; ok {
		g.Description = nullableString(e.Description)
	}
	if includeTranslations {
		g.Translations = buildTranslations(entries)
	}

	if costPrice != nil && costPrice.Valid {
		d, err := money.FromNumeric(*costPrice)
		if err != nil {
			return gen.Product{}, fmt.Errorf("catalog: product cost price: %w", err)
		}
		v := money.String(d)
		g.CostPrice = &v
	}

	if includeVariantsImages {
		variants, err := s.variantsFor(ctx, shopID, pc.ID, costPrice != nil)
		if err != nil {
			return gen.Product{}, fmt.Errorf("catalog: list variants: %w", err)
		}
		g.Variants = &variants

		images, err := s.productImagesFor(ctx, shopID, pc.ID)
		if err != nil {
			return gen.Product{}, fmt.Errorf("catalog: list product images: %w", err)
		}
		g.Images = &images
	}

	return g, nil
}

// productTranslationEntries loads every locale's name/description for
// productID via ListProductTranslations — the only product query that
// selects description at all.
func (s *Service) productTranslationEntries(ctx context.Context, productID uuid.UUID) (map[string]translationEntry, error) {
	rows, err := s.q.ListProductTranslations(ctx, productID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]translationEntry, len(rows))
	for _, r := range rows {
		if supportedLocales[r.Locale] {
			out[r.Locale] = translationEntry{Name: r.Name, Description: r.Description}
		}
	}
	return out, nil
}

// ListProducts lists products. Any authenticated role; `q` (trimmed, >= 2
// chars) searches by name; includeInactive is staff-only (catalog.write);
// costPrice/translations are present only for owner/manager.
// description is always null on list items (see buildFullProduct's doc
// comment) — populating it per row would need one extra
// ListProductTranslations query per item.
func (h *Handler) ListProducts(ctx context.Context, req gen.ListProductsRequestObject) (gen.ListProductsResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	p := permsFromContext(ctx)
	includeInactive := req.Params.IncludeInactive != nil && *req.Params.IncludeInactive && p.includeTranslations

	var q *string
	if req.Params.Q != nil {
		trimmed := strings.TrimSpace(*req.Params.Q)
		if utf8.RuneCountInString(trimmed) >= minSearchLength {
			q = &trimmed
		}
	}

	limit := clampLimit(req.Params.Limit)
	cursorCreatedAt0, cursorID0, err := decodeCursor(req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	cursorCreatedAt, cursorID := cursorPtr(cursorCreatedAt0, cursorID0)

	locale := h.svc.resolveLocale(ctx)
	items := []gen.Product{}
	var nextCursor *string

	if p.includeCost {
		rows, err := h.svc.q.ListProductsForStaff(ctx, db.ListProductsForStaffParams{
			Locale: locale, ShopID: authCtx.ShopID, IncludeInactive: includeInactive, CategoryID: req.Params.CategoryId,
			Q: q, CursorCreatedAt: cursorCreatedAt, CursorID: cursorID, Limit: limit + 1,
		})
		if err != nil {
			return nil, fmt.Errorf("catalog: list products for staff: %w", err)
		}
		page, nc := paginateT(rows, limit, func(r db.ListProductsForStaffRow) (time.Time, uuid.UUID) { return r.CreatedAt, r.ID })
		nextCursor = nc
		for _, r := range page {
			g, err := toGenProductBase(commonFromListStaffRow(r), locale)
			if err != nil {
				return nil, fmt.Errorf("catalog: %w", err)
			}
			if r.CostPrice.Valid {
				d, err := money.FromNumeric(r.CostPrice)
				if err != nil {
					return nil, fmt.Errorf("catalog: %w", err)
				}
				v := money.String(d)
				g.CostPrice = &v
			}
			items = append(items, g)
		}
	} else {
		rows, err := h.svc.q.ListProductsForCashier(ctx, db.ListProductsForCashierParams{
			Locale: locale, ShopID: authCtx.ShopID, IncludeInactive: includeInactive, CategoryID: req.Params.CategoryId,
			Q: q, CursorCreatedAt: cursorCreatedAt, CursorID: cursorID, Limit: limit + 1,
		})
		if err != nil {
			return nil, fmt.Errorf("catalog: list products for cashier: %w", err)
		}
		page, nc := paginateT(rows, limit, func(r db.ListProductsForCashierRow) (time.Time, uuid.UUID) { return r.CreatedAt, r.ID })
		nextCursor = nc
		for _, r := range page {
			g, err := toGenProductBase(commonFromListCashierRow(r), locale)
			if err != nil {
				return nil, fmt.Errorf("catalog: %w", err)
			}
			items = append(items, g)
		}
	}

	return gen.ListProducts200JSONResponse(gen.ProductList{Items: items, NextCursor: nullableString(nextCursor)}), nil
}

// GetProduct gets a product, including its variants and images.
// costPrice/translations are present only for owner/manager.
func (h *Handler) GetProduct(ctx context.Context, req gen.GetProductRequestObject) (gen.GetProductResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	p := permsFromContext(ctx)
	locale := h.svc.resolveLocale(ctx)

	if p.includeCost {
		row, err := h.svc.q.GetProductForStaff(ctx, db.GetProductForStaffParams{Locale: locale, ShopID: authCtx.ShopID, ID: req.Id})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, apierr.NotFound("product")
			}
			return nil, fmt.Errorf("catalog: get product for staff: %w", err)
		}
		g, err := h.svc.buildFullProduct(ctx, authCtx.ShopID, commonFromStaffRow(row), &row.CostPrice, locale, p.includeTranslations, true)
		if err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		return gen.GetProduct200JSONResponse(g), nil
	}

	row, err := h.svc.q.GetProductForCashier(ctx, db.GetProductForCashierParams{Locale: locale, ShopID: authCtx.ShopID, ID: req.Id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("product")
		}
		return nil, fmt.Errorf("catalog: get product for cashier: %w", err)
	}
	g, err := h.svc.buildFullProduct(ctx, authCtx.ShopID, commonFromCashierRow(row), nil, locale, false, true)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	return gen.GetProduct200JSONResponse(g), nil
}

// preparedVariant is a request's variant, already validated, ready to
// insert inside CreateProduct's transaction.
type preparedVariant struct {
	Sku, Barcode                *string
	Attributes                  json.RawMessage
	PriceOverride, CostOverride pgtype.Numeric
	IsActive                    bool
}

// prepareVariants validates body's variants (or synthesizes the one
// implicit `{}` variant when absent), returning the field errors
// (variants[i].<field>) for anything invalid.
func (h *Handler) prepareVariants(ctx context.Context, shopID uuid.UUID, body []gen.VariantCreate, fields map[string]string) []preparedVariant {
	variants := make([]preparedVariant, 0, len(body))
	for i, vc := range body {
		canonical, ok, err := h.svc.validateAttributes(ctx, shopID, vc.Attributes)
		if err != nil {
			fields[fmt.Sprintf("variants[%d].attributes", i)] = "invalid"
			continue
		}
		if !ok {
			fields[fmt.Sprintf("variants[%d].attributes", i)] = "invalid"
			continue
		}
		pv := preparedVariant{Sku: vc.Sku, Barcode: vc.Barcode, Attributes: canonical, IsActive: true}
		if vc.IsActive != nil {
			pv.IsActive = *vc.IsActive
		}
		if vc.PriceOverride != nil {
			d, apiErr := money.ParseAmount(*vc.PriceOverride)
			if apiErr != nil {
				fields[fmt.Sprintf("variants[%d].priceOverride", i)] = "invalid"
			} else {
				pv.PriceOverride = money.ToNumeric(d)
			}
		}
		if vc.CostOverride != nil {
			d, apiErr := money.ParseAmount(*vc.CostOverride)
			if apiErr != nil {
				fields[fmt.Sprintf("variants[%d].costOverride", i)] = "invalid"
			} else {
				pv.CostOverride = money.ToNumeric(d)
			}
		}
		variants = append(variants, pv)
	}
	return variants
}

// CreateProduct creates a product. Requires catalog.write (manager+).
// `variants` absent creates one implicit `{}` variant; present validates
// each and sets has_variants.
func (h *Handler) CreateProduct(ctx context.Context, req gen.CreateProductRequestObject) (gen.CreateProductResponseObject, error) {
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

	basePrice, apiErr := money.ParseAmount(body.BasePrice)
	if apiErr != nil {
		fields["basePrice"] = "invalid"
	}

	var costPrice pgtype.Numeric
	if body.CostPrice != nil {
		d, apiErr := money.ParseAmount(*body.CostPrice)
		if apiErr != nil {
			fields["costPrice"] = "invalid"
		} else {
			costPrice = money.ToNumeric(d)
		}
	}

	var promoPrice pgtype.Numeric
	if body.PromoPrice != nil {
		d, apiErr := money.ParseAmount(*body.PromoPrice)
		if apiErr != nil {
			fields["promoPrice"] = "invalid"
		} else {
			promoPrice = money.ToNumeric(d)
		}
	}
	if body.PromoFrom != nil && body.PromoTo != nil && body.PromoFrom.After(*body.PromoTo) {
		fields["promoTo"] = "invalid"
	}

	var candidates []string
	if body.Slug != nil {
		if !validSlug(*body.Slug) {
			fields["slug"] = "invalid"
		}
		candidates = []string{*body.Slug}
	} else {
		candidates = slugCandidates(entries[h.svc.defaultLocale].Name, productSlugRetries)
	}

	hasVariants := body.Variants != nil
	var variants []preparedVariant
	if hasVariants {
		variants = h.prepareVariants(ctx, authCtx.ShopID, *body.Variants, fields)
	} else {
		variants = []preparedVariant{{Attributes: json.RawMessage(implicitAttributes), IsActive: true}}
	}

	if len(fields) > 0 {
		return nil, apierr.Validation(fields)
	}

	isActive := true
	if body.IsActive != nil {
		isActive = *body.IsActive
	}
	var isFeatured bool
	if body.IsFeatured != nil {
		isFeatured = *body.IsFeatured
	}

	var created db.Product
	found := false
	for i, slug := range candidates {
		p, writeErr := h.createProductAttempt(ctx, authCtx.ShopID, slug, body, basePrice, costPrice, promoPrice, isActive, isFeatured, entries, variants, hasVariants)
		if writeErr == nil {
			created, found = p, true
			break
		}
		if field, ok := conflictField(writeErr); ok {
			if field == "slug" && body.Slug == nil && i < len(candidates)-1 {
				continue // auto-generated slug collided: retry with the next candidate
			}
			return nil, apierr.Conflict(field)
		}
		if fkField, ok := invalidFKField(writeErr); ok {
			return nil, apierr.Validation(map[string]string{fkField: "invalid"})
		}
		return nil, fmt.Errorf("catalog: create product: %w", writeErr)
	}
	if !found {
		return nil, apierr.Conflict("slug")
	}

	locale := h.svc.resolveLocale(ctx)
	name, localeUsed := resolveDisplay(entries, locale, h.svc.defaultLocale)
	pc := productCommon{
		ID: created.ID, CategoryID: created.CategoryID, UnitID: created.UnitID, Slug: created.Slug, Sku: created.Sku,
		BasePrice: created.BasePrice, PromoPrice: created.PromoPrice, PromoFrom: created.PromoFrom, PromoTo: created.PromoTo,
		IsActive: created.IsActive, IsFeatured: created.IsFeatured, Name: name, LocaleUsed: localeUsed,
	}
	var costPtr *pgtype.Numeric
	if created.CostPrice.Valid {
		c := created.CostPrice
		costPtr = &c
	}
	g, err := h.svc.buildFullProduct(ctx, authCtx.ShopID, pc, costPtr, locale, true, true)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	return gen.CreateProduct201JSONResponse(g), nil
}

// createProductAttempt runs one candidate slug's insert + translations +
// variants + has_variants flip as a single transaction, so a mid-way
// failure (a duplicate sku, an invalid unit/category id caught only by
// the FK) never leaves a half-written product behind.
func (h *Handler) createProductAttempt(ctx context.Context, shopID uuid.UUID, slug string, body *gen.ProductCreate, basePrice decimal.Decimal, costPrice, promoPrice pgtype.Numeric, isActive, isFeatured bool, entries map[string]translationEntry, variants []preparedVariant, hasVariants bool) (db.Product, error) {
	tx, err := h.svc.pool.Begin(ctx)
	if err != nil {
		return db.Product{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := h.svc.q.WithTx(tx)

	p, err := qtx.CreateProduct(ctx, db.CreateProductParams{
		ID: newID(), ShopID: shopID, CategoryID: body.CategoryId, UnitID: body.UnitId, Slug: slug, Sku: body.Sku,
		BasePrice: money.ToNumeric(basePrice), CostPrice: costPrice, PromoPrice: promoPrice,
		PromoFrom: body.PromoFrom, PromoTo: body.PromoTo, IsActive: isActive, IsFeatured: isFeatured,
	})
	if err != nil {
		return db.Product{}, err
	}

	for locale, e := range entries {
		if err := qtx.UpsertProductTranslation(ctx, db.UpsertProductTranslationParams{ProductID: p.ID, Locale: locale, Name: e.Name, Description: e.Description}); err != nil {
			return db.Product{}, err
		}
	}
	for _, v := range variants {
		if _, err := qtx.CreateVariant(ctx, db.CreateVariantParams{
			ID: newID(), ShopID: shopID, ProductID: p.ID, Sku: v.Sku, Barcode: v.Barcode,
			Attributes: v.Attributes, PriceOverride: v.PriceOverride, CostOverride: v.CostOverride, IsActive: v.IsActive,
		}); err != nil {
			return db.Product{}, err
		}
	}
	if hasVariants {
		if err := qtx.SetProductHasVariants(ctx, db.SetProductHasVariantsParams{ShopID: shopID, ID: p.ID, HasVariants: true}); err != nil {
			return db.Product{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return db.Product{}, fmt.Errorf("commit: %w", err)
	}
	return p, nil
}

// UpdateProduct updates a product. Requires catalog.write (manager+).
// `sku`/`promoPrice`/`promoFrom`/`promoTo`/`categoryId`/`costPrice` are
// nullable (D-35): explicit `null` clears the field. `promoPrice`,
// `promoFrom` and `promoTo` share one underlying clear flag
// (db.UpdateProductParams.ClearPromo) — clearing any one of them clears
// all three, since a promo without one of its three parts is not a valid
// promo.
func (h *Handler) UpdateProduct(ctx context.Context, req gen.UpdateProductRequestObject) (gen.UpdateProductResponseObject, error) {
	if _, ok := auth.FromContext(ctx); !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermCatalogWrite); err != nil {
		return nil, err
	}
	authCtx, _ := auth.FromContext(ctx)
	body := req.Body

	fields := map[string]string{}

	var basePrice pgtype.Numeric
	hasBasePrice := false
	if body.BasePrice != nil {
		d, apiErr := money.ParseAmount(*body.BasePrice)
		if apiErr != nil {
			fields["basePrice"] = "invalid"
		} else {
			basePrice, hasBasePrice = money.ToNumeric(d), true
		}
	}

	clearCategory := false
	var categoryID *uuid.UUID
	if cp := optionalUUID(body.CategoryId); cp != nil {
		if *cp == nil {
			clearCategory = true
		} else {
			categoryID = *cp
		}
	}

	clearSku := false
	var sku *string
	if sp := optionalString(body.Sku); sp != nil {
		if *sp == nil {
			clearSku = true
		} else {
			sku = *sp
		}
	}

	clearCost := false
	var costPrice pgtype.Numeric
	if cp := optionalString(body.CostPrice); cp != nil {
		if *cp == nil {
			clearCost = true
		} else {
			d, apiErr := money.ParseAmount(**cp)
			if apiErr != nil {
				fields["costPrice"] = "invalid"
			} else {
				costPrice = money.ToNumeric(d)
			}
		}
	}

	clearPromo := false
	var promoPrice pgtype.Numeric
	if pp := optionalString(body.PromoPrice); pp != nil {
		if *pp == nil {
			clearPromo = true
		} else {
			d, apiErr := money.ParseAmount(**pp)
			if apiErr != nil {
				fields["promoPrice"] = "invalid"
			} else {
				promoPrice = money.ToNumeric(d)
			}
		}
	}
	var promoFrom *time.Time
	if pf := optionalTime(body.PromoFrom); pf != nil {
		if *pf == nil {
			clearPromo = true
		} else {
			promoFrom = *pf
		}
	}
	var promoTo *time.Time
	if pt := optionalTime(body.PromoTo); pt != nil {
		if *pt == nil {
			clearPromo = true
		} else {
			promoTo = *pt
		}
	}
	if promoFrom != nil && promoTo != nil && promoFrom.After(*promoTo) {
		fields["promoTo"] = "invalid"
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

	if len(fields) > 0 {
		return nil, apierr.Validation(fields)
	}

	tx, err := h.svc.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("catalog: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := h.svc.q.WithTx(tx)

	params := db.UpdateProductParams{
		ClearCategory: clearCategory, CategoryID: categoryID, UnitID: body.UnitId, Slug: body.Slug,
		ClearSku: clearSku, Sku: sku, ClearCost: clearCost, CostPrice: costPrice,
		ClearPromo: clearPromo, PromoPrice: promoPrice, PromoFrom: promoFrom, PromoTo: promoTo,
		IsActive: body.IsActive, IsFeatured: body.IsFeatured, ShopID: authCtx.ShopID, ID: req.Id,
	}
	if hasBasePrice {
		params.BasePrice = basePrice
	}

	updated, err := qtx.UpdateProduct(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("product")
		}
		if field, ok := conflictField(err); ok {
			return nil, apierr.Conflict(field)
		}
		if fkField, ok := invalidFKField(err); ok {
			return nil, apierr.Validation(map[string]string{fkField: "invalid"})
		}
		return nil, fmt.Errorf("catalog: update product: %w", err)
	}

	for locale, e := range patchEntries {
		if err := qtx.UpsertProductTranslation(ctx, db.UpsertProductTranslationParams{ProductID: updated.ID, Locale: locale, Name: e.Name, Description: e.Description}); err != nil {
			return nil, fmt.Errorf("catalog: upsert product translation: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("catalog: commit update product: %w", err)
	}

	locale := h.svc.resolveLocale(ctx)
	fresh, err := h.svc.q.GetProductForStaff(ctx, db.GetProductForStaffParams{Locale: locale, ShopID: authCtx.ShopID, ID: updated.ID})
	if err != nil {
		return nil, fmt.Errorf("catalog: get product after update: %w", err)
	}
	g, err := h.svc.buildFullProduct(ctx, authCtx.ShopID, commonFromStaffRow(fresh), &fresh.CostPrice, locale, true, true)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	return gen.UpdateProduct200JSONResponse(g), nil
}

// DeleteProduct soft-deletes a product. Requires catalog.write
// (manager+). Variants are left in place but become unreachable through
// this shop's normal product/variant queries (they all filter
// deleted_at IS NULL on the parent too, via the product join, or are
// simply no longer reachable from a deleted product's own endpoints).
func (h *Handler) DeleteProduct(ctx context.Context, req gen.DeleteProductRequestObject) (gen.DeleteProductResponseObject, error) {
	if _, ok := auth.FromContext(ctx); !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermCatalogWrite); err != nil {
		return nil, err
	}
	authCtx, _ := auth.FromContext(ctx)

	if _, err := h.svc.q.GetProductForStaff(ctx, db.GetProductForStaffParams{Locale: h.svc.defaultLocale, ShopID: authCtx.ShopID, ID: req.Id}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("product")
		}
		return nil, fmt.Errorf("catalog: get product: %w", err)
	}

	if err := h.svc.q.SoftDeleteProduct(ctx, db.SoftDeleteProductParams{ShopID: authCtx.ShopID, ID: req.Id}); err != nil {
		return nil, fmt.Errorf("catalog: soft delete product: %w", err)
	}
	return gen.DeleteProduct204Response{}, nil
}
