package seed_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/catalog"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
	"github.com/Sulton-Ali/savdo/api/internal/media"
	"github.com/Sulton-Ali/savdo/api/internal/seed"
)

// taggedProductSlug and wantTaggedVariantAttrs mirror one fixed entry of
// catalog_data.go's productSpecs — "men-shirt-classic-white" — whose 2nd
// image is tagged to its own 1st variant (size XS, colour oq/white).
// Duplicated here deliberately: catalog_data.go's productSpecs are
// unexported, so this external test package pins the one fact it needs
// (which attributes the tagged image's variant must have) as its own
// fixture, rather than reaching into seed's internals.
const taggedProductSlug = "men-shirt-classic-white"

var wantTaggedVariantAttrs = map[string]string{"size": "XS", "color": "oq"}

// assertTaggedImageMatchesSpecVariant finds productID's one image tagged
// to a variant and asserts that variant's attributes are exactly
// wantTaggedVariantAttrs — the regression this test guards: variants
// created in the same transaction can share created_at (Postgres now()
// is transaction-time), so a positional match between an imageSpec and
// whatever order a list query returned silently tagged the wrong
// variant (confirmed: this exact product's 2nd image landed on size=L
// instead of XS before the fix).
func assertTaggedImageMatchesSpecVariant(ctx context.Context, t *testing.T, q *db.Queries, shopID, productID uuid.UUID) {
	t.Helper()

	images, err := q.ListProductImages(ctx, db.ListProductImagesParams{ShopID: shopID, ProductID: productID})
	if err != nil {
		t.Fatalf("ListProductImages(%s) error = %v", taggedProductSlug, err)
	}
	var variantID *uuid.UUID
	for _, img := range images {
		if img.VariantID != nil {
			variantID = img.VariantID
			break
		}
	}
	if variantID == nil {
		t.Fatalf("product %q: no image is tagged to a variant, want one (catalog_data.go tags its 2nd image)", taggedProductSlug)
	}

	variant, err := q.GetVariantForStaff(ctx, db.GetVariantForStaffParams{ShopID: shopID, ID: *variantID})
	if err != nil {
		t.Fatalf("GetVariantForStaff(%s) error = %v", *variantID, err)
	}
	var attrs map[string]string
	if err := json.Unmarshal(variant.Attributes, &attrs); err != nil {
		t.Fatalf("unmarshal variant %s attributes: %v", variant.ID, err)
	}
	if attrs["size"] != wantTaggedVariantAttrs["size"] || attrs["color"] != wantTaggedVariantAttrs["color"] {
		t.Errorf("product %q: tagged image's variant attributes = %v, want %v", taggedProductSlug, attrs, wantTaggedVariantAttrs)
	}
}

// findProductBySlug locates slug in products, failing the test if it's
// not there — every caller here expects a specific, always-seeded slug.
func findProductBySlug(t *testing.T, products []db.ListProductsForStaffRow, slug string) db.ListProductsForStaffRow {
	t.Helper()
	for _, p := range products {
		if p.Slug == slug {
			return p
		}
	}
	t.Fatalf("product %q not found among %d seeded products", slug, len(products))
	return db.ListProductsForStaffRow{}
}

