package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

// numeric parses a decimal literal into a valid pgtype.Numeric, the way a
// caller supplying a NUMERIC(14,2) parameter would. Never build these from
// float64 (§ 04-DATA-MODEL.md rule 3).
func numeric(t *testing.T, s string) pgtype.Numeric {
	t.Helper()
	var n pgtype.Numeric
	if err := n.Scan(s); err != nil {
		t.Fatalf("numeric(%q): %v", s, err)
	}
	return n
}

func catalogShop(ctx context.Context, t *testing.T, q *db.Queries, slug string) db.Shop {
	t.Helper()
	shop, err := q.CreateShop(ctx, db.CreateShopParams{ID: uuid.New(), Slug: slug, Name: "Catalog Shop " + slug})
	if err != nil {
		t.Fatalf("CreateShop(%q): %v", slug, err)
	}
	return shop
}

func catalogUnit(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, code string) db.Unit {
	t.Helper()
	unit, err := q.UpsertUnit(ctx, db.UpsertUnitParams{ID: uuid.New(), ShopID: shopID, Code: code, Precision: 0})
	if err != nil {
		t.Fatalf("UpsertUnit(%q): %v", code, err)
	}
	return unit
}

func catalogProduct(ctx context.Context, t *testing.T, q *db.Queries, shopID, unitID uuid.UUID, slug string) db.Product {
	t.Helper()
	p, err := q.CreateProduct(ctx, db.CreateProductParams{
		ID: uuid.New(), ShopID: shopID, UnitID: unitID, Slug: slug,
		BasePrice: numeric(t, "125000.00"), IsActive: true,
	})
	if err != nil {
		t.Fatalf("CreateProduct(%q): %v", slug, err)
	}
	return p
}

func catalogMedia(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, storageKey string, sha [32]byte) db.MediaFile {
	t.Helper()
	m, err := q.CreateMediaFile(ctx, db.CreateMediaFileParams{
		ID: uuid.New(), ShopID: shopID, StorageKey: storageKey, Mime: "image/webp",
		SizeBytes: 1024, Sha256: sha[:],
	})
	if err != nil {
		t.Fatalf("CreateMediaFile(%q): %v", storageKey, err)
	}
	return m
}

func TestProducts_slugUniquePerShopNotGlobally(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shopA := catalogShop(ctx, t, q, "shop-a")
	shopB := catalogShop(ctx, t, q, "shop-b")
	unitA := catalogUnit(ctx, t, q, shopA.ID, "pcs")
	unitB := catalogUnit(ctx, t, q, shopB.ID, "pcs")

	catalogProduct(ctx, t, q, shopA.ID, unitA.ID, "cotton-shirt")

	if _, err := q.CreateProduct(ctx, db.CreateProductParams{
		ID: uuid.New(), ShopID: shopB.ID, UnitID: unitB.ID, Slug: "cotton-shirt",
		BasePrice: numeric(t, "99000.00"), IsActive: true,
	}); err != nil {
		t.Fatalf("want the same slug to succeed in a different shop, got: %v", err)
	}

	if _, err := q.CreateProduct(ctx, db.CreateProductParams{
		ID: uuid.New(), ShopID: shopA.ID, UnitID: unitA.ID, Slug: "cotton-shirt",
		BasePrice: numeric(t, "150000.00"), IsActive: true,
	}); err == nil {
		t.Fatal("want a uniqueness error inserting the same slug twice in shop A, got none")
	}
}

func TestProductVariants_attributesUniquePerProduct(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-variants")
	unit := catalogUnit(ctx, t, q, shop.ID, "pcs")
	product := catalogProduct(ctx, t, q, shop.ID, unit.ID, "hoodie")

	large := json.RawMessage(`{"size":"L"}`)
	medium := json.RawMessage(`{"size":"M"}`)

	if _, err := q.CreateVariant(ctx, db.CreateVariantParams{
		ID: uuid.New(), ShopID: shop.ID, ProductID: product.ID, Attributes: large, IsActive: true,
	}); err != nil {
		t.Fatalf("create first variant: %v", err)
	}
	if _, err := q.CreateVariant(ctx, db.CreateVariantParams{
		ID: uuid.New(), ShopID: shop.ID, ProductID: product.ID, Attributes: medium, IsActive: true,
	}); err != nil {
		t.Fatalf("want a different attributes value to succeed on the same product, got: %v", err)
	}
	_, err := q.CreateVariant(ctx, db.CreateVariantParams{
		ID: uuid.New(), ShopID: shop.ID, ProductID: product.ID, Attributes: large, IsActive: true,
	})
	if err == nil {
		t.Fatal("want a uniqueness error inserting the same (product_id, attributes) twice, got none")
	}
	// The conflict must resolve to product_variants_product_id_attributes_key
	// by name — catalog.conflictField (api/internal/catalog/errors.go) maps
	// that exact constraint name to the "attributes" API field, and the
	// migration 0008 rewrite (table constraint -> partial unique index) is
	// only safe if the name survives unchanged.
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("want a pg unique_violation (23505), got: %v", err)
	}
	if pgErr.ConstraintName != "product_variants_product_id_attributes_key" {
		t.Fatalf("ConstraintName = %q, want product_variants_product_id_attributes_key", pgErr.ConstraintName)
	}
}

