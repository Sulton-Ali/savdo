package catalog_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

func TestAddProductImage_capAndDuplicateAndCoverDefaults(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")
	product := mustCreateProduct(t, h, shopRow.ID, unit.ID, "Cotton Shirt", "125000.00")

	for i := 0; i < 8; i++ {
		media := seedMedia(ctx, t, q, shopRow.ID, fmt.Sprintf("shop/img-%d", i))
		resp, err := h.AddProductImage(owner(shopRow.ID), gen.AddProductImageRequestObject{
			Id: product.Id, Body: &gen.AddProductImageJSONRequestBody{MediaId: media.ID},
		})
		if err != nil {
			t.Fatalf("AddProductImage(%d): %v", i, err)
		}
		created := resp.(gen.AddProductImage201JSONResponse)
		if i == 0 {
			if !created.IsCover {
				t.Errorf("first image IsCover = false, want true (default cover)")
			}
		} else if created.IsCover {
			t.Errorf("image %d IsCover = true, want false (cover already set)", i)
		}
	}

	// The 9th image exceeds the cap.
	overflowMedia := seedMedia(ctx, t, q, shopRow.ID, "shop/img-overflow")
	_, err := h.AddProductImage(owner(shopRow.ID), gen.AddProductImageRequestObject{
		Id: product.Id, Body: &gen.AddProductImageJSONRequestBody{MediaId: overflowMedia.ID},
	})
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("err = %#v, want 400 VALIDATION_FAILED", err)
	}
	fields := apiErr.Details["fields"].(map[string]string)
	if fields["mediaId"] != "invalid" {
		t.Fatalf("fields = %+v, want mediaId=invalid", fields)
	}
}

func TestAddProductImage_duplicateMediaConflict(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")
	product := mustCreateProduct(t, h, shopRow.ID, unit.ID, "Cotton Shirt", "125000.00")
	media := seedMedia(ctx, t, q, shopRow.ID, "shop/img-1")

	if _, err := h.AddProductImage(owner(shopRow.ID), gen.AddProductImageRequestObject{
		Id: product.Id, Body: &gen.AddProductImageJSONRequestBody{MediaId: media.ID},
	}); err != nil {
		t.Fatalf("AddProductImage (1st): %v", err)
	}

	_, err := h.AddProductImage(owner(shopRow.ID), gen.AddProductImageRequestObject{
		Id: product.Id, Body: &gen.AddProductImageJSONRequestBody{MediaId: media.ID},
	})
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Code != gen.CONFLICT || apiErr.Details["field"] != "mediaId" {
		t.Fatalf("err = %#v, want 409 CONFLICT field=mediaId", err)
	}
}

func TestRemoveProductImage_promotesNextCover(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")
	product := mustCreateProduct(t, h, shopRow.ID, unit.ID, "Cotton Shirt", "125000.00")

	media1 := seedMedia(ctx, t, q, shopRow.ID, "shop/img-1")
	media2 := seedMedia(ctx, t, q, shopRow.ID, "shop/img-2")

	resp1, err := h.AddProductImage(owner(shopRow.ID), gen.AddProductImageRequestObject{
		Id: product.Id, Body: &gen.AddProductImageJSONRequestBody{MediaId: media1.ID},
	})
	if err != nil {
		t.Fatalf("AddProductImage (1st): %v", err)
	}
	image1 := resp1.(gen.AddProductImage201JSONResponse)
	if !image1.IsCover {
		t.Fatalf("image1.IsCover = false, want true (first image is cover by default)")
	}

	resp2, err := h.AddProductImage(owner(shopRow.ID), gen.AddProductImageRequestObject{
		Id: product.Id, Body: &gen.AddProductImageJSONRequestBody{MediaId: media2.ID},
	})
	if err != nil {
		t.Fatalf("AddProductImage (2nd): %v", err)
	}
	image2 := resp2.(gen.AddProductImage201JSONResponse)

	if _, err := h.RemoveProductImage(owner(shopRow.ID), gen.RemoveProductImageRequestObject{Id: product.Id, ImageId: image1.Id}); err != nil {
		t.Fatalf("RemoveProductImage: %v", err)
	}

	// image2 should now be the cover.
	getResp, err := h.GetProduct(owner(shopRow.ID), gen.GetProductRequestObject{Id: product.Id})
	if err != nil {
		t.Fatalf("GetProduct: %v", err)
	}
	got := gen.Product(getResp.(gen.GetProduct200JSONResponse))
	if got.Images == nil || len(*got.Images) != 1 {
		t.Fatalf("Images = %+v, want exactly 1 remaining", got.Images)
	}
	remaining := (*got.Images)[0]
	if remaining.Id != image2.Id || !remaining.IsCover {
		t.Fatalf("remaining image = %+v, want image2 promoted to cover", remaining)
	}
}

