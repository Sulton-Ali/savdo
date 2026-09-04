package seed

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/catalog"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/media"
)

// seedLocale is the locale every seeded translation and read uses. It
// matches the demo shop's own default_locale (the column's migration
// default, "uz" — Seed never sets it explicitly, see upsertShop's doc
// comment), which CreateAttributeDefinition/CreateCategory/CreateProduct
// all require an entry for.
const seedLocale = "uz"

// CatalogReport summarizes what Catalog did, for the CLI's summary
// line and for tests. It counts only what was newly created (or, for
// ImagesRepaired, attached to a product that already existed but had
// none) on this call — a second call against a fully-seeded shop
// reports every field as 0.
type CatalogReport struct {
	UnitsCreated      int
	AttributesCreated int
	CategoriesCreated int
	ProductsCreated   int
	VariantsCreated   int
	ImagesCreated     int

	// ImagesRepaired counts images attached to a product that already
	// existed (so seedProducts skipped creating it) but had zero images
	// on file — e.g. a product created some other way, or one whose
	// images were lost from disk without the database rows following.
	// Its variants are never touched: an existing product's variant set
	// is left exactly as it is, only a missing image set is repaired.
	ImagesRepaired int
}

// catalogAuthContext stands in for auth.Middleware: every catalog write
// Catalog makes goes through catalog.Handler exactly as an
// authenticated owner's request would (docs/06-ROADMAP.md Phase 2 T5:
// "never raw INSERTs into catalog tables"), so this is the one place that
// attaches the auth.Context those handlers read via auth.FromContext,
// layered onto the caller's own ctx so cancellation still propagates.
func catalogAuthContext(ctx context.Context, shopID, ownerID uuid.UUID) context.Context {
	return auth.WithContext(ctx, auth.Context{
		ShopID: shopID, UserID: ownerID, Role: db.UserRoleOwner,
		SessionID: newUUID(), Client: db.SessionClientWeb,
	})
}

// Catalog seeds units, attribute definitions, categories, products
// (with their variants) and product images for shopID. Every write goes
// through the catalog service (catalogHandler, which already holds its
// own pool) and the media pipeline (mediaSvc), never a raw INSERT — the
// one exception is units and their translations, upserted directly via q
// (db.Queries): GET /units is the contract's only unit operation (Phase 2
// catalog has no catalog.Service method to create one), so there is no
// service call to route this through.
//
// Idempotent by natural key, the same pattern Seed itself uses for the
// shop/locations/users: a unit is upserted (ON CONFLICT (shop_id, code)),
// and everything else is looked up first and skipped if it already
// exists — an attribute definition by code, a category by slug, a
// product by slug — so a second call against the same database creates
// nothing new and reports every count as 0. The one exception is a
// product's images: an existing product with zero images on file (its
// variants are never touched) gets its spec's images attached anyway,
// counted as CatalogReport.ImagesRepaired rather than ImagesCreated
// (seedProducts' repairProductImages) — so a product that exists but
// somehow never got its images still ends up with them.
func Catalog(ctx context.Context, q *db.Queries, catalogHandler *catalog.Handler, mediaSvc *media.Service, shopID, ownerID uuid.UUID) (CatalogReport, error) {
	authCtx := catalogAuthContext(ctx, shopID, ownerID)

	var report CatalogReport

	unitIDs, unitsCreated, err := seedUnits(authCtx, q, shopID)
	if err != nil {
		return CatalogReport{}, fmt.Errorf("seed catalog: units: %w", err)
	}
	report.UnitsCreated = unitsCreated

	attrsCreated, err := seedAttributeDefinitions(authCtx, q, shopID, catalogHandler)
	if err != nil {
		return CatalogReport{}, fmt.Errorf("seed catalog: attribute definitions: %w", err)
	}
	report.AttributesCreated = attrsCreated

	categoryIDs, categoriesCreated, err := seedCategories(authCtx, q, shopID, catalogHandler)
	if err != nil {
		return CatalogReport{}, fmt.Errorf("seed catalog: categories: %w", err)
	}
	report.CategoriesCreated = categoriesCreated

	productsCreated, variantsCreated, imagesCreated, imagesRepaired, err := seedProducts(authCtx, q, shopID, ownerID, catalogHandler, mediaSvc, unitIDs, categoryIDs)
	if err != nil {
		return CatalogReport{}, fmt.Errorf("seed catalog: products: %w", err)
	}
	report.ProductsCreated = productsCreated
	report.VariantsCreated = variantsCreated
	report.ImagesCreated = imagesCreated
	report.ImagesRepaired = imagesRepaired

	return report, nil
}

