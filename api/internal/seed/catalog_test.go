package seed_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sulton-Ali/savdo/api/internal/catalog"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
	"github.com/Sulton-Ali/savdo/api/internal/media"
	"github.com/Sulton-Ali/savdo/api/internal/seed"
)

// newCatalogTestDeps builds the same catalog.Handler + media.Service pair
// runSeedCatalog (cmd/savdo/main.go) wires in production, backed by a
// throwaway on-disk media root (t.TempDir()) instead of MEDIA_DIR.
func newCatalogTestDeps(t *testing.T, pool *pgxpool.Pool) (*db.Queries, *catalog.Handler, *media.Service) {
	t.Helper()
	q := db.New(pool)

	storage, err := media.NewLocalStorage(t.TempDir(), "/media")
	if err != nil {
		t.Fatalf("media.NewLocalStorage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })

	mediaSvc := media.NewService(q, storage, "/media", 10<<20, 2, 8)
	catalogSvc := catalog.NewService(pool, q, "uz", "/media")
	return q, catalog.NewHandler(catalogSvc), mediaSvc
}

// TestSeedCatalog_seedsARealisticCatalogueIdempotently is this task's
// acceptance test (docs/06-ROADMAP.md Phase 2 T5): seed twice and assert
// counts — units, attribute definitions, categories and products land at
// their designed totals on the first call, every product has at least one
// variant and one image, and the second call creates nothing (no
// duplicates).
func TestSeedCatalog_seedsARealisticCatalogueIdempotently(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()

	shopReport, err := seed.Seed(ctx, pool, seed.DefaultShopSlug)
	if err != nil {
		t.Fatalf("Seed() error = %v", err)
	}

	q, catalogHandler, mediaSvc := newCatalogTestDeps(t, pool)

	owner, err := q.GetOwner(ctx, shopReport.ShopID)
	if err != nil {
		t.Fatalf("GetOwner() error = %v", err)
	}

	// The catalogue's designed shape (catalog_data.go): 4 units, size +
	// colour attribute definitions, 3 top-level categories with 5
	// children between them (8 total), 30 products, and the exact
	// variant/image totals catalog_data.go's productSpecs sum to.
	const (
		wantUnits      = 4
		wantAttributes = 2
		wantCategories = 8
		wantProducts   = 30
		wantVariants   = 135
		wantImages     = 61
	)

	first, err := seed.Catalog(ctx, q, catalogHandler, mediaSvc, shopReport.ShopID, owner.ID)
	if err != nil {
		t.Fatalf("first Catalog() error = %v", err)
	}
	if first.UnitsCreated != wantUnits {
		t.Errorf("UnitsCreated = %d, want %d", first.UnitsCreated, wantUnits)
	}
	if first.AttributesCreated != wantAttributes {
		t.Errorf("AttributesCreated = %d, want %d", first.AttributesCreated, wantAttributes)
	}
	if first.CategoriesCreated != wantCategories {
		t.Errorf("CategoriesCreated = %d, want %d", first.CategoriesCreated, wantCategories)
	}
	if first.ProductsCreated != wantProducts {
		t.Errorf("ProductsCreated = %d, want %d", first.ProductsCreated, wantProducts)
	}
	if first.VariantsCreated != wantVariants {
		t.Errorf("VariantsCreated = %d, want %d", first.VariantsCreated, wantVariants)
	}
	if first.ImagesCreated != wantImages {
		t.Errorf("ImagesCreated = %d, want %d", first.ImagesCreated, wantImages)
	}

	units, err := q.ListUnits(ctx, db.ListUnitsParams{Locale: "uz", ShopID: shopReport.ShopID})
	if err != nil {
		t.Fatalf("ListUnits() error = %v", err)
	}
	if len(units) != wantUnits {
		t.Errorf("len(units) = %d, want %d", len(units), wantUnits)
	}

	attrs, err := q.ListAttributeDefinitions(ctx, db.ListAttributeDefinitionsParams{Locale: "uz", ShopID: shopReport.ShopID})
	if err != nil {
		t.Fatalf("ListAttributeDefinitions() error = %v", err)
	}
	if len(attrs) != wantAttributes {
		t.Errorf("len(attributeDefinitions) = %d, want %d", len(attrs), wantAttributes)
	}

	cats, err := q.ListCategories(ctx, db.ListCategoriesParams{Locale: "uz", ShopID: shopReport.ShopID, IncludeInactive: true})
	if err != nil {
		t.Fatalf("ListCategories() error = %v", err)
	}
	if len(cats) != wantCategories {
		t.Errorf("len(categories) = %d, want %d", len(cats), wantCategories)
	}

	products, err := q.ListProductsForStaff(ctx, db.ListProductsForStaffParams{
		Locale: "uz", ShopID: shopReport.ShopID, IncludeInactive: true, Limit: 1000,
	})
	if err != nil {
		t.Fatalf("ListProductsForStaff() error = %v", err)
	}
	if len(products) != wantProducts {
		t.Fatalf("len(products) = %d, want %d", len(products), wantProducts)
	}

	totalVariants, totalImages := 0, 0
	for _, p := range products {
		variants, err := q.ListVariantsForStaff(ctx, db.ListVariantsForStaffParams{ShopID: shopReport.ShopID, ProductID: p.ID})
		if err != nil {
			t.Fatalf("ListVariantsForStaff(%s) error = %v", p.Slug, err)
		}
		if len(variants) < 1 {
			t.Errorf("product %q has %d variants, want >= 1", p.Slug, len(variants))
		}
		totalVariants += len(variants)

		images, err := q.ListProductImages(ctx, db.ListProductImagesParams{ShopID: shopReport.ShopID, ProductID: p.ID})
		if err != nil {
			t.Fatalf("ListProductImages(%s) error = %v", p.Slug, err)
		}
		if len(images) < 1 {
			t.Errorf("product %q has %d images, want >= 1", p.Slug, len(images))
		}
		totalImages += len(images)
	}
	if totalVariants != wantVariants {
		t.Errorf("total variants in DB = %d, want %d", totalVariants, wantVariants)
	}
	if totalImages != wantImages {
		t.Errorf("total images in DB = %d, want %d", totalImages, wantImages)
	}

	second, err := seed.Catalog(ctx, q, catalogHandler, mediaSvc, shopReport.ShopID, owner.ID)
	if err != nil {
		t.Fatalf("second Catalog() error = %v", err)
	}
	if second != (seed.CatalogReport{}) {
		t.Errorf("second Catalog() = %+v, want a zero report (nothing created, no duplicates)", second)
	}

	productsAfter, err := q.ListProductsForStaff(ctx, db.ListProductsForStaffParams{
		Locale: "uz", ShopID: shopReport.ShopID, IncludeInactive: true, Limit: 1000,
	})
	if err != nil {
		t.Fatalf("ListProductsForStaff() after second seed error = %v", err)
	}
	if len(productsAfter) != wantProducts {
		t.Errorf("len(products) after second seed = %d, want %d (no duplicates)", len(productsAfter), wantProducts)
	}
}