// TestProductVariants_softDeletedAttributesCanBeReused pins the reason
// migration 0008 turned product_variants_product_id_attributes_key from a
// plain table UNIQUE(product_id, attributes) into a partial unique index
// WHERE deleted_at IS NULL: without the partial predicate, soft-deleting a
// variant (e.g. the "L" size of a hoodie) would permanently block ever
// recreating that same combination on the product, even though the
// deleted row is no longer a live variant.
func TestProductVariants_softDeletedAttributesCanBeReused(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-variants-soft-delete")
	unit := catalogUnit(ctx, t, q, shop.ID, "pcs")
	product := catalogProduct(ctx, t, q, shop.ID, unit.ID, "hoodie-soft-delete")

	large := json.RawMessage(`{"size":"L"}`)

	v1, err := q.CreateVariant(ctx, db.CreateVariantParams{
		ID: uuid.New(), ShopID: shop.ID, ProductID: product.ID, Attributes: large, IsActive: true,
	})
	if err != nil {
		t.Fatalf("create first variant: %v", err)
	}

	// Two live variants with the same attributes must still conflict.
	if _, err := q.CreateVariant(ctx, db.CreateVariantParams{
		ID: uuid.New(), ShopID: shop.ID, ProductID: product.ID, Attributes: large, IsActive: true,
	}); err == nil {
		t.Fatal("want a uniqueness error inserting a second live variant with the same attributes, got none")
	}

	if err := q.SoftDeleteVariant(ctx, db.SoftDeleteVariantParams{ShopID: shop.ID, ID: v1.ID}); err != nil {
		t.Fatalf("SoftDeleteVariant: %v", err)
	}

	// Now that the only variant with these attributes is soft-deleted, the
	// same combination must be free to reuse on the same product.
	if _, err := q.CreateVariant(ctx, db.CreateVariantParams{
		ID: uuid.New(), ShopID: shop.ID, ProductID: product.ID, Attributes: large, IsActive: true,
	}); err != nil {
		t.Fatalf("want re-creating the same attributes after a soft delete to succeed, got: %v", err)
	}
}

func TestUpdateVariantAttributes_inPlaceRewriteRespectsUniqueIndex(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-variant-attrs")
	unit := catalogUnit(ctx, t, q, shop.ID, "pcs")
	product := catalogProduct(ctx, t, q, shop.ID, unit.ID, "shirt")

	small := json.RawMessage(`{"size":"S"}`)
	large := json.RawMessage(`{"size":"L"}`)

	v1, err := q.CreateVariant(ctx, db.CreateVariantParams{
		ID: uuid.New(), ShopID: shop.ID, ProductID: product.ID, Attributes: small, IsActive: true,
	})
	if err != nil {
		t.Fatalf("create variant 1: %v", err)
	}
	v2, err := q.CreateVariant(ctx, db.CreateVariantParams{
		ID: uuid.New(), ShopID: shop.ID, ProductID: product.ID, Attributes: large, IsActive: true,
	})
	if err != nil {
		t.Fatalf("create variant 2: %v", err)
	}

	medium := json.RawMessage(`{"size":"M"}`)
	updated, err := q.UpdateVariantAttributes(ctx, db.UpdateVariantAttributesParams{
		ShopID: shop.ID, ID: v1.ID, Attributes: medium,
	})
	if err != nil {
		t.Fatalf("UpdateVariantAttributes to an unused value: %v", err)
	}
	var got, want map[string]any
	if err := json.Unmarshal(updated.Attributes, &got); err != nil {
		t.Fatalf("unmarshal returned attributes: %v", err)
	}
	if err := json.Unmarshal(medium, &want); err != nil {
		t.Fatalf("unmarshal expected attributes: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Attributes = %s, want %s", updated.Attributes, medium)
	}
	if updated.UpdatedAt.Equal(v1.UpdatedAt) || !updated.UpdatedAt.After(v1.UpdatedAt) {
		t.Fatalf("UpdatedAt = %v, want after original %v", updated.UpdatedAt, v1.UpdatedAt)
	}

	// Rewriting v1's attributes onto v2's existing value must hit the
	// (product_id, attributes) unique index, not silently succeed.
	if _, err := q.UpdateVariantAttributes(ctx, db.UpdateVariantAttributesParams{
		ShopID: shop.ID, ID: v1.ID, Attributes: large,
	}); err == nil {
		t.Fatal("want a uniqueness error rewriting onto another variant's attributes, got none")
	}

	// Wrong shop_id must not find the row (cross-tenant isolation).
	otherShop := catalogShop(ctx, t, q, "shop-variant-attrs-other")
	if _, err := q.UpdateVariantAttributes(ctx, db.UpdateVariantAttributesParams{
		ShopID: otherShop.ID, ID: v2.ID, Attributes: json.RawMessage(`{"size":"XL"}`),
	}); err == nil {
		t.Fatal("want no row found updating a variant under the wrong shop_id, got none")
	}
}