// seedUnits upserts unitSpecs (ON CONFLICT (shop_id, code) — see units.sql,
// already written to be idempotent) and their translations, returning
// each unit's id by code for seedProducts to reference.
func seedUnits(ctx context.Context, q *db.Queries, shopID uuid.UUID) (map[string]uuid.UUID, int, error) {
	existing, err := q.ListUnits(ctx, db.ListUnitsParams{Locale: seedLocale, ShopID: shopID})
	if err != nil {
		return nil, 0, fmt.Errorf("list units: %w", err)
	}
	existingByCode := make(map[string]uuid.UUID, len(existing))
	for _, u := range existing {
		existingByCode[u.Code] = u.ID
	}

	ids := make(map[string]uuid.UUID, len(unitSpecs))
	created := 0
	for _, spec := range unitSpecs {
		id, ok := existingByCode[spec.code]
		if !ok {
			id = newUUID()
			created++
		}
		unit, err := q.UpsertUnit(ctx, db.UpsertUnitParams{ID: id, ShopID: shopID, Code: spec.code, Precision: spec.precision})
		if err != nil {
			return nil, 0, fmt.Errorf("upsert unit %q: %w", spec.code, err)
		}
		for locale, name := range map[string]string{"uz": spec.uz, "ru": spec.ru, "en": spec.en} {
			if err := q.UpsertUnitTranslation(ctx, db.UpsertUnitTranslationParams{UnitID: unit.ID, Locale: locale, Name: name}); err != nil {
				return nil, 0, fmt.Errorf("upsert unit translation %s/%s: %w", spec.code, locale, err)
			}
		}
		ids[spec.code] = unit.ID
	}
	return ids, created, nil
}

// seedAttributeDefinitions creates whichever of attributeSpecs don't
// already exist for shopID (matched by code), through
// catalogHandler.CreateAttributeDefinition — leaving an existing
// definition's translations untouched, the same skip-if-exists contract
// Seed's own upsertLocations/upsertUsers use.
func seedAttributeDefinitions(ctx context.Context, q *db.Queries, shopID uuid.UUID, h *catalog.Handler) (int, error) {
	existing, err := q.ListAttributeDefinitions(ctx, db.ListAttributeDefinitionsParams{Locale: seedLocale, ShopID: shopID})
	if err != nil {
		return 0, fmt.Errorf("list attribute definitions: %w", err)
	}
	existingCodes := make(map[string]bool, len(existing))
	for _, a := range existing {
		existingCodes[a.Code] = true
	}

	created := 0
	for _, spec := range attributeSpecs {
		if existingCodes[spec.code] {
			continue
		}
		sortOrder := spec.sortOrder
		body := gen.AttributeDefinitionCreate{
			Code: spec.code, SortOrder: &sortOrder,
			Translations: translations3(spec.uz, spec.ru, spec.en),
		}
		if _, err := h.CreateAttributeDefinition(ctx, gen.CreateAttributeDefinitionRequestObject{Body: &body}); err != nil {
			return 0, fmt.Errorf("create attribute definition %q: %w", spec.code, err)
		}
		created++
	}
	return created, nil
}

// seedCategories creates whichever of categorySpecs don't already exist
// for shopID (matched by slug), through catalogHandler.CreateCategory.
// categorySpecs lists every parent before its children, so a child's
// parentSlug always resolves against ids already known — either from a
// prior run (loaded into ids upfront) or created earlier in this same
// call.
func seedCategories(ctx context.Context, q *db.Queries, shopID uuid.UUID, h *catalog.Handler) (map[string]uuid.UUID, int, error) {
	existing, err := q.ListCategories(ctx, db.ListCategoriesParams{Locale: seedLocale, ShopID: shopID, IncludeInactive: true})
	if err != nil {
		return nil, 0, fmt.Errorf("list categories: %w", err)
	}
	ids := make(map[string]uuid.UUID, len(categorySpecs))
	for _, c := range existing {
		ids[c.Slug] = c.ID
	}

	created := 0
	for _, spec := range categorySpecs {
		if _, ok := ids[spec.slug]; ok {
			continue
		}

		var parentID *uuid.UUID
		if spec.parentSlug != "" {
			pid, ok := ids[spec.parentSlug]
			if !ok {
				return nil, 0, fmt.Errorf("category %q: parent %q not seeded yet", spec.slug, spec.parentSlug)
			}
			parentID = &pid
		}

		slug := spec.slug
		sortOrder := spec.sortOrder
		body := gen.CategoryCreate{
			Slug: &slug, ParentId: parentID, SortOrder: &sortOrder,
			Translations: translations3(spec.uz, spec.ru, spec.en),
		}
		resp, err := h.CreateCategory(ctx, gen.CreateCategoryRequestObject{Body: &body})
		if err != nil {
			return nil, 0, fmt.Errorf("create category %q: %w", spec.slug, err)
		}
		created201, ok := resp.(gen.CreateCategory201JSONResponse)
		if !ok {
			return nil, 0, fmt.Errorf("create category %q: unexpected response type %T", spec.slug, resp)
		}
		ids[spec.slug] = created201.Id
		created++
	}
	return ids, created, nil
}

