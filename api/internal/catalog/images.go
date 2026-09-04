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
	"github.com/Sulton-Ali/savdo/api/internal/media"
)

// maxProductImages is the "up to 8 images per product" cap (D-34).
const maxProductImages = 8

// toGenProductImage builds a gen.ProductImage from a joined
// product_images/media_files row, deriving its thumb/card/full URLs via
// media.URLs(s.mediaBaseURL, ...) — the same helper the media module's
// own Handler uses, so a stored storage_key becomes a URL exactly the
// same way regardless of which module builds the response.
func (s *Service) toGenProductImage(row db.ListProductImagesRow) gen.ProductImage {
	return gen.ProductImage{
		Id:        row.ID,
		MediaId:   row.MediaID,
		VariantId: nullableUUID(row.VariantID),
		SortOrder: int(row.SortOrder),
		IsCover:   row.IsCover,
		Urls:      media.URLs(s.mediaBaseURL, row.StorageKey),
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
		items[i] = s.toGenProductImage(r)
	}
	return items, nil
}

// AddProductImage attaches an uploaded image to a product. Requires
// catalog.write (manager+). The 8-image cap is checked and the row
// inserted inside one transaction, after LockShop(shopID) — the same
// per-tenant serialization point CreateLocation/UpdateLocation use — so
// two concurrent attaches at count 7 cannot both read "7, under the cap"
// and both insert, landing the product at 9 images.
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

	mediaFile, err := h.svc.q.GetMediaFile(ctx, db.GetMediaFileParams{ShopID: authCtx.ShopID, ID: body.MediaId})
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

	tx, err := h.svc.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("catalog: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := h.svc.q.WithTx(tx)

	if _, err := qtx.LockShop(ctx, authCtx.ShopID); err != nil {
		return nil, fmt.Errorf("catalog: lock shop: %w", err)
	}

	count, err := qtx.CountProductImages(ctx, db.CountProductImagesParams{ShopID: authCtx.ShopID, ProductID: req.Id})
	if err != nil {
		return nil, fmt.Errorf("catalog: count product images: %w", err)
	}
	if count >= maxProductImages {
		return nil, apierr.Validation(map[string]string{"mediaId": "invalid"})
	}

	isCover := count == 0
	if body.IsCover != nil {
		isCover = *body.IsCover
	}
	// count < maxProductImages (checked above), so this always fits int32.
	sortOrder := int32(count) // #nosec G115 -- bounded by maxProductImages above

	if isCover {
		if err := qtx.ClearCover(ctx, db.ClearCoverParams{ShopID: authCtx.ShopID, ProductID: req.Id}); err != nil {
			return nil, fmt.Errorf("catalog: clear cover: %w", err)
		}
	}
	created, err := qtx.AddProductImage(ctx, db.AddProductImageParams{
		ID: newID(), ShopID: authCtx.ShopID, ProductID: req.Id, VariantID: body.VariantId,
		MediaID: body.MediaId, SortOrder: sortOrder, IsCover: isCover,
	})
	if err != nil {
		if apiErr, ok := mapWriteError(err); ok {
			return nil, apiErr
		}
		return nil, fmt.Errorf("catalog: add product image: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("catalog: commit add product image: %w", err)
	}

	resp := gen.ProductImage{
		Id: created.ID, MediaId: created.MediaID, VariantId: nullableUUID(created.VariantID),
		SortOrder: int(created.SortOrder), IsCover: created.IsCover, Urls: media.URLs(h.svc.mediaBaseURL, mediaFile.StorageKey),
	}
	return gen.AddProductImage201JSONResponse(resp), nil
}

// UpdateProductImage retags an image's variant and/or changes its cover
// status. Requires catalog.write (manager+, D-43). Partial update (D-35):
// `variantId` absent leaves the tie unchanged, explicit `null` unties the
// image to product-level, a uuid must name a variant of this product in
// this shop (else 400 fields.variantId: invalid, the same check and
// vocabulary AddProductImage uses). `isCover` absent leaves the flag
// unchanged; `true` makes this image the cover and clears the previous
// one in the same transaction (ClearCover then the patch, same ordering
// AddProductImage/ReorderProductImages use so the partial unique index
// product_images_one_cover_key is never hit); `false` clears only this
// image's flag — AddProductImage already allows a caller to leave a
// product with zero cover images by passing isCover:false on its first
// upload, so this mirrors that existing permissiveness rather than
// promoting a replacement.
func (h *Handler) UpdateProductImage(ctx context.Context, req gen.UpdateProductImageRequestObject) (gen.UpdateProductImageResponseObject, error) {
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
	for i := range rows {
		if rows[i].ID == req.ImageId {
			r := rows[i]
			target = &r
			break
		}
	}
	if target == nil {
		return nil, apierr.NotFound("image")
	}

	body := req.Body

	var clearVariant bool
	var variantID *uuid.UUID
	if vp := optionalUUID(body.VariantId); vp != nil {
		if *vp == nil {
			clearVariant = true // explicit null: untie from the variant
		} else {
			v, err := h.svc.q.GetVariantForStaff(ctx, db.GetVariantForStaffParams{ShopID: authCtx.ShopID, ID: **vp})
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return nil, apierr.Validation(map[string]string{"variantId": "invalid"})
				}
				return nil, fmt.Errorf("catalog: get variant: %w", err)
			}
			if v.ProductID != req.Id {
				return nil, apierr.Validation(map[string]string{"variantId": "invalid"})
			}
			variantID = *vp
		}
	}

	tx, err := h.svc.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("catalog: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := h.svc.q.WithTx(tx)

	if body.IsCover != nil && *body.IsCover {
		if err := qtx.ClearCover(ctx, db.ClearCoverParams{ShopID: authCtx.ShopID, ProductID: req.Id}); err != nil {
			return nil, fmt.Errorf("catalog: clear cover: %w", err)
		}
	}
	if err := qtx.UpdateProductImage(ctx, db.UpdateProductImageParams{
		ClearVariant: clearVariant, VariantID: variantID, IsCover: body.IsCover,
		ShopID: authCtx.ShopID, ID: req.ImageId,
	}); err != nil {
		if apiErr, ok := mapWriteError(err); ok {
			return nil, apiErr
		}
		return nil, fmt.Errorf("catalog: update product image: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("catalog: commit update product image: %w", err)
	}

	updated := *target
	if clearVariant {
		updated.VariantID = nil
	} else if variantID != nil {
		updated.VariantID = variantID
	}
	if body.IsCover != nil {
		updated.IsCover = *body.IsCover
	}
	return gen.UpdateProductImage200JSONResponse(h.svc.toGenProductImage(updated)), nil
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