func TestProductImages_onlyOneCoverPerProduct(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-images")
	unit := catalogUnit(ctx, t, q, shop.ID, "pcs")
	product := catalogProduct(ctx, t, q, shop.ID, unit.ID, "jacket")
	media1 := catalogMedia(ctx, t, q, shop.ID, "jacket-1.webp", [32]byte{1})
	media2 := catalogMedia(ctx, t, q, shop.ID, "jacket-2.webp", [32]byte{2})

	if _, err := q.AddProductImage(ctx, db.AddProductImageParams{
		ID: uuid.New(), ShopID: shop.ID, ProductID: product.ID, MediaID: media1.ID, IsCover: true,
	}); err != nil {
		t.Fatalf("add first cover image: %v", err)
	}
	if _, err := q.AddProductImage(ctx, db.AddProductImageParams{
		ID: uuid.New(), ShopID: shop.ID, ProductID: product.ID, MediaID: media2.ID, IsCover: false,
	}); err != nil {
		t.Fatalf("want a non-cover second image to succeed, got: %v", err)
	}
	if _, err := q.AddProductImage(ctx, db.AddProductImageParams{
		ID: uuid.New(), ShopID: shop.ID, ProductID: product.ID, MediaID: media2.ID, IsCover: true,
	}); err == nil {
		t.Fatal("want a uniqueness error inserting a second cover image for the same product, got none")
	}
}

func TestMediaFiles_sha256UniquePerShopNotGlobally(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shopA := catalogShop(ctx, t, q, "shop-media-a")
	shopB := catalogShop(ctx, t, q, "shop-media-b")
	sha := [32]byte{9, 9, 9}

	catalogMedia(ctx, t, q, shopA.ID, "a.webp", sha)

	if _, err := q.CreateMediaFile(ctx, db.CreateMediaFileParams{
		ID: uuid.New(), ShopID: shopB.ID, StorageKey: "b.webp", Mime: "image/webp",
		SizeBytes: 1024, Sha256: sha[:],
	}); err != nil {
		t.Fatalf("want the same sha256 to succeed in a different shop, got: %v", err)
	}
	if _, err := q.CreateMediaFile(ctx, db.CreateMediaFileParams{
		ID: uuid.New(), ShopID: shopA.ID, StorageKey: "a-dup.webp", Mime: "image/webp",
		SizeBytes: 1024, Sha256: sha[:],
	}); err == nil {
		t.Fatal("want a uniqueness error inserting the same sha256 twice in shop A, got none")
	}
}

func TestProductTranslations_trigramIndexExists(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	var indexdef string
	err := pool.QueryRow(ctx, `
		SELECT indexdef FROM pg_indexes
		WHERE schemaname = 'public' AND tablename = 'product_translations'
			AND indexname = 'product_translations_name_trgm_idx'
	`).Scan(&indexdef)
	if err != nil {
		t.Fatalf("product_translations_name_trgm_idx not found: %v", err)
	}
	if !strings.Contains(indexdef, "USING gin") || !strings.Contains(indexdef, "gin_trgm_ops") {
		t.Fatalf("indexdef = %q, want a GIN index using gin_trgm_ops", indexdef)
	}
}