func TestReorderProductImages_validatesImageIdSet(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")
	product := mustCreateProduct(t, h, shopRow.ID, unit.ID, "Cotton Shirt", "125000.00")

	media1 := seedMedia(ctx, t, q, shopRow.ID, "shop/img-1")
	media2 := seedMedia(ctx, t, q, shopRow.ID, "shop/img-2")
	resp1, err := h.AddProductImage(owner(shopRow.ID), gen.AddProductImageRequestObject{Id: product.Id, Body: &gen.AddProductImageJSONRequestBody{MediaId: media1.ID}})
	if err != nil {
		t.Fatalf("AddProductImage (1st): %v", err)
	}
	resp2, err := h.AddProductImage(owner(shopRow.ID), gen.AddProductImageRequestObject{Id: product.Id, Body: &gen.AddProductImageJSONRequestBody{MediaId: media2.ID}})
	if err != nil {
		t.Fatalf("AddProductImage (2nd): %v", err)
	}
	image1 := resp1.(gen.AddProductImage201JSONResponse)
	image2 := resp2.(gen.AddProductImage201JSONResponse)

	// Missing one of the current images is invalid.
	_, err = h.ReorderProductImages(owner(shopRow.ID), gen.ReorderProductImagesRequestObject{
		Id: product.Id, Body: &gen.ReorderProductImagesJSONRequestBody{ImageIds: []uuid.UUID{image1.Id}},
	})
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("err = %#v, want 400 VALIDATION_FAILED", err)
	}
	fields := apiErr.Details["fields"].(map[string]string)
	if fields["imageIds"] != "invalid" {
		t.Fatalf("fields = %+v, want imageIds=invalid", fields)
	}

	// A valid full permutation, with an explicit new cover, succeeds.
	reorderResp, err := h.ReorderProductImages(owner(shopRow.ID), gen.ReorderProductImagesRequestObject{
		Id:   product.Id,
		Body: &gen.ReorderProductImagesJSONRequestBody{ImageIds: []uuid.UUID{image2.Id, image1.Id}, CoverImageId: &image2.Id},
	})
	if err != nil {
		t.Fatalf("ReorderProductImages: %v", err)
	}
	items := reorderResp.(gen.ReorderProductImages200JSONResponse).Items
	if len(items) != 2 || items[0].Id != image2.Id || !items[0].IsCover {
		t.Fatalf("reordered items = %+v, want image2 first and cover", items)
	}
}

// TestAddProductImage_concurrentAttachesAtCapLeaveExactlyEight proves the
// LockShop-serialized count-then-insert in AddProductImage closes the
// race a naive "count, then insert" would have: with a product already
// at 7 images, 5 concurrent attach attempts must not all read "7, under
// the cap" and all insert, which would land the product past 8. Run with
// -race to also catch any unsynchronized access.
func TestAddProductImage_concurrentAttachesAtCapLeaveExactlyEight(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")
	product := mustCreateProduct(t, h, shopRow.ID, unit.ID, "Cotton Shirt", "125000.00")

	for i := 0; i < 7; i++ {
		media := seedMedia(ctx, t, q, shopRow.ID, fmt.Sprintf("shop/pre-%d", i))
		if _, err := h.AddProductImage(owner(shopRow.ID), gen.AddProductImageRequestObject{
			Id: product.Id, Body: &gen.AddProductImageJSONRequestBody{MediaId: media.ID},
		}); err != nil {
			t.Fatalf("AddProductImage (pre-fill %d): %v", i, err)
		}
	}

	const attempts = 5
	medias := make([]uuid.UUID, attempts)
	for i := range attempts {
		medias[i] = seedMedia(ctx, t, q, shopRow.ID, fmt.Sprintf("shop/race-%d", i)).ID
	}

	results := make(chan error, attempts)
	start := make(chan struct{})
	for _, mediaID := range medias {
		go func(mediaID uuid.UUID) {
			<-start
			_, err := h.AddProductImage(owner(shopRow.ID), gen.AddProductImageRequestObject{
				Id: product.Id, Body: &gen.AddProductImageJSONRequestBody{MediaId: mediaID},
			})
			results <- err
		}(mediaID)
	}
	close(start)

	var oks, rejections int
	for range attempts {
		switch err := <-results; err {
		case nil:
			oks++
		default:
			apiErr, ok := err.(*apierr.Error)
			if !ok || apiErr.Code != gen.VALIDATIONFAILED {
				t.Fatalf("concurrent attach error = %#v, want nil or 400 VALIDATION_FAILED", err)
			}
			rejections++
		}
	}
	if oks != 1 || rejections != attempts-1 {
		t.Fatalf("oks=%d rejections=%d, want exactly 1 ok and %d rejections", oks, rejections, attempts-1)
	}

	final, err := q.ListProductImages(ctx, db.ListProductImagesParams{ShopID: shopRow.ID, ProductID: product.Id})
	if err != nil {
		t.Fatalf("ListProductImages: %v", err)
	}
	const wantImageCap = 8
	if len(final) != wantImageCap {
		t.Fatalf("final image count = %d, want exactly %d", len(final), wantImageCap)
	}
	seen := map[int]bool{}
	for _, img := range final {
		if seen[int(img.SortOrder)] {
			t.Fatalf("duplicate sort_order %d among final images: %+v", img.SortOrder, final)
		}
		seen[int(img.SortOrder)] = true
	}
}
