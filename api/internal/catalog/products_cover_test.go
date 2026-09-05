package catalog_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// TestListProducts_coverImage_isCoverFlaggedWins proves the image flagged
// is_cover wins as coverImage even when it is not first by sort_order
// (D-83). Both the owner (staff) and cashier list shapes carry it —
// coverImage is not gated by any permission.
func TestListProducts_coverImage_isCoverFlaggedWins(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")
	product := mustCreateProduct(t, h, shopRow.ID, unit.ID, "Cotton Shirt", "125000.00")

	mediaFirst := seedMedia(ctx, t, q, shopRow.ID, "shop/img-first")
	mediaCover := seedMedia(ctx, t, q, shopRow.ID, "shop/img-cover")

	// Inserted directly via sqlc so sort_order/is_cover are exact, rather
	// than relying on AddProductImage's "first image defaults to cover"
	// policy.
	if _, err := q.AddProductImage(ctx, db.AddProductImageParams{
		ID: uuid.New(), ShopID: shopRow.ID, ProductID: product.Id, MediaID: mediaFirst.ID, SortOrder: 0, IsCover: false,
	}); err != nil {
		t.Fatalf("AddProductImage(first): %v", err)
	}
	if _, err := q.AddProductImage(ctx, db.AddProductImageParams{
		ID: uuid.New(), ShopID: shopRow.ID, ProductID: product.Id, MediaID: mediaCover.ID, SortOrder: 1, IsCover: true,
	}); err != nil {
		t.Fatalf("AddProductImage(cover): %v", err)
	}

	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{
		{"owner", owner(shopRow.ID)},
		{"cashier", cashier(shopRow.ID)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listResp, err := h.ListProducts(tc.ctx, gen.ListProductsRequestObject{})
			if err != nil {
				t.Fatalf("ListProducts: %v", err)
			}
			items := listResp.(gen.ListProducts200JSONResponse).Items
			if len(items) != 1 {
				t.Fatalf("items = %+v, want exactly 1", items)
			}
			cover := items[0].CoverImage
			if cover == nil {
				t.Fatalf("CoverImage = nil, want the is_cover-flagged image")
			}
			if cover.MediaId != mediaCover.ID {
				t.Errorf("CoverImage.MediaId = %v, want %v (the flagged cover, not the first by sort order)", cover.MediaId, mediaCover.ID)
			}
			if !cover.IsCover {
				t.Errorf("CoverImage.IsCover = false, want true")
			}

			// Lists never carry the full images array (D-83).
			if items[0].Images != nil {
				t.Errorf("Images = %+v, want nil on a list item", items[0].Images)
			}
		})
	}
}

// TestListProducts_coverImage_fallsBackToFirstByPosition proves that when
// no image is flagged is_cover, coverImage falls back to the first image
// by sort_order (D-83).
func TestListProducts_coverImage_fallsBackToFirstByPosition(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")
	product := mustCreateProduct(t, h, shopRow.ID, unit.ID, "Cotton Shirt", "125000.00")

	mediaFirst := seedMedia(ctx, t, q, shopRow.ID, "shop/img-first")
	mediaSecond := seedMedia(ctx, t, q, shopRow.ID, "shop/img-second")

	if _, err := q.AddProductImage(ctx, db.AddProductImageParams{
		ID: uuid.New(), ShopID: shopRow.ID, ProductID: product.Id, MediaID: mediaFirst.ID, SortOrder: 0, IsCover: false,
	}); err != nil {
		t.Fatalf("AddProductImage(first): %v", err)
	}
	if _, err := q.AddProductImage(ctx, db.AddProductImageParams{
		ID: uuid.New(), ShopID: shopRow.ID, ProductID: product.Id, MediaID: mediaSecond.ID, SortOrder: 1, IsCover: false,
	}); err != nil {
		t.Fatalf("AddProductImage(second): %v", err)
	}

	listResp, err := h.ListProducts(owner(shopRow.ID), gen.ListProductsRequestObject{})
	if err != nil {
		t.Fatalf("ListProducts: %v", err)
	}
	items := listResp.(gen.ListProducts200JSONResponse).Items
	if len(items) != 1 {
		t.Fatalf("items = %+v, want exactly 1", items)
	}
	cover := items[0].CoverImage
	if cover == nil {
		t.Fatalf("CoverImage = nil, want the first image by sort order")
	}
	if cover.MediaId != mediaFirst.ID {
		t.Errorf("CoverImage.MediaId = %v, want %v (first by sort order, no cover flagged)", cover.MediaId, mediaFirst.ID)
	}
}

// TestListProducts_coverImage_absentWhenNoImages proves a product with no
// images at all has no coverImage field set (D-83).
func TestListProducts_coverImage_absentWhenNoImages(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")
	mustCreateProduct(t, h, shopRow.ID, unit.ID, "Cotton Shirt", "125000.00")

	listResp, err := h.ListProducts(owner(shopRow.ID), gen.ListProductsRequestObject{})
	if err != nil {
		t.Fatalf("ListProducts: %v", err)
	}
	items := listResp.(gen.ListProducts200JSONResponse).Items
	if len(items) != 1 {
		t.Fatalf("items = %+v, want exactly 1", items)
	}
	if items[0].CoverImage != nil {
		t.Errorf("CoverImage = %+v, want nil (no images)", items[0].CoverImage)
	}
}

// TestListProducts_coverImage_shopIsolation proves shop A's list never
// carries an image belonging to shop B's product, even though both shops
// exist and shop B's product has its own cover image (hard rule 1).
func TestListProducts_coverImage_shopIsolation(t *testing.T) {
	h, q, _ := newTestHandler(t)
	ctx := context.Background()
	shopA := seedShop(ctx, t, q, "shop-a")
	shopB := seedShop(ctx, t, q, "shop-b")
	unitA := seedUnit(ctx, t, q, shopA.ID, "pcs")
	unitB := seedUnit(ctx, t, q, shopB.ID, "pcs")

	mustCreateProduct(t, h, shopA.ID, unitA.ID, "A Product", "100.00")
	productB := mustCreateProduct(t, h, shopB.ID, unitB.ID, "B Product", "200.00")

	mediaB := seedMedia(ctx, t, q, shopB.ID, "shop-b/img-cover")
	if _, err := q.AddProductImage(ctx, db.AddProductImageParams{
		ID: uuid.New(), ShopID: shopB.ID, ProductID: productB.Id, MediaID: mediaB.ID, SortOrder: 0, IsCover: true,
	}); err != nil {
		t.Fatalf("AddProductImage(shop B): %v", err)
	}

	listResp, err := h.ListProducts(owner(shopA.ID), gen.ListProductsRequestObject{})
	if err != nil {
		t.Fatalf("ListProducts: %v", err)
	}
	items := listResp.(gen.ListProducts200JSONResponse).Items
	if len(items) != 1 || items[0].Name != "A Product" {
		t.Fatalf("items = %+v, want exactly shop A's own product", items)
	}
	if items[0].CoverImage != nil {
		t.Errorf("CoverImage = %+v, want nil (shop A's product has no images; shop B's must not leak)", items[0].CoverImage)
	}
}