// ListProductsForCashierRow must never carry a cost field — compile-time
// enforcement isn't possible for "a struct lacks a field", so this asserts
// it by reflection instead. Same for ListProductsPublicRow. This is the
// literal test the ADR-010 / § 8 rule 8 hard rule asks for.
func TestListProductsForCashierAndPublic_haveNoCostFields(t *testing.T) {
	assertNoCostField := func(t *testing.T, v any) {
		t.Helper()
		typ := reflect.TypeOf(v)
		for i := 0; i < typ.NumField(); i++ {
			name := typ.Field(i).Name
			if strings.Contains(strings.ToLower(name), "cost") {
				t.Fatalf("%s has field %q — cost must never reach a cashier or public response", typ.Name(), name)
			}
		}
	}
	assertNoCostField(t, db.ListProductsForCashierRow{})
	assertNoCostField(t, db.ListProductsPublicRow{})
	assertNoCostField(t, db.GetProductForCashierRow{})
	assertNoCostField(t, db.GetProductPublicRow{})
	assertNoCostField(t, db.ListVariantsForCashierRow{})
	assertNoCostField(t, db.GetVariantForCashierRow{})

	// Sanity check the assertion itself isn't vacuous: the staff row DOES
	// carry cost, so a regression that strips CostPrice from the schema
	// entirely (rather than just from the cashier queries) would still be
	// caught by other tests, but this pins the staff side of the contrast.
	staffType := reflect.TypeOf(db.ListProductsForStaffRow{})
	if _, ok := staffType.FieldByName("CostPrice"); !ok {
		t.Fatal("ListProductsForStaffRow should carry CostPrice — staff is allowed to see cost")
	}
}

func TestCountActiveVariants_scopedToShop(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shopA := catalogShop(ctx, t, q, "shop-count-variants-a")
	shopB := catalogShop(ctx, t, q, "shop-count-variants-b")
	unitA := catalogUnit(ctx, t, q, shopA.ID, "pcs")
	productA := catalogProduct(ctx, t, q, shopA.ID, unitA.ID, "count-variants-a")

	if _, err := q.CreateVariant(ctx, db.CreateVariantParams{
		ID: uuid.New(), ShopID: shopA.ID, ProductID: productA.ID, Attributes: json.RawMessage(`{}`), IsActive: true,
	}); err != nil {
		t.Fatalf("CreateVariant: %v", err)
	}

	count, err := q.CountActiveVariants(ctx, db.CountActiveVariantsParams{ShopID: shopA.ID, ProductID: productA.ID})
	if err != nil {
		t.Fatalf("CountActiveVariants(shop A): %v", err)
	}
	if count != 1 {
		t.Fatalf("CountActiveVariants(shop A) = %d, want 1", count)
	}

	// Same product_id, wrong shop_id: hard rule 1 says every query filters
	// by shop_id, so this must count 0, not 1.
	count, err = q.CountActiveVariants(ctx, db.CountActiveVariantsParams{ShopID: shopB.ID, ProductID: productA.ID})
	if err != nil {
		t.Fatalf("CountActiveVariants(shop B): %v", err)
	}
	if count != 0 {
		t.Fatalf("CountActiveVariants(shop B) = %d, want 0 — a variant must not count under the wrong shop_id", count)
	}
}

func TestCountProductImages_scopedToShop(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shopA := catalogShop(ctx, t, q, "shop-count-images-a")
	shopB := catalogShop(ctx, t, q, "shop-count-images-b")
	unitA := catalogUnit(ctx, t, q, shopA.ID, "pcs")
	productA := catalogProduct(ctx, t, q, shopA.ID, unitA.ID, "count-images-a")
	mediaA := catalogMedia(ctx, t, q, shopA.ID, "count-images-a.webp", [32]byte{7})

	if _, err := q.AddProductImage(ctx, db.AddProductImageParams{
		ID: uuid.New(), ShopID: shopA.ID, ProductID: productA.ID, MediaID: mediaA.ID,
	}); err != nil {
		t.Fatalf("AddProductImage: %v", err)
	}

	count, err := q.CountProductImages(ctx, db.CountProductImagesParams{ShopID: shopA.ID, ProductID: productA.ID})
	if err != nil {
		t.Fatalf("CountProductImages(shop A): %v", err)
	}
	if count != 1 {
		t.Fatalf("CountProductImages(shop A) = %d, want 1", count)
	}

	count, err = q.CountProductImages(ctx, db.CountProductImagesParams{ShopID: shopB.ID, ProductID: productA.ID})
	if err != nil {
		t.Fatalf("CountProductImages(shop B): %v", err)
	}
	if count != 0 {
		t.Fatalf("CountProductImages(shop B) = %d, want 0 — an image must not count under the wrong shop_id", count)
	}
}

