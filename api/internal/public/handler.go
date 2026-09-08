package public

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/catalog"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/media"
	"github.com/Sulton-Ali/savdo/api/internal/money"
)

// contentBlockKeys is the O-19 vocabulary in the fixed order
// GetPublicShop walks it in, mirroring content.allKeys (unexported,
// content package) so PublicShop.blocks is built deterministically.
var contentBlockKeys = []gen.ContentKey{gen.Hero, gen.About, gen.Hours, gen.Contacts, gen.Social, gen.Seo}

// GetPublicShop returns the shop's identity and its six landing content
// blocks, each resolved for the caller's Accept-Language with the D-104
// uz fallback. No auth (allow-listed past auth.Service.Middleware).
func (h *Handler) GetPublicShop(ctx context.Context, _ gen.GetPublicShopRequestObject) (gen.GetPublicShopResponseObject, error) {
	shop, err := h.svc.resolveShop(ctx)
	if err != nil {
		return nil, err
	}
	locale := catalog.ResolveLocale(ctx, shop.DefaultLocale)

	resolved, err := h.svc.contentSvc.Resolve(ctx, shop.ID, gen.Locale(locale))
	if err != nil {
		return nil, fmt.Errorf("public: resolve content: %w", err)
	}

	out := gen.PublicShop{
		Name: shop.Name, Slug: shop.Slug, Currency: shop.Currency,
		DefaultLocale: gen.Locale(shop.DefaultLocale), Locale: gen.Locale(locale),
	}

	fallback := false
	for _, key := range contentBlockKeys {
		block := resolved[key]
		if block.Data == nil {
			continue
		}
		if block.TranslationFallback {
			fallback = true
		}
		if err := h.applyBlock(ctx, shop.ID, key, block.Data, &out.Blocks); err != nil {
			return nil, err
		}
	}
	out.TranslationFallback = fallback

	return gen.GetPublicShop200JSONResponse(out), nil
}

// applyBlock decodes one resolved content block's generic data into its
// O-19 typed shape and sets the matching PublicShopBlocks field. hero
// additionally resolves imageMediaId to its MediaUrls set (O-21) — the
// only block that needs a second lookup.
func (h *Handler) applyBlock(ctx context.Context, shopID uuid.UUID, key gen.ContentKey, data map[string]interface{}, blocks *gen.PublicShopBlocks) error {
	switch key {
	case gen.Hero:
		hero, err := decodeBlock[gen.ContentHero](data)
		if err != nil {
			return fmt.Errorf("public: decode hero block: %w", err)
		}
		out := gen.PublicHero{Title: hero.Title, Tagline: hero.Tagline, ImageMediaId: hero.ImageMediaId}
		if hero.ImageMediaId != nil {
			mediaFile, err := h.svc.q.GetMediaFile(ctx, db.GetMediaFileParams{ShopID: shopID, ID: *hero.ImageMediaId})
			if err != nil {
				if !errors.Is(err, pgx.ErrNoRows) {
					return fmt.Errorf("public: get hero media: %w", err)
				}
				// imageMediaId names media that no longer exists — image
				// stays absent (O-21's own "absent when... no longer
				// exists"), not an error: the rest of the shop response
				// is still valid.
			} else {
				urls := media.URLs(h.svc.mediaBaseURL, mediaFile.StorageKey)
				out.Image = &urls
			}
		}
		blocks.Hero = &out
	case gen.About:
		about, err := decodeBlock[gen.ContentAbout](data)
		if err != nil {
			return fmt.Errorf("public: decode about block: %w", err)
		}
		blocks.About = &about
	case gen.Hours:
		hours, err := decodeBlock[gen.ContentHours](data)
		if err != nil {
			return fmt.Errorf("public: decode hours block: %w", err)
		}
		blocks.Hours = &hours
	case gen.Contacts:
		contacts, err := decodeBlock[gen.ContentContacts](data)
		if err != nil {
			return fmt.Errorf("public: decode contacts block: %w", err)
		}
		blocks.Contacts = &contacts
	case gen.Social:
		social, err := decodeBlock[gen.ContentSocial](data)
		if err != nil {
			return fmt.Errorf("public: decode social block: %w", err)
		}
		blocks.Social = &social
	case gen.Seo:
		seo, err := decodeBlock[gen.ContentSeo](data)
		if err != nil {
			return fmt.Errorf("public: decode seo block: %w", err)
		}
		blocks.Seo = &seo
	}
	return nil
}

