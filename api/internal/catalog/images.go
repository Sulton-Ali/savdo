package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// maxProductImages is the "up to 8 images per product" cap (D-34).
const maxProductImages = 8

// toGenProductImage builds a gen.ProductImage from a joined
// product_images/media_files row.
func toGenProductImage(row db.ListProductImagesRow) gen.ProductImage {
	return gen.ProductImage{
		Id:        row.ID,
		MediaId:   row.MediaID,
		VariantId: nullableUUID(row.VariantID),
		SortOrder: int(row.SortOrder),
		IsCover:   row.IsCover,
		Urls:      mediaURLs(row.StorageKey),
	}
}

// productImagesFor lists productID's images (any authenticated caller may
// read them; images carry no cost/PII), for embedding into a full Product
// response.
func (s *Service) productImagesFor(ctx context.Context, shopID, productID uuid.UUID) ([]gen.ProductImage, error) {
	rows, err := s.q.ListProductImages(ctx, db.ListProductImagesParams{ShopID: shopID, ProductID: productID})
	if err != nil {
		return nil, err
	}
	items := make([]gen.ProductImage, len(rows))
	for i, r := range rows {
		items[i] = toGenProductImage(r)
	}
	return items, nil
}

// AddProductImage attaches an uploaded image to a product. Requires
// catalog.write (manager+).
func (h *Handler) AddProductImage(ctx context.Context, req gen.AddProductImageRequestObject) (gen.AddProductImageResponseObject, error) {
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

	body := req.Body

	media, err := h.svc.q.GetMediaFile(ctx, db.GetMediaFileParams{ShopID: authCtx.ShopID, ID: body.MediaId})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.Validation(map[string]string{"mediaId": "invalid"})
		}
		return nil, fmt.Errorf("catalog: get media file: %w", err)
	}

	if body.VariantId != nil {
		v, err := h.svc.q.GetVariantForStaff(ctx, db.GetVariantForStaffParams{ShopID: authCtx.ShopID, ID: *body.VariantId})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, apierr.Validation(map[string]string{"variantId": "invalid"})
			}
			return nil, fmt.Errorf("catalog: get variant: %w", err)
		}
		if v.ProductID != req.Id {
			return nil, apierr.Validation(map[string]string{"variantId": "invalid"})
		}
	}

	count, err := h.svc.q.CountProductImages(ctx, db.CountProductImagesParams{ShopID: authCtx.ShopID, ProductID: req.Id})
	if err != nil {
		return nil, fmt.Errorf("catalog: count product images: %w", err)
	}
	if count >= maxProductImages {
		err := apierr.Validation(map[string]string{"mediaId": "invalid"})
		err.Details["reason"] = "limit"
		return nil, err
	}

	isCover := count == 0
	if body.IsCover != nil {
		isCover = *body.IsCover
	}
	// count < maxProductImages (checked above), so this always fits int32.
	sortOrder := int32(count) // #nosec G115 -- bounded by maxProductImages above

	var created db.ProductImage
	if isCover {
		tx, err := h.svc.pool.Begin(ctx)
		if err != nil {
			return nil, fmt.Errorf("catalog: begin tx: %w", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		qtx := h.svc.q.WithTx(tx)

		if err := qtx.ClearCover(ctx, db.ClearCoverParams{ShopID: authCtx.ShopID, ProductID: req.Id}); err != nil {
			return nil, fmt.Errorf("catalog: clear cover: %w", err)
		}
		created, err = qtx.AddProductImage(ctx, db.AddProductImageParams{
			ID: newID(), ShopID: authCtx.ShopID, ProductID: req.Id, VariantID: body.VariantId,
			MediaID: body.MediaId, SortOrder: sortOrder, IsCover: true,
		})
		if err != nil {
			if field, ok := conflictField(err); ok {
				return nil, apierr.Conflict(field)
			}
			return nil, fmt.Errorf("catalog: add product image: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("catalog: commit add product image: %w", err)
		}
	} else {
		created, err = h.svc.q.AddProductImage(ctx, db.AddProductImageParams{
			ID: newID(), ShopID: authCtx.ShopID, ProductID: req.Id, VariantID: body.VariantId,
			MediaID: body.MediaId, SortOrder: sortOrder, IsCover: false,
		})
		if err != nil {
			if field, ok := conflictField(err); ok {
				return nil, apierr.Conflict(field)
			}
			return nil, fmt.Errorf("catalog: add product image: %w", err)
		}
	}

	resp := gen.ProductImage{
		Id: created.ID, MediaId: created.MediaID, VariantId: nullableUUID(created.VariantID),
		SortOrder: int(created.SortOrder), IsCover: created.IsCover, Urls: mediaURLs(media.StorageKey),
	}
	return gen.AddProductImage201JSONResponse(resp), nil
}

// RemoveProductImage removes an image from a product. Requires
// catalog.write (manager+). Hard delete (join row); promotes the next
// image by sort order to cover if the removed one was the cover.
func (h *Handler) RemoveProductImage(ctx context.Context, req gen.RemoveProductImageRequestObject) (gen.RemoveProductImageResponseObject, error) {
	if _, ok := auth.FromContext(ctx); !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermCatalogWrite); err != nil {
		return nil, err
	}
	authCtx, _ := auth.FromContext(ctx)

	rows, err := h.svc.q.ListProductImages(ctx, db.ListProductImagesParams{ShopID: authCtx.ShopID, ProductID: req.Id})
	if err != nil {
		return nil, fmt.Errorf("catalog: list product images: %w", err)
	}

	var target *db.ListProductImagesRow
	var promote *db.ListProductImagesRow
	for i := range rows {
		if rows[i].ID == req.ImageId {
			r := rows[i]
			target = &r
			continue
		}
		if promote == nil || rows[i].SortOrder < promote.SortOrder {
			r := rows[i]
			promote = &r
		}
	}
	if target == nil {
		return nil, apierr.NotFound("image")
	}

	tx, err := h.svc.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("catalog: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := h.svc.q.WithTx(tx)

	if err := qtx.RemoveProductImage(ctx, db.RemoveProductImageParams{ShopID: authCtx.ShopID, ID: req.ImageId}); err != nil {
		return nil, fmt.Errorf("catalog: remove product image: %w", err)
	}
	if target.IsCover && promote != nil {
		if err := qtx.SetCover(ctx, db.SetCoverParams{ShopID: authCtx.ShopID, ID: promote.ID}); err != nil {
			return nil, fmt.Errorf("catalog: promote cover: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("catalog: commit remove product image: %w", err)
	}

	return gen.RemoveProductImage204Response{}, nil
}

// ReorderProductImages reorders a product's images and/or changes its
// cover image. Requires catalog.write (manager+). `imageIds` must name
// exactly the product's current image set.
func (h *Handler) ReorderProductImages(ctx context.Context, req gen.ReorderProductImagesRequestObject) (gen.ReorderProductImagesResponseObject, error) {
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

	current, err := h.svc.q.ListImageIDsForProduct(ctx, db.ListImageIDsForProductParams{ShopID: authCtx.ShopID, ProductID: req.Id})
	if err != nil {
		return nil, fmt.Errorf("catalog: list image ids: %w", err)
	}

	body := req.Body
	if !sameIDSet(current, body.ImageIds) {
		return nil, apierr.Validation(map[string]string{"imageIds": "invalid"})
	}

	if body.CoverImageId != nil && !containsID(body.ImageIds, *body.CoverImageId) {
		return nil, apierr.Validation(map[string]string{"coverImageId": "invalid"})
	}

	tx, err := h.svc.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("catalog: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := h.svc.q.WithTx(tx)

	for i, id := range body.ImageIds {
		if err := qtx.UpdateImageOrder(ctx, db.UpdateImageOrderParams{ShopID: authCtx.ShopID, ID: id, SortOrder: int32(i)}); err != nil {
			return nil, fmt.Errorf("catalog: update image order: %w", err)
		}
	}
	if body.CoverImageId != nil {
		if err := qtx.ClearCover(ctx, db.ClearCoverParams{ShopID: authCtx.ShopID, ProductID: req.Id}); err != nil {
			return nil, fmt.Errorf("catalog: clear cover: %w", err)
		}
		if err := qtx.SetCover(ctx, db.SetCoverParams{ShopID: authCtx.ShopID, ID: *body.CoverImageId}); err != nil {
			return nil, fmt.Errorf("catalog: set cover: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("catalog: commit reorder product images: %w", err)
	}

	items, err := h.svc.productImagesFor(ctx, authCtx.ShopID, req.Id)
	if err != nil {
		return nil, fmt.Errorf("catalog: list product images after reorder: %w", err)
	}
	return gen.ReorderProductImages200JSONResponse(gen.ProductImageList{Items: items}), nil
}

func sameIDSet(a []uuid.UUID, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[uuid.UUID]bool, len(a))
	for _, id := range a {
		set[id] = true
	}
	for _, id := range b {
		if !set[id] {
			return false
		}
		delete(set, id)
	}
	return len(set) == 0
}

func containsID(ids []uuid.UUID, id uuid.UUID) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}