func TestGetCategoryDepth_missingCategoryReturnsZero(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-depth-missing")

	depth, err := q.GetCategoryDepth(ctx, db.GetCategoryDepthParams{ShopID: shop.ID, CategoryID: uuid.New()})
	if err != nil {
		t.Fatalf("GetCategoryDepth on a non-existent category must not fail (NULL from an empty aggregate): %v", err)
	}
	if depth != 0 {
		t.Fatalf("GetCategoryDepth(missing) = %d, want 0", depth)
	}
}

// Zero-translation rows must list/get with an empty locale_used/name instead
// of failing to scan — LEFT JOIN LATERAL leaves t.locale/t.name NULL when no
// translation matches, and sqlc does not infer that as nullable, so every
// fallback query projects COALESCE(..., ''). One test per entity that has a
// LATERAL fallback query, per the review that reproduced the crash.

func TestCategories_zeroTranslations_listsAndGetsWithEmptyLocale(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-cat-empty")
	cat, err := q.CreateCategory(ctx, db.CreateCategoryParams{
		ID: uuid.New(), ShopID: shop.ID, Slug: "no-translation-yet", IsActive: true,
	})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	rows, err := q.ListCategories(ctx, db.ListCategoriesParams{ShopID: shop.ID, Locale: "uz", IncludeInactive: true})
	if err != nil {
		t.Fatalf("ListCategories on an untranslated category must not fail to scan: %v", err)
	}
	if len(rows) != 1 || rows[0].LocaleUsed != "" || rows[0].Name != "" {
		t.Fatalf("want one row with empty locale_used/name, got %+v", rows)
	}

	got, err := q.GetCategory(ctx, db.GetCategoryParams{ShopID: shop.ID, ID: cat.ID, Locale: "uz"})
	if err != nil {
		t.Fatalf("GetCategory on an untranslated category must not fail to scan: %v", err)
	}
	if got.LocaleUsed != "" || got.Name != "" {
		t.Fatalf("want empty locale_used/name, got locale_used=%q name=%q", got.LocaleUsed, got.Name)
	}
}

func TestUnits_zeroTranslations_listsWithEmptyLocale(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-unit-empty")
	catalogUnit(ctx, t, q, shop.ID, "pcs")

	rows, err := q.ListUnits(ctx, db.ListUnitsParams{ShopID: shop.ID, Locale: "uz"})
	if err != nil {
		t.Fatalf("ListUnits on an untranslated unit must not fail to scan: %v", err)
	}
	if len(rows) != 1 || rows[0].LocaleUsed != "" || rows[0].Name != "" {
		t.Fatalf("want one row with empty locale_used/name, got %+v", rows)
	}
}

func TestAttributeDefinitions_zeroTranslations_listsWithEmptyLocale(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-attr-empty")
	if _, err := q.CreateAttributeDefinition(ctx, db.CreateAttributeDefinitionParams{
		ID: uuid.New(), ShopID: shop.ID, Code: "size",
	}); err != nil {
		t.Fatalf("CreateAttributeDefinition: %v", err)
	}

	rows, err := q.ListAttributeDefinitions(ctx, db.ListAttributeDefinitionsParams{ShopID: shop.ID, Locale: "uz"})
	if err != nil {
		t.Fatalf("ListAttributeDefinitions on an untranslated attribute must not fail to scan: %v", err)
	}
	if len(rows) != 1 || rows[0].LocaleUsed != "" || rows[0].Name != "" {
		t.Fatalf("want one row with empty locale_used/name, got %+v", rows)
	}
	if string(rows[0].Translations) != "{}" {
		t.Fatalf("Translations = %s, want {}", rows[0].Translations)
	}
}

