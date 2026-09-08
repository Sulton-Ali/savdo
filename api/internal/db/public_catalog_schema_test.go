package db_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

// publicCategory creates a category for the public-catalogue tests below,
// active or not per isActive.
func publicCategory(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, slug string, isActive bool) db.Category {
	t.Helper()
	c, err := q.CreateCategory(ctx, db.CreateCategoryParams{
		ID: uuid.New(), ShopID: shopID, Slug: slug, IsActive: isActive,
	})
	if err != nil {
		t.Fatalf("CreateCategory(%q): %v", slug, err)
	}
	return c
}

// publicProduct creates a product for the public-catalogue tests below,
// with the given category (nil for none), active/featured flags.
func publicProduct(ctx context.Context, t *testing.T, q *db.Queries, shopID, unitID uuid.UUID, categoryID *uuid.UUID, slug string, isActive, isFeatured bool) db.Product {
	t.Helper()
	p, err := q.CreateProduct(ctx, db.CreateProductParams{
		ID: uuid.New(), ShopID: shopID, CategoryID: categoryID, UnitID: unitID, Slug: slug,
		BasePrice: numeric(t, "100000.00"), IsActive: isActive, IsFeatured: isFeatured,
	})
	if err != nil {
		t.Fatalf("CreateProduct(%q): %v", slug, err)
	}
	return p
}