// ListPublicCategories returns every active category with its active
// product count (O-20/O-21). Not cursor-paginated (a shop has few
// categories).
func (h *Handler) ListPublicCategories(ctx context.Context, _ gen.ListPublicCategoriesRequestObject) (gen.ListPublicCategoriesResponseObject, error) {
	shop, err := h.svc.resolveShop(ctx)
	if err != nil {
		return nil, err
	}
	locale := catalog.ResolveLocale(ctx, shop.DefaultLocale)

	rows, err := h.svc.q.ListPublicCategories(ctx, db.ListPublicCategoriesParams{Locale: locale, ShopID: shop.ID})
	if err != nil {
		return nil, fmt.Errorf("public: list categories: %w", err)
	}

	items := make([]gen.PublicCategory, len(rows))
	for i, r := range rows {
		items[i] = gen.PublicCategory{Id: r.ID, Slug: r.Slug, Name: r.Name, ProductCount: int(r.ProductCount)}
	}
	return gen.ListPublicCategories200JSONResponse(gen.PublicCategoryList{Items: items}), nil
}

// searchParam trims, length-caps and LIKE-escapes q — nil (no filter)
// when q is nil or blank after trimming.
func searchParam(q *string) *string {
	if q == nil {
		return nil
	}
	return searchTerm(*q)
}