func TestProducts_zeroTranslations_listAndGetWithEmptyLocale(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-product-empty")
	unit := catalogUnit(ctx, t, q, shop.ID, "pcs")
	product := catalogProduct(ctx, t, q, shop.ID, unit.ID, "no-translation-yet")

	staffRows, err := q.ListProductsForStaff(ctx, db.ListProductsForStaffParams{ShopID: shop.ID, Locale: "uz", Limit: 10})
	if err != nil {
		t.Fatalf("ListProductsForStaff on an untranslated product must not fail to scan: %v", err)
	}
	if len(staffRows) != 1 || staffRows[0].LocaleUsed != "" || staffRows[0].Name != "" {
		t.Fatalf("ListProductsForStaff: want one row with empty locale_used/name, got %+v", staffRows)
	}

	cashierRows, err := q.ListProductsForCashier(ctx, db.ListProductsForCashierParams{ShopID: shop.ID, Locale: "uz", Limit: 10})
	if err != nil {
		t.Fatalf("ListProductsForCashier on an untranslated product must not fail to scan: %v", err)
	}
	if len(cashierRows) != 1 || cashierRows[0].LocaleUsed != "" || cashierRows[0].Name != "" {
		t.Fatalf("ListProductsForCashier: want one row with empty locale_used/name, got %+v", cashierRows)
	}

	publicRows, err := q.ListProductsPublic(ctx, db.ListProductsPublicParams{ShopID: shop.ID, Locale: "uz", Limit: 10})
	if err != nil {
		t.Fatalf("ListProductsPublic on an untranslated product must not fail to scan: %v", err)
	}
	if len(publicRows) != 1 || publicRows[0].LocaleUsed != "" || publicRows[0].Name != "" {
		t.Fatalf("ListProductsPublic: want one row with empty locale_used/name, got %+v", publicRows)
	}

	staffGet, err := q.GetProductForStaff(ctx, db.GetProductForStaffParams{ShopID: shop.ID, ID: product.ID, Locale: "uz"})
	if err != nil {
		t.Fatalf("GetProductForStaff on an untranslated product must not fail to scan: %v", err)
	}
	if staffGet.LocaleUsed != "" || staffGet.Name != "" {
		t.Fatalf("GetProductForStaff: want empty locale_used/name, got locale_used=%q name=%q", staffGet.LocaleUsed, staffGet.Name)
	}

	cashierGet, err := q.GetProductForCashier(ctx, db.GetProductForCashierParams{ShopID: shop.ID, ID: product.ID, Locale: "uz"})
	if err != nil {
		t.Fatalf("GetProductForCashier on an untranslated product must not fail to scan: %v", err)
	}
	if cashierGet.LocaleUsed != "" || cashierGet.Name != "" {
		t.Fatalf("GetProductForCashier: want empty locale_used/name, got locale_used=%q name=%q", cashierGet.LocaleUsed, cashierGet.Name)
	}

	publicGet, err := q.GetProductPublic(ctx, db.GetProductPublicParams{ShopID: shop.ID, ID: product.ID, Locale: "uz"})
	if err != nil {
		t.Fatalf("GetProductPublic on an untranslated product must not fail to scan: %v", err)
	}
	if publicGet.LocaleUsed != "" || publicGet.Name != "" {
		t.Fatalf("GetProductPublic: want empty locale_used/name, got locale_used=%q name=%q", publicGet.LocaleUsed, publicGet.Name)
	}
}

func TestListUnits_localeFallback_returnsLocaleUsed(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-locale")
	pcs := catalogUnit(ctx, t, q, shop.ID, "pcs")
	kg := catalogUnit(ctx, t, q, shop.ID, "kg")

	if err := q.UpsertUnitTranslation(ctx, db.UpsertUnitTranslationParams{UnitID: pcs.ID, Locale: "uz", Name: "Dona"}); err != nil {
		t.Fatalf("translate pcs uz: %v", err)
	}
	if err := q.UpsertUnitTranslation(ctx, db.UpsertUnitTranslationParams{UnitID: pcs.ID, Locale: "ru", Name: "Штука"}); err != nil {
		t.Fatalf("translate pcs ru: %v", err)
	}
	// kg has only a ru translation — no uz, no exact match for "en" either.
	if err := q.UpsertUnitTranslation(ctx, db.UpsertUnitTranslationParams{UnitID: kg.ID, Locale: "ru", Name: "Килограмм"}); err != nil {
		t.Fatalf("translate kg ru: %v", err)
	}

	byCode := func(rows []db.ListUnitsRow, code string) db.ListUnitsRow {
		t.Helper()
		for _, r := range rows {
			if r.Code == code {
				return r
			}
		}
		t.Fatalf("no row with code %q", code)
		return db.ListUnitsRow{}
	}

	// Exact match: requested locale exists.
	rows, err := q.ListUnits(ctx, db.ListUnitsParams{ShopID: shop.ID, Locale: "ru"})
	if err != nil {
		t.Fatalf("ListUnits(ru): %v", err)
	}
	row := byCode(rows, "pcs")
	if row.LocaleUsed != "ru" || row.Name != "Штука" {
		t.Fatalf("exact match: got locale_used=%q name=%q, want ru/Штука", row.LocaleUsed, row.Name)
	}

	// Fallback tier 2: requested locale missing, 'uz' exists.
	rows, err = q.ListUnits(ctx, db.ListUnitsParams{ShopID: shop.ID, Locale: "en"})
	if err != nil {
		t.Fatalf("ListUnits(en): %v", err)
	}
	row = byCode(rows, "pcs")
	if row.LocaleUsed != "uz" || row.Name != "Dona" {
		t.Fatalf("uz fallback: got locale_used=%q name=%q, want uz/Dona", row.LocaleUsed, row.Name)
	}

	// Fallback tier 3: requested locale and 'uz' both missing, falls back to
	// whatever exists ("any" — here the only row, ru).
	row = byCode(rows, "kg")
	if row.LocaleUsed != "ru" || row.Name != "Килограмм" {
		t.Fatalf("any fallback: got locale_used=%q name=%q, want ru/Килограмм", row.LocaleUsed, row.Name)
	}
}