// existingProducts maps shopID's current product slugs (active and
// inactive alike) to their id, for seedProducts' skip-if-exists check and
// for repairing a pre-existing product's missing images. Products are few
// enough (dozens, not thousands) that one unpaginated
// ListProductsForStaff call covers the whole catalogue.
func existingProducts(ctx context.Context, q *db.Queries, shopID uuid.UUID) (map[string]uuid.UUID, error) {
	rows, err := q.ListProductsForStaff(ctx, db.ListProductsForStaffParams{
		Locale: seedLocale, ShopID: shopID, IncludeInactive: true, Limit: 1000,
	})
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}
	bySlug := make(map[string]uuid.UUID, len(rows))
	for _, r := range rows {
		bySlug[r.Slug] = r.ID
	}
	return bySlug, nil
}

// seedProducts creates whichever of productSpecs don't already exist for
// shopID (matched by slug), each through one catalogHandler.CreateProduct
// call carrying its variants, then attaches its generated placeholder
// images through mediaSvc.Upload + catalogHandler.AddProductImage. A
// product that already exists is never re-created and its variants are
// never touched, but if it has zero images on file — e.g. it was created
// some other way, or its images were lost from disk without the database
// rows following — its spec's images are attached to it anyway
// (repairProductImages), counted separately as ImagesRepaired.
func seedProducts(ctx context.Context, q *db.Queries, shopID, ownerID uuid.UUID, h *catalog.Handler, mediaSvc *media.Service, unitIDs, categoryIDs map[string]uuid.UUID) (productsCreated, variantsCreated, imagesCreated, imagesRepaired int, err error) {
	existing, err := existingProducts(ctx, q, shopID)
	if err != nil {
		return 0, 0, 0, 0, err
	}

	pcsUnitID, ok := unitIDs["pcs"]
	if !ok {
		return 0, 0, 0, 0, fmt.Errorf("unit %q was not seeded", "pcs")
	}

	for _, spec := range productSpecs {
		if existingID, ok := existing[spec.slug]; ok {
			n, err := repairProductImages(ctx, q, h, mediaSvc, shopID, ownerID, spec, existingID)
			if err != nil {
				return 0, 0, 0, 0, err
			}
			imagesRepaired += n
			continue
		}

		categoryID, ok := categoryIDs[spec.categorySlug]
		if !ok {
			return 0, 0, 0, 0, fmt.Errorf("product %q: category %q was not seeded", spec.slug, spec.categorySlug)
		}

		created, err := createProduct(ctx, h, spec, categoryID, pcsUnitID)
		if err != nil {
			return 0, 0, 0, 0, err
		}
		productsCreated++
		variantsCreated += len(spec.variants)

		variantIDs := responseVariantIDs(created)
		n, err := attachImages(ctx, h, mediaSvc, shopID, ownerID, spec, created.Id, variantIDs)
		if err != nil {
			return 0, 0, 0, 0, err
		}
		imagesCreated += n
	}
	return productsCreated, variantsCreated, imagesCreated, imagesRepaired, nil
}

// repairProductImages attaches spec's images to productID — a product
// seedProducts found already existing — only when it currently has none.
// A product with at least one image already is left alone: this is a
// repair for a product that fell through without ever getting the images
// its own spec calls for, not a way to force every existing product back
// to exactly spec.images on every run.
func repairProductImages(ctx context.Context, q *db.Queries, h *catalog.Handler, mediaSvc *media.Service, shopID, ownerID uuid.UUID, spec productSpec, productID uuid.UUID) (int, error) {
	images, err := q.ListProductImages(ctx, db.ListProductImagesParams{ShopID: shopID, ProductID: productID})
	if err != nil {
		return 0, fmt.Errorf("list product images for %q: %w", spec.slug, err)
	}
	if len(images) > 0 {
		return 0, nil
	}

	variants, err := q.ListVariantsForStaff(ctx, db.ListVariantsForStaffParams{ShopID: shopID, ProductID: productID})
	if err != nil {
		return 0, fmt.Errorf("list variants for %q: %w", spec.slug, err)
	}
	variantIDs := make([]uuid.UUID, len(variants))
	for i, v := range variants {
		variantIDs[i] = v.ID
	}

	return attachImages(ctx, h, mediaSvc, shopID, ownerID, spec, productID, variantIDs)
}