// ListPublicProducts browses the catalogue: only active products, a
// product with no category included (categorySlug null), one whose
// category is inactive excluded (O-22); price/availability are product-
// level (D-103/O-20), newest first (D-92).
func (h *Handler) ListPublicProducts(ctx context.Context, req gen.ListPublicProductsRequestObject) (gen.ListPublicProductsResponseObject, error) {
	shop, err := h.svc.resolveShop(ctx)
	if err != nil {
		return nil, err
	}
	locale := catalog.ResolveLocale(ctx, shop.DefaultLocale)

	limit := clampLimit(req.Params.Limit)
	cursorCreatedAt, cursorID, err := decodeCursor(req.Params.Cursor)
	if err != nil {
		return nil, err
	}

	rows, err := h.svc.q.ListPublicProducts(ctx, db.ListPublicProductsParams{
		Locale: locale, ShopID: shop.ID, CategorySlug: req.Params.Category, Featured: req.Params.Featured,
		Q: searchParam(req.Params.Q), CursorCreatedAt: cursorCreatedAt, CursorID: cursorID, Limit: limit + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("public: list products: %w", err)
	}
	page, nextCursor := paginateProducts(rows, limit)
	ids := productIDs(page)

	covers, err := h.coverImages(ctx, shop.ID, ids)
	if err != nil {
		return nil, err
	}
	availability, err := h.listAvailability(ctx, shop.ID, ids)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	loc := shopLocation(shop)
	items := make([]gen.PublicProductListItem, len(page))
	for i, r := range page {
		price, err := effectivePrice(r.BasePrice, r.PromoPrice, r.PromoFrom, r.PromoTo, pgtype.Numeric{}, now, loc)
		if err != nil {
			return nil, fmt.Errorf("public: product price: %w", err)
		}
		item := gen.PublicProductListItem{
			Id: r.ID, Slug: r.Slug, Name: r.Name, Price: price,
			Availability: bestAvailability(availability[r.ID]), CategorySlug: nullableString(r.CategorySlug),
		}
		if cover, ok := covers[r.ID]; ok {
			c := cover
			item.CoverImage = &c
		}
		items[i] = item
	}

	return gen.ListPublicProducts200JSONResponse(gen.PublicProductList{Items: items, NextCursor: nullableString(nextCursor)}), nil
}

// coverImages batches D-83's cover-image pick (isCover, else first by
// position) for a whole page of product ids in one query — never one
// query per product (a known trap this task's own spec names).
func (h *Handler) coverImages(ctx context.Context, shopID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]gen.ProductImage, error) {
	out := make(map[uuid.UUID]gen.ProductImage, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := h.svc.q.ListCoverImagesForProducts(ctx, db.ListCoverImagesForProductsParams{ShopID: shopID, ProductIds: ids})
	if err != nil {
		return nil, fmt.Errorf("public: list cover images: %w", err)
	}
	for _, r := range rows {
		out[r.ProductID] = toProductImage(h.svc.mediaBaseURL, r.ID, r.MediaID, r.VariantID, r.SortOrder, r.IsCover, r.StorageKey)
	}
	return out, nil
}

// listAvailability batches per-variant qty/threshold for a whole page of
// product ids in one query (SumVariantQtyForProducts), grouped by
// product id — ListPublicProducts.availability is then
// bestAvailability(...) of each product's group. SumVariantQtyForProducts
// itself filters to active, non-deleted variants (its own doc comment,
// stock.sql), so a product whose only in-stock variant has since been
// deactivated contributes no row here — bestAvailability's empty-input
// fallback (out_of_stock) then agrees with GET /public/products/{slug}'s
// own empty `variants` array for the same product (O-20).
func (h *Handler) listAvailability(ctx context.Context, shopID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID][]gen.Availability, error) {
	out := make(map[uuid.UUID][]gen.Availability, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := h.svc.q.SumVariantQtyForProducts(ctx, db.SumVariantQtyForProductsParams{ShopID: shopID, ProductIds: ids})
	if err != nil {
		return nil, fmt.Errorf("public: sum variant qty: %w", err)
	}
	for _, r := range rows {
		qty, err := money.FromNumeric(r.Qty)
		if err != nil {
			return nil, fmt.Errorf("public: variant qty: %w", err)
		}
		out[r.ProductID] = append(out[r.ProductID], classifyAvailability(qty, decimal.NewFromInt32(r.Threshold)))
	}
	return out, nil
}

// GetPublicProductBySlug returns one active product by slug, with only
// its active variants (each carrying its own price/availability) and its
// images. 404 for an unknown, inactive, or inactive-category product
// (O-22) — GetPublicProductBySlug's own WHERE clause already enforces
// all three.
func (h *Handler) GetPublicProductBySlug(ctx context.Context, req gen.GetPublicProductBySlugRequestObject) (gen.GetPublicProductBySlugResponseObject, error) {
	shop, err := h.svc.resolveShop(ctx)
	if err != nil {
		return nil, err
	}
	locale := catalog.ResolveLocale(ctx, shop.DefaultLocale)

	row, err := h.svc.q.GetPublicProductBySlug(ctx, db.GetPublicProductBySlugParams{Locale: locale, ShopID: shop.ID, Slug: req.Slug})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("product")
		}
		return nil, fmt.Errorf("public: get product: %w", err)
	}
	// name/description/locale_used already come resolved from the same
	// LATERAL-joined row (products.sql's GetPublicProductBySlug), the
	// ADR-012 requested->uz->any fallback applied once, at the database,
	// as one atomic pick — no second ListProductTranslations query.

	var categoryName *string
	if row.CategoryID != nil {
		// The category is guaranteed active and non-deleted here —
		// GetPublicProductBySlug's own WHERE clause already checked
		// that (O-22) — this call resolves only its display name.
		cat, err := h.svc.q.GetCategory(ctx, db.GetCategoryParams{Locale: locale, ShopID: shop.ID, ID: *row.CategoryID})
		if err != nil {
			return nil, fmt.Errorf("public: get category: %w", err)
		}
		categoryName = &cat.Name
	}

	basePrice, err := money.FromNumeric(row.BasePrice)
	if err != nil {
		return nil, fmt.Errorf("public: product base price: %w", err)
	}
	promoPrice, err := nullableNumeric(row.PromoPrice)
	if err != nil {
		return nil, fmt.Errorf("public: product promo price: %w", err)
	}
	promoFrom, promoTo := nullableTime(row.PromoFrom), nullableTime(row.PromoTo)
	// D-109 (owner ruling): a promo that is not active right now — future
	// or past — must be entirely invisible on the public site, not just
	// inactive in price: promoPrice/promoFrom/promoTo are null unless the
	// promo is active per the same D-68 calendar-day rule effectivePrice
	// already applies to compute price.current/price.promoActive below. A
	// caller must not be able to read tomorrow's promo window off this
	// response before it starts.
	now := time.Now()
	loc := shopLocation(shop)
	if !promoActive(row.PromoFrom, row.PromoTo, now, loc) {
		promoPrice, err = nullableNumeric(pgtype.Numeric{}) // Valid=false -> null
		if err != nil {
			return nil, fmt.Errorf("public: hide inactive promo price: %w", err)
		}
		promoFrom = nullableTime(nil)
		promoTo = nullableTime(nil)
	}

	variants, activeVariantIDs, err := h.publicVariants(ctx, shop, row)
	if err != nil {
		return nil, err
	}

	imageRows, err := h.svc.q.ListProductImages(ctx, db.ListProductImagesParams{ShopID: shop.ID, ProductID: row.ID})
	if err != nil {
		return nil, fmt.Errorf("public: list product images: %w", err)
	}
	images := make([]gen.ProductImage, len(imageRows))
	for i, r := range imageRows {
		img := toProductImage(h.svc.mediaBaseURL, r.ID, r.MediaID, r.VariantID, r.SortOrder, r.IsCover, r.StorageKey)
		// An image tagged to a variant that is no longer active (or was
		// deleted) must not name a variant this response otherwise never
		// mentions — variantId is nulled rather than dropping the image
		// itself, so a product's cover/gallery still shows a since-
		// deactivated variant's photo, just no longer attributed to it.
		if r.VariantID != nil && !activeVariantIDs[*r.VariantID] {
			img.VariantId = nullableUUID(nil)
		}
		images[i] = img
	}

	resp := gen.ProductPublic{
		Id: row.ID, CategoryId: nullableUUID(row.CategoryID), CategorySlug: nullableString(row.CategorySlug),
		CategoryName: nullableString(categoryName), Slug: row.Slug, Sku: nullableString(row.Sku), UnitId: row.UnitID,
		BasePrice: money.String(basePrice), PromoPrice: promoPrice, PromoFrom: promoFrom, PromoTo: promoTo,
		// IsActive is always true: GetPublicProductBySlug's own WHERE
		// clause (p.is_active) guarantees it, the same way ADR-010 keeps
		// this schema active-only.
		IsActive: true, IsFeatured: row.IsFeatured, Name: row.Name, Description: nullableString(row.Description),
		// Locale is the locale the name/description actually came from
		// (effectiveLocale), never the raw requested locale, matching
		// every other single-translated-entity producer (catalog's own
		// products/categories/units) — PublicShop.locale is deliberately
		// different (its own doc comment/tests): it composes six
		// independently-resolved blocks, so "the locale the data came
		// from" has no single answer there.
		Locale: effectiveLocale(row.LocaleUsed, locale), TranslationFallback: row.LocaleUsed != locale,
		Variants: &variants, Images: &images,
	}
	return gen.GetPublicProductBySlug200JSONResponse(resp), nil
}