func TestListCategoryTranslations_returnsAllLocalesOrdered(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-cat-translations")
	cat, err := q.CreateCategory(ctx, db.CreateCategoryParams{
		ID: uuid.New(), ShopID: shop.ID, Slug: "outerwear", IsActive: true,
	})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	desc := "Куртки и пальто"
	if err := q.UpsertCategoryTranslation(ctx, db.UpsertCategoryTranslationParams{
		CategoryID: cat.ID, Locale: "ru", Name: "Верхняя одежда", Description: &desc,
	}); err != nil {
		t.Fatalf("upsert ru translation: %v", err)
	}
	if err := q.UpsertCategoryTranslation(ctx, db.UpsertCategoryTranslationParams{
		CategoryID: cat.ID, Locale: "uz", Name: "Ustki kiyim",
	}); err != nil {
		t.Fatalf("upsert uz translation: %v", err)
	}

	rows, err := q.ListCategoryTranslations(ctx, db.ListCategoryTranslationsParams{CategoryID: cat.ID, ShopID: shop.ID})
	if err != nil {
		t.Fatalf("ListCategoryTranslations: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 translations, got %d: %+v", len(rows), rows)
	}
	// ORDER BY locale: "ru" < "uz".
	if rows[0].Locale != "ru" || rows[0].Name != "Верхняя одежда" || rows[0].Description == nil || *rows[0].Description != desc {
		t.Fatalf("row 0 = %+v, want ru/Верхняя одежда/%q", rows[0], desc)
	}
	if rows[1].Locale != "uz" || rows[1].Name != "Ustki kiyim" || rows[1].Description != nil {
		t.Fatalf("row 1 = %+v, want uz/Ustki kiyim/nil description", rows[1])
	}
}

// The LATERAL fallback's description column must come through as a real
// NULL (*string == nil), not the '' sentinel used for name/locale_used —
// description is nullable at the schema level, unlike name, so an absent
// translation and a translation with no description must be
// distinguishable from "has a description of the empty string".
func TestGetCategoryAndListCategories_descriptionIsNullableNotEmptyString(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-cat-desc")

	withDesc, err := q.CreateCategory(ctx, db.CreateCategoryParams{
		ID: uuid.New(), ShopID: shop.ID, Slug: "with-desc", IsActive: true,
	})
	if err != nil {
		t.Fatalf("CreateCategory(with-desc): %v", err)
	}
	desc := "Electronics and gadgets"
	if err := q.UpsertCategoryTranslation(ctx, db.UpsertCategoryTranslationParams{
		CategoryID: withDesc.ID, Locale: "uz", Name: "Elektronika", Description: &desc,
	}); err != nil {
		t.Fatalf("upsert translation with description: %v", err)
	}

	noDesc, err := q.CreateCategory(ctx, db.CreateCategoryParams{
		ID: uuid.New(), ShopID: shop.ID, Slug: "no-desc", IsActive: true,
	})
	if err != nil {
		t.Fatalf("CreateCategory(no-desc): %v", err)
	}
	if err := q.UpsertCategoryTranslation(ctx, db.UpsertCategoryTranslationParams{
		CategoryID: noDesc.ID, Locale: "uz", Name: "Boshqa",
	}); err != nil {
		t.Fatalf("upsert translation without description: %v", err)
	}

	untranslated, err := q.CreateCategory(ctx, db.CreateCategoryParams{
		ID: uuid.New(), ShopID: shop.ID, Slug: "untranslated", IsActive: true,
	})
	if err != nil {
		t.Fatalf("CreateCategory(untranslated): %v", err)
	}

	got, err := q.GetCategory(ctx, db.GetCategoryParams{ShopID: shop.ID, ID: withDesc.ID, Locale: "uz"})
	if err != nil {
		t.Fatalf("GetCategory(with-desc): %v", err)
	}
	if got.Description == nil || *got.Description != desc {
		t.Fatalf("GetCategory(with-desc).Description = %v, want %q", got.Description, desc)
	}

	got, err = q.GetCategory(ctx, db.GetCategoryParams{ShopID: shop.ID, ID: noDesc.ID, Locale: "uz"})
	if err != nil {
		t.Fatalf("GetCategory(no-desc): %v", err)
	}
	if got.Description != nil {
		t.Fatalf("GetCategory(no-desc).Description = %v, want nil (NULL, not '')", *got.Description)
	}

	got, err = q.GetCategory(ctx, db.GetCategoryParams{ShopID: shop.ID, ID: untranslated.ID, Locale: "uz"})
	if err != nil {
		t.Fatalf("GetCategory(untranslated): %v", err)
	}
	if got.Description != nil {
		t.Fatalf("GetCategory(untranslated).Description = %v, want nil", *got.Description)
	}

	rows, err := q.ListCategories(ctx, db.ListCategoriesParams{ShopID: shop.ID, Locale: "uz", IncludeInactive: true})
	if err != nil {
		t.Fatalf("ListCategories: %v", err)
	}
	byID := func(id uuid.UUID) db.ListCategoriesRow {
		t.Helper()
		for _, r := range rows {
			if r.ID == id {
				return r
			}
		}
		t.Fatalf("no row with id %s", id)
		return db.ListCategoriesRow{}
	}
	if r := byID(withDesc.ID); r.Description == nil || *r.Description != desc {
		t.Fatalf("ListCategories(with-desc).Description = %v, want %q", r.Description, desc)
	}
	if r := byID(noDesc.ID); r.Description != nil {
		t.Fatalf("ListCategories(no-desc).Description = %v, want nil", *r.Description)
	}
	if r := byID(untranslated.ID); r.Description != nil {
		t.Fatalf("ListCategories(untranslated).Description = %v, want nil", *r.Description)
	}
}