// newCatalogTestDeps builds the same catalog.Handler + media.Service pair
// runSeedCatalog (cmd/savdo/main.go) wires in production, backed by a
// throwaway on-disk media root (t.TempDir()) instead of MEDIA_DIR. The
// returned *media.LocalStorage is the same instance mediaSvc writes
// through, for tests (TestSeedCatalog_writesRealDerivativeFilesToDisk) that
// need to open a derivative's actual bytes off disk rather than only
// checking the media_files row exists.
func newCatalogTestDeps(t *testing.T, pool *pgxpool.Pool) (*db.Queries, *catalog.Handler, *media.Service, *media.LocalStorage) {
	t.Helper()
	q := db.New(pool)

	storage, err := media.NewLocalStorage(t.TempDir(), "/media")
	if err != nil {
		t.Fatalf("media.NewLocalStorage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })

	mediaSvc := media.NewService(q, storage, "/media", 10<<20, 2, 8)
	catalogSvc := catalog.NewService(pool, q, "uz", "/media")
	return q, catalog.NewHandler(catalogSvc), mediaSvc, storage
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

	q, catalogHandler, mediaSvc, _ := newCatalogTestDeps(t, pool)

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

	taggedProduct := findProductBySlug(t, products, taggedProductSlug)
	assertTaggedImageMatchesSpecVariant(ctx, t, q, shopReport.ShopID, taggedProduct.ID)

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

// countMediaFiles counts every regular file under root, excluding
// media.LocalStorage's own scratch subdirectory (".tmp" — spooled uploads
// that never become a derivative, irrelevant to "did seeding write N
// derivative files").
func countMediaFiles(t *testing.T, root string) int {
	t.Helper()
	count := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if rel == ".tmp" || strings.HasPrefix(rel, ".tmp"+string(filepath.Separator)) {
			return nil
		}
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("walk media root %q: %v", root, err)
	}
	return count
}

// openDerivative opens one of storageKey's three WebP derivatives (suffix
// one of "_thumb", "_card", "_full") through storage — the same Storage
// interface media.Service.Upload wrote it through — by building the key
// through the package's own exported media.URLs (baseURL "/media") and
// stripping that prefix back off, rather than this test hand-rolling its
// own copy of the "<stem><suffix>.webp" naming convention.
func openDerivative(ctx context.Context, t *testing.T, storage *media.LocalStorage, storageKey, suffix string) []byte {
	t.Helper()
	urls := media.URLs("/media", storageKey)
	var url string
	switch suffix {
	case "_thumb":
		url = urls.Thumb
	case "_card":
		url = urls.Card
	case "_full":
		url = urls.Full
	default:
		t.Fatalf("openDerivative: unknown suffix %q", suffix)
	}
	key := strings.TrimPrefix(url, "/media/")

	rc, err := storage.Open(ctx, key)
	if err != nil {
		t.Fatalf("storage.Open(%q) error = %v", key, err)
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read derivative %q: %v", key, err)
	}
	return data
}

// TestSeedCatalog_writesRealDerivativeFilesToDisk is this task's acceptance
// test (D-84): seeding the demo catalogue must write real WebP derivative
// bytes to disk through the media pipeline — not just media_files/
// product_images rows with nothing backing them on disk — and re-seeding
// must not write any more files than the first run did (Catalog's own
// idempotency, checked here at the filesystem level rather than only via
// CatalogReport's counts).
func TestSeedCatalog_writesRealDerivativeFilesToDisk(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()

	shopReport, err := seed.Seed(ctx, pool, seed.DefaultShopSlug)
	if err != nil {
		t.Fatalf("Seed() error = %v", err)
	}

	mediaRoot := t.TempDir()
	storage, err := media.NewLocalStorage(mediaRoot, "/media")
	if err != nil {
		t.Fatalf("media.NewLocalStorage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })

	q := db.New(pool)
	mediaSvc := media.NewService(q, storage, "/media", 10<<20, 2, 8)
	catalogSvc := catalog.NewService(pool, q, "uz", "/media")
	catalogHandler := catalog.NewHandler(catalogSvc)

	owner, err := q.GetOwner(ctx, shopReport.ShopID)
	if err != nil {
		t.Fatalf("GetOwner() error = %v", err)
	}

	first, err := seed.Catalog(ctx, q, catalogHandler, mediaSvc, shopReport.ShopID, owner.ID)
	if err != nil {
		t.Fatalf("first Catalog() error = %v", err)
	}
	if first.ImagesCreated == 0 {
		t.Fatalf("first Catalog() ImagesCreated = 0, want > 0 (nothing to check on disk)")
	}

	products, err := q.ListProductsForStaff(ctx, db.ListProductsForStaffParams{
		Locale: "uz", ShopID: shopReport.ShopID, IncludeInactive: true, Limit: 1000,
	})
	if err != nil {
		t.Fatalf("ListProductsForStaff() error = %v", err)
	}
	target := findProductBySlug(t, products, taggedProductSlug)

	images, err := q.ListProductImages(ctx, db.ListProductImagesParams{ShopID: shopReport.ShopID, ProductID: target.ID})
	if err != nil {
		t.Fatalf("ListProductImages(%s) error = %v", target.Slug, err)
	}
	if len(images) == 0 {
		t.Fatalf("product %q has no images to check on disk", target.Slug)
	}

	mediaFile, err := q.GetMediaFile(ctx, db.GetMediaFileParams{ShopID: shopReport.ShopID, ID: images[0].MediaID})
	if err != nil {
		t.Fatalf("GetMediaFile(%s) error = %v", images[0].MediaID, err)
	}

	for _, suffix := range []string{"_thumb", "_card", "_full"} {
		data := openDerivative(ctx, t, storage, mediaFile.StorageKey, suffix)
		if len(data) == 0 {
			t.Fatalf("derivative %q for %q is empty", suffix, target.Slug)
		}
		cfg, format, decodeErr := image.DecodeConfig(bytes.NewReader(data))
		if decodeErr != nil {
			t.Fatalf("derivative %q for %q: not a decodable image: %v", suffix, target.Slug, decodeErr)
		}
		if format != "webp" {
			t.Errorf("derivative %q for %q: format = %q, want %q", suffix, target.Slug, format, "webp")
		}
		if cfg.Width <= 0 || cfg.Height <= 0 {
			t.Errorf("derivative %q for %q: dimensions = %dx%d, want both > 0", suffix, target.Slug, cfg.Width, cfg.Height)
		}
	}

	filesAfterFirst := countMediaFiles(t, mediaRoot)
	if filesAfterFirst == 0 {
		t.Fatalf("no files under media root %q after seeding, want at least 3 per image", mediaRoot)
	}

	second, err := seed.Catalog(ctx, q, catalogHandler, mediaSvc, shopReport.ShopID, owner.ID)
	if err != nil {
		t.Fatalf("second Catalog() error = %v", err)
	}
	if second != (seed.CatalogReport{}) {
		t.Errorf("second Catalog() = %+v, want a zero report (nothing created, no duplicates)", second)
	}

	filesAfterSecond := countMediaFiles(t, mediaRoot)
	if filesAfterSecond != filesAfterFirst {
		t.Errorf("file count after second seed = %d, want %d (unchanged, no duplicate files)", filesAfterSecond, filesAfterFirst)
	}
}

// ownerAuthContext builds the auth.Context catalog.Handler reads via
// auth.FromContext, for this test's own direct handler calls (mirroring
// seed's own unexported catalogAuthContext) — the shape Middleware would
// attach to an authenticated owner request.
func ownerAuthContext(ctx context.Context, shopID, ownerID uuid.UUID) context.Context {
	return auth.WithContext(ctx, auth.Context{
		ShopID: shopID, UserID: ownerID, Role: db.UserRoleOwner, SessionID: uuid.New(), Client: db.SessionClientWeb,
	})
}

// TestSeedCatalog_repairsMissingImagesOnAnExistingProduct covers the
// review follow-up: a product Catalog() finds already existing (so it
// never re-creates it, and never touches its variants) but which
// currently has zero images gets its spec's images attached anyway,
// counted as ImagesRepaired — e.g. a product created some other way, or
// one whose images were lost from disk without the database rows
// following. Removing every image from an already-seeded product via
// catalogHandler.RemoveProductImage puts it in exactly that state: a
// product that exists with no images, indistinguishable from one that
// was created without them.
func TestSeedCatalog_repairsMissingImagesOnAnExistingProduct(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()

	shopReport, err := seed.Seed(ctx, pool, seed.DefaultShopSlug)
	if err != nil {
		t.Fatalf("Seed() error = %v", err)
	}

	q, catalogHandler, mediaSvc, _ := newCatalogTestDeps(t, pool)

	owner, err := q.GetOwner(ctx, shopReport.ShopID)
	if err != nil {
		t.Fatalf("GetOwner() error = %v", err)
	}

	first, err := seed.Catalog(ctx, q, catalogHandler, mediaSvc, shopReport.ShopID, owner.ID)
	if err != nil {
		t.Fatalf("first Catalog() error = %v", err)
	}
	if first.ImagesRepaired != 0 {
		t.Fatalf("first Catalog() ImagesRepaired = %d, want 0 (nothing to repair on a fresh seed)", first.ImagesRepaired)
	}

	products, err := q.ListProductsForStaff(ctx, db.ListProductsForStaffParams{
		Locale: "uz", ShopID: shopReport.ShopID, IncludeInactive: true, Limit: 1000,
	})
	if err != nil {
		t.Fatalf("ListProductsForStaff() error = %v", err)
	}
	// Target the one product this file also pins a known tagged-variant
	// fixture for (taggedProductSlug), so the post-repair assertion below
	// can check the repaired tagged image landed on the right variant,
	// not just that some image came back.
	target := findProductBySlug(t, products, taggedProductSlug)

	images, err := q.ListProductImages(ctx, db.ListProductImagesParams{ShopID: shopReport.ShopID, ProductID: target.ID})
	if err != nil {
		t.Fatalf("ListProductImages(%s) error = %v", target.Slug, err)
	}
	if len(images) == 0 {
		t.Fatalf("seeded product %q has no images to remove", target.Slug)
	}
	wantRepaired := len(images)

	authCtx := ownerAuthContext(ctx, shopReport.ShopID, owner.ID)
	for _, img := range images {
		if _, err := catalogHandler.RemoveProductImage(authCtx, gen.RemoveProductImageRequestObject{Id: target.ID, ImageId: img.ID}); err != nil {
			t.Fatalf("RemoveProductImage(%s) error = %v", img.ID, err)
		}
	}
	stripped, err := q.ListProductImages(ctx, db.ListProductImagesParams{ShopID: shopReport.ShopID, ProductID: target.ID})
	if err != nil {
		t.Fatalf("ListProductImages(%s) after removal error = %v", target.Slug, err)
	}
	if len(stripped) != 0 {
		t.Fatalf("product %q still has %d images after removal, want 0", target.Slug, len(stripped))
	}

	second, err := seed.Catalog(ctx, q, catalogHandler, mediaSvc, shopReport.ShopID, owner.ID)
	if err != nil {
		t.Fatalf("second Catalog() error = %v", err)
	}
	if second.ProductsCreated != 0 {
		t.Errorf("second Catalog() ProductsCreated = %d, want 0 (product already existed)", second.ProductsCreated)
	}
	if second.ImagesCreated != 0 {
		t.Errorf("second Catalog() ImagesCreated = %d, want 0 (repaired, not newly created)", second.ImagesCreated)
	}
	if second.ImagesRepaired != wantRepaired {
		t.Errorf("second Catalog() ImagesRepaired = %d, want %d", second.ImagesRepaired, wantRepaired)
	}

	repaired, err := q.ListProductImages(ctx, db.ListProductImagesParams{ShopID: shopReport.ShopID, ProductID: target.ID})
	if err != nil {
		t.Fatalf("ListProductImages(%s) after repair error = %v", target.Slug, err)
	}
	if len(repaired) != wantRepaired {
		t.Errorf("product %q has %d images after repair, want %d", target.Slug, len(repaired), wantRepaired)
	}
	assertTaggedImageMatchesSpecVariant(ctx, t, q, shopReport.ShopID, target.ID)

	variantsAfter, err := q.ListVariantsForStaff(ctx, db.ListVariantsForStaffParams{ShopID: shopReport.ShopID, ProductID: target.ID})
	if err != nil {
		t.Fatalf("ListVariantsForStaff(%s) error = %v", target.Slug, err)
	}
	if len(variantsAfter) == 0 {
		t.Errorf("product %q has 0 variants after repair, want its variants untouched", target.Slug)
	}

	third, err := seed.Catalog(ctx, q, catalogHandler, mediaSvc, shopReport.ShopID, owner.ID)
	if err != nil {
		t.Fatalf("third Catalog() error = %v", err)
	}
	if third != (seed.CatalogReport{}) {
		t.Errorf("third Catalog() = %+v, want a zero report (already repaired, nothing left to do)", third)
	}
}