// responseVariantIDs extracts a just-created product's variant ids, in the
// order CreateProduct returned them (buildFullProduct's own
// ListVariantsForStaff-backed ordering — see attachImages/imageSpec.
// variantIdx), for attachImages to tie a variant-specific image to.
func responseVariantIDs(product gen.CreateProduct201JSONResponse) []uuid.UUID {
	if product.Variants == nil {
		return nil
	}
	ids := make([]uuid.UUID, len(*product.Variants))
	for i, v := range *product.Variants {
		ids[i] = v.Id
	}
	return ids
}

// createProduct builds and sends the CreateProduct request for spec,
// returning the created product (including its resolved variant ids, for
// attachImages to tie a variant-specific image to).
func createProduct(ctx context.Context, h *catalog.Handler, spec productSpec, categoryID, unitID uuid.UUID) (gen.CreateProduct201JSONResponse, error) {
	variantCreates := make([]gen.VariantCreate, len(spec.variants))
	for i, v := range spec.variants {
		isActive := v.isActive
		sku := fmt.Sprintf("%s-%02d", spec.sku, i+1)
		vc := gen.VariantCreate{
			Attributes: gen.AttributeValues{"size": v.size, "color": v.color},
			Sku:        &sku,
			IsActive:   &isActive,
		}
		if v.priceOverride != "" {
			po := v.priceOverride
			vc.PriceOverride = &po
		}
		variantCreates[i] = vc
	}

	slug := spec.slug
	sku := spec.sku
	body := gen.ProductCreate{
		Slug: &slug, Sku: &sku, CategoryId: &categoryID, UnitId: unitID,
		BasePrice:    spec.basePrice,
		Translations: translations3Desc(spec.nameUz, spec.nameRu, spec.nameEn, spec.descUz, spec.descRu, spec.descEn),
		Variants:     &variantCreates,
	}
	if spec.costPrice != "" {
		cp := spec.costPrice
		body.CostPrice = &cp
	}
	if spec.promoPrice != "" {
		pp := spec.promoPrice
		body.PromoPrice = &pp
		from := time.Now().UTC().Add(-24 * time.Hour)
		to := time.Now().UTC().Add(30 * 24 * time.Hour)
		body.PromoFrom = &from
		body.PromoTo = &to
	}

	resp, err := h.CreateProduct(ctx, gen.CreateProductRequestObject{Body: &body})
	if err != nil {
		return gen.CreateProduct201JSONResponse{}, fmt.Errorf("create product %q: %w", spec.slug, err)
	}
	created, ok := resp.(gen.CreateProduct201JSONResponse)
	if !ok {
		return gen.CreateProduct201JSONResponse{}, fmt.Errorf("create product %q: unexpected response type %T", spec.slug, resp)
	}
	return created, nil
}

// attachImages generates spec.images' placeholder PNGs, uploads each
// through mediaSvc.Upload (the media pipeline: validate, derive WebP
// thumb/card/full, record a media_files row) and attaches it to productID
// via catalogHandler.AddProductImage, tying it to variantIDs[img.
// variantIdx] when the imageSpec names one. variantIDs must be in the
// product's own variant-creation order (spec.variants' order) for that
// tagging to land on the intended variant — both callers (createProduct's
// response and repairProductImages' ListVariantsForStaff) already
// resolve it that way.
func attachImages(ctx context.Context, h *catalog.Handler, mediaSvc *media.Service, shopID, ownerID uuid.UUID, spec productSpec, productID uuid.UUID, variantIDs []uuid.UUID) (int, error) {
	count := 0
	for _, img := range spec.images {
		data, err := generatePlaceholderImage(img.bgHex, img.label)
		if err != nil {
			return count, fmt.Errorf("generate placeholder image for %q: %w", spec.slug, err)
		}

		mediaFile, err := mediaSvc.Upload(ctx, shopID, ownerID, bytes.NewReader(data))
		if err != nil {
			return count, fmt.Errorf("upload image for %q: %w", spec.slug, err)
		}

		imgBody := gen.ProductImageCreate{MediaId: mediaFile.ID}
		if img.variantIdx >= 0 && img.variantIdx < len(variantIDs) {
			vid := variantIDs[img.variantIdx]
			imgBody.VariantId = &vid
		}
		if _, err := h.AddProductImage(ctx, gen.AddProductImageRequestObject{Id: productID, Body: &imgBody}); err != nil {
			return count, fmt.Errorf("add product image for %q: %w", spec.slug, err)
		}
		count++
	}
	return count, nil
}