func TestListPublicProducts_excludesInactiveProductsAndInactiveCategories(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-public-products")
	unit := catalogUnit(ctx, t, q, shop.ID, "pcs")
	activeCat := publicCategory(ctx, t, q, shop.ID, "active-cat", true)
	inactiveCat := publicCategory(ctx, t, q, shop.ID, "inactive-cat", false)

	visible := publicProduct(ctx, t, q, shop.ID, unit.ID, &activeCat.ID, "visible", true, false)
	publicProduct(ctx, t, q, shop.ID, unit.ID, &activeCat.ID, "inactive-product", false, false)
	publicProduct(ctx, t, q, shop.ID, unit.ID, &inactiveCat.ID, "product-in-inactive-category", true, false)
	publicProduct(ctx, t, q, shop.ID, unit.ID, nil, "product-with-no-category", true, false)

	rows, err := q.ListPublicProducts(ctx, db.ListPublicProductsParams{ShopID: shop.ID, Locale: "uz", Limit: 100})
	if err != nil {
		t.Fatalf("ListPublicProducts: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != visible.ID {
		t.Fatalf("ListPublicProducts = %+v, want exactly the one active product in an active category", rows)
	}
	if rows[0].CategorySlug != activeCat.Slug {
		t.Fatalf("CategorySlug = %q, want %q", rows[0].CategorySlug, activeCat.Slug)
	}
}

func TestListPublicProducts_categorySlugAndFeaturedFilters(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-public-filters")
	unit := catalogUnit(ctx, t, q, shop.ID, "pcs")
	catA := publicCategory(ctx, t, q, shop.ID, "cat-a", true)
	catB := publicCategory(ctx, t, q, shop.ID, "cat-b", true)

	featuredInA := publicProduct(ctx, t, q, shop.ID, unit.ID, &catA.ID, "featured-a", true, true)
	plainInA := publicProduct(ctx, t, q, shop.ID, unit.ID, &catA.ID, "plain-a", true, false)
	featuredInB := publicProduct(ctx, t, q, shop.ID, unit.ID, &catB.ID, "featured-b", true, true)

	byCategory, err := q.ListPublicProducts(ctx, db.ListPublicProductsParams{
		ShopID: shop.ID, Locale: "uz", Limit: 100, CategorySlug: &catA.Slug,
	})
	if err != nil {
		t.Fatalf("ListPublicProducts(category=cat-a): %v", err)
	}
	assertPublicProductIDs(t, byCategory, featuredInA.ID, plainInA.ID)

	featured := true
	byFeatured, err := q.ListPublicProducts(ctx, db.ListPublicProductsParams{
		ShopID: shop.ID, Locale: "uz", Limit: 100, Featured: &featured,
	})
	if err != nil {
		t.Fatalf("ListPublicProducts(featured=true): %v", err)
	}
	assertPublicProductIDs(t, byFeatured, featuredInA.ID, featuredInB.ID)
}

func assertPublicProductIDs(t *testing.T, rows []db.ListPublicProductsRow, want ...uuid.UUID) {
	t.Helper()
	if len(rows) != len(want) {
		t.Fatalf("want %d rows, got %d: %+v", len(want), len(rows), rows)
	}
	wantSet := map[uuid.UUID]bool{}
	for _, id := range want {
		wantSet[id] = true
	}
	for _, r := range rows {
		if !wantSet[r.ID] {
			t.Errorf("unexpected product %s in result", r.ID)
		}
	}
}

func TestListPublicProducts_cursorPagination(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-public-cursor")
	unit := catalogUnit(ctx, t, q, shop.ID, "pcs")
	cat := publicCategory(ctx, t, q, shop.ID, "cursor-cat", true)

	const n = 5
	var created []uuid.UUID
	for i := 0; i < n; i++ {
		p := publicProduct(ctx, t, q, shop.ID, unit.ID, &cat.ID, fmt.Sprintf("cursor-product-%d", i), true, false)
		created = append(created, p.ID)
	}

	reference, err := q.ListPublicProducts(ctx, db.ListPublicProductsParams{ShopID: shop.ID, Locale: "uz", Limit: 100})
	if err != nil {
		t.Fatalf("ListPublicProducts (reference): %v", err)
	}
	if len(reference) != n {
		t.Fatalf("want %d products, got %d", n, len(reference))
	}

	var paginated []db.ListPublicProductsRow
	params := db.ListPublicProductsParams{ShopID: shop.ID, Locale: "uz", Limit: 2}
	for {
		page, err := q.ListPublicProducts(ctx, params)
		if err != nil {
			t.Fatalf("ListPublicProducts (page): %v", err)
		}
		paginated = append(paginated, page...)
		if len(page) < 2 {
			break
		}
		last := page[len(page)-1]
		ca, id := last.CreatedAt, last.ID
		params.CursorCreatedAt, params.CursorID = &ca, &id
	}

	if len(paginated) != len(reference) {
		t.Fatalf("paginated %d rows, want %d (matching the unpaginated reference)", len(paginated), len(reference))
	}
	for i := range reference {
		if paginated[i].ID != reference[i].ID {
			t.Fatalf("order mismatch at index %d: reference %s, paginated %s", i, reference[i].ID, paginated[i].ID)
		}
	}
}

func TestGetPublicProductBySlug_activeOnlyInActiveCategory(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-public-get")
	unit := catalogUnit(ctx, t, q, shop.ID, "pcs")
	activeCat := publicCategory(ctx, t, q, shop.ID, "get-active-cat", true)
	inactiveCat := publicCategory(ctx, t, q, shop.ID, "get-inactive-cat", false)

	visible := publicProduct(ctx, t, q, shop.ID, unit.ID, &activeCat.ID, "get-visible", true, false)
	publicProduct(ctx, t, q, shop.ID, unit.ID, &activeCat.ID, "get-inactive-product", false, false)
	publicProduct(ctx, t, q, shop.ID, unit.ID, &inactiveCat.ID, "get-inactive-category", true, false)

	got, err := q.GetPublicProductBySlug(ctx, db.GetPublicProductBySlugParams{ShopID: shop.ID, Locale: "uz", Slug: "get-visible"})
	if err != nil {
		t.Fatalf("GetPublicProductBySlug(visible): %v", err)
	}
	if got.ID != visible.ID || got.CategorySlug != activeCat.Slug {
		t.Fatalf("got %+v, want id=%s categorySlug=%s", got, visible.ID, activeCat.Slug)
	}

	if _, err := q.GetPublicProductBySlug(ctx, db.GetPublicProductBySlugParams{ShopID: shop.ID, Locale: "uz", Slug: "get-inactive-product"}); err == nil {
		t.Fatal("want no row for an inactive product, got one")
	}
	if _, err := q.GetPublicProductBySlug(ctx, db.GetPublicProductBySlugParams{ShopID: shop.ID, Locale: "uz", Slug: "get-inactive-category"}); err == nil {
		t.Fatal("want no row for a product in an inactive category, got one")
	}
}

func TestListPublicCategories_activeOnlyWithProductCount(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-public-categories")
	unit := catalogUnit(ctx, t, q, shop.ID, "pcs")
	activeCat := publicCategory(ctx, t, q, shop.ID, "count-active", true)
	publicCategory(ctx, t, q, shop.ID, "count-inactive", false)

	publicProduct(ctx, t, q, shop.ID, unit.ID, &activeCat.ID, "count-product-1", true, false)
	publicProduct(ctx, t, q, shop.ID, unit.ID, &activeCat.ID, "count-product-2", true, false)
	publicProduct(ctx, t, q, shop.ID, unit.ID, &activeCat.ID, "count-product-inactive", false, false)

	rows, err := q.ListPublicCategories(ctx, db.ListPublicCategoriesParams{ShopID: shop.ID, Locale: "uz"})
	if err != nil {
		t.Fatalf("ListPublicCategories: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != activeCat.ID {
		t.Fatalf("ListPublicCategories = %+v, want exactly the one active category", rows)
	}
	if rows[0].ProductCount != 2 {
		t.Fatalf("ProductCount = %d, want 2 (active products only)", rows[0].ProductCount)
	}
}

func TestSumVariantQtyByProduct_ignoresInactiveLocationsAndHandlesAbsence(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-qty-sum")
	unit := catalogUnit(ctx, t, q, shop.ID, "pcs")
	product := catalogProduct(ctx, t, q, shop.ID, unit.ID, "qty-sum-product")

	stocked := stockVariant(ctx, t, q, shop.ID, product.ID, `{"size":"S"}`)
	neverStocked := stockVariant(ctx, t, q, shop.ID, product.ID, `{"size":"M"}`)

	activeLoc := stockLocation(ctx, t, q, shop.ID, "Active Location")
	inactiveLoc, err := q.CreateLocation(ctx, db.CreateLocationParams{
		ID: uuid.New(), ShopID: shop.ID, Name: "Inactive Location", Kind: db.LocationKindStore, IsActive: false,
	})
	if err != nil {
		t.Fatalf("CreateLocation (inactive): %v", err)
	}

	applyDelta(ctx, t, pool, shop.ID, stocked.ID, activeLoc.ID, db.StockMovementKindPurchaseIn, "5.000")
	applyDelta(ctx, t, pool, shop.ID, stocked.ID, inactiveLoc.ID, db.StockMovementKindPurchaseIn, "100.000")

	rows, err := q.SumVariantQtyByProduct(ctx, db.SumVariantQtyByProductParams{ShopID: shop.ID, ProductID: product.ID})
	if err != nil {
		t.Fatalf("SumVariantQtyByProduct: %v", err)
	}
	byVariant := map[uuid.UUID]db.SumVariantQtyByProductRow{}
	for _, r := range rows {
		byVariant[r.VariantID] = r
	}

	stockedRow, ok := byVariant[stocked.ID]
	if !ok {
		t.Fatal("want a row for the stocked variant, got none")
	}
	if got := numericString(t, stockedRow.Qty); normalizeScale3(got) != "5.000" {
		t.Fatalf("stocked variant qty = %s, want 5.000 (the inactive location's 100 must be ignored)", got)
	}
	if stockedRow.Threshold != 2 {
		t.Fatalf("threshold = %d, want the shop default 2", stockedRow.Threshold)
	}

	neverStockedRow, ok := byVariant[neverStocked.ID]
	if !ok {
		t.Fatal("want a zero-qty row for a variant with no stock_levels row at all, got none (absence must not drop the row)")
	}
	if got := numericString(t, neverStockedRow.Qty); normalizeScale3(got) != "0.000" {
		t.Fatalf("never-stocked variant qty = %s, want 0.000", got)
	}
}

func TestSumVariantQtyForProducts_noNPlusOneAcrossProducts(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-qty-sum-page")
	unit := catalogUnit(ctx, t, q, shop.ID, "pcs")
	loc := stockLocation(ctx, t, q, shop.ID, "Main")

	productA := catalogProduct(ctx, t, q, shop.ID, unit.ID, "qty-sum-page-a")
	productB := catalogProduct(ctx, t, q, shop.ID, unit.ID, "qty-sum-page-b")
	vA := stockVariant(ctx, t, q, shop.ID, productA.ID, "{}")
	vB := stockVariant(ctx, t, q, shop.ID, productB.ID, "{}")

	applyDelta(ctx, t, pool, shop.ID, vA.ID, loc.ID, db.StockMovementKindPurchaseIn, "3.000")
	applyDelta(ctx, t, pool, shop.ID, vB.ID, loc.ID, db.StockMovementKindPurchaseIn, "9.000")

	rows, err := q.SumVariantQtyForProducts(ctx, db.SumVariantQtyForProductsParams{
		ShopID: shop.ID, ProductIds: []uuid.UUID{productA.ID, productB.ID},
	})
	if err != nil {
		t.Fatalf("SumVariantQtyForProducts: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 rows (one per product's single variant), got %d: %+v", len(rows), rows)
	}
	for _, r := range rows {
		switch r.VariantID {
		case vA.ID:
			if r.ProductID != productA.ID || normalizeScale3(numericString(t, r.Qty)) != "3.000" {
				t.Errorf("variant A row = %+v, want productID=%s qty=3.000", r, productA.ID)
			}
		case vB.ID:
			if r.ProductID != productB.ID || normalizeScale3(numericString(t, r.Qty)) != "9.000" {
				t.Errorf("variant B row = %+v, want productID=%s qty=9.000", r, productB.ID)
			}
		default:
			t.Errorf("unexpected variant %s in result", r.VariantID)
		}
	}
}