// publicVariants lists product's active variants as VariantPublic, each
// with its own D-67/D-68 price and O-20 availability — never an
// inactive variant (O-20's own "only active variants" rule). The second
// return value is the same set's variant ids, for the caller (images) to
// tell an active-variant image from one whose variant has since gone
// inactive or been deleted (ListVariantsForCashier already excludes a
// deleted variant's row entirely; the !v.IsActive skip below excludes an
// inactive one).
func (h *Handler) publicVariants(ctx context.Context, shop db.Shop, product db.GetPublicProductBySlugRow) ([]gen.VariantPublic, map[uuid.UUID]bool, error) {
	sums, err := h.svc.q.SumVariantQtyByProduct(ctx, db.SumVariantQtyByProductParams{ShopID: shop.ID, ProductID: product.ID})
	if err != nil {
		return nil, nil, fmt.Errorf("public: sum variant qty: %w", err)
	}
	qtyByVariant := make(map[uuid.UUID]db.SumVariantQtyByProductRow, len(sums))
	for _, s := range sums {
		qtyByVariant[s.VariantID] = s
	}

	rows, err := h.svc.q.ListVariantsForCashier(ctx, db.ListVariantsForCashierParams{ShopID: shop.ID, ProductID: product.ID})
	if err != nil {
		return nil, nil, fmt.Errorf("public: list variants: %w", err)
	}

	now := time.Now()
	loc := shopLocation(shop)
	variants := make([]gen.VariantPublic, 0, len(rows))
	activeVariantIDs := make(map[uuid.UUID]bool, len(rows))
	for _, v := range rows {
		if !v.IsActive {
			continue
		}
		activeVariantIDs[v.ID] = true
		attrs, err := attributesFrom(v.Attributes)
		if err != nil {
			return nil, nil, fmt.Errorf("public: variant attributes: %w", err)
		}
		price, err := effectivePrice(product.BasePrice, product.PromoPrice, product.PromoFrom, product.PromoTo, v.PriceOverride, now, loc)
		if err != nil {
			return nil, nil, fmt.Errorf("public: variant price: %w", err)
		}
		priceOverride, err := nullableNumeric(v.PriceOverride)
		if err != nil {
			return nil, nil, fmt.Errorf("public: variant price override: %w", err)
		}
		availability := gen.OutOfStock
		if s, ok := qtyByVariant[v.ID]; ok {
			qty, err := money.FromNumeric(s.Qty)
			if err != nil {
				return nil, nil, fmt.Errorf("public: variant qty: %w", err)
			}
			availability = classifyAvailability(qty, decimal.NewFromInt32(s.Threshold))
		}
		variants = append(variants, gen.VariantPublic{
			Id: v.ID, Sku: nullableString(v.Sku), Barcode: nullableString(v.Barcode),
			Attributes: attrs, PriceOverride: priceOverride, Availability: availability, Price: price,
		})
	}
	return variants, activeVariantIDs, nil
}