func TestUpdateCategory_clearParentAndClearImage(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-cat-clear")
	media := catalogMedia(ctx, t, q, shop.ID, "cat-image.webp", [32]byte{7})

	parent, err := q.CreateCategory(ctx, db.CreateCategoryParams{
		ID: uuid.New(), ShopID: shop.ID, Slug: "parent", IsActive: true,
	})
	if err != nil {
		t.Fatalf("CreateCategory(parent): %v", err)
	}
	child, err := q.CreateCategory(ctx, db.CreateCategoryParams{
		ID: uuid.New(), ShopID: shop.ID, ParentID: &parent.ID, Slug: "child", IsActive: true, ImageID: &media.ID,
	})
	if err != nil {
		t.Fatalf("CreateCategory(child): %v", err)
	}
	if child.ParentID == nil || *child.ParentID != parent.ID {
		t.Fatalf("child.ParentID = %v, want %s", child.ParentID, parent.ID)
	}
	if child.ImageID == nil || *child.ImageID != media.ID {
		t.Fatalf("child.ImageID = %v, want %s", child.ImageID, media.ID)
	}

	// clear_parent / clear_image false, no narg supplied: COALESCE keeps the
	// existing values unchanged (same as before the flags existed).
	unchanged, err := q.UpdateCategory(ctx, db.UpdateCategoryParams{
		ShopID: shop.ID, ID: child.ID, SortOrder: nil,
	})
	if err != nil {
		t.Fatalf("UpdateCategory(no-op patch): %v", err)
	}
	if unchanged.ParentID == nil || *unchanged.ParentID != parent.ID {
		t.Fatalf("no-op patch: ParentID = %v, want unchanged %s", unchanged.ParentID, parent.ID)
	}
	if unchanged.ImageID == nil || *unchanged.ImageID != media.ID {
		t.Fatalf("no-op patch: ImageID = %v, want unchanged %s", unchanged.ImageID, media.ID)
	}

	// clear_parent / clear_image true: both go to NULL regardless of any
	// narg value (there is none here, but the flag wins either way).
	cleared, err := q.UpdateCategory(ctx, db.UpdateCategoryParams{
		ShopID: shop.ID, ID: child.ID, ClearParent: true, ClearImage: true,
	})
	if err != nil {
		t.Fatalf("UpdateCategory(clear): %v", err)
	}
	if cleared.ParentID != nil {
		t.Fatalf("cleared.ParentID = %v, want nil", *cleared.ParentID)
	}
	if cleared.ImageID != nil {
		t.Fatalf("cleared.ImageID = %v, want nil", *cleared.ImageID)
	}
}
