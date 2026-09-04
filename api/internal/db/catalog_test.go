package db_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
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
	if _, err := q.CreateVariant(ctx, db.CreateVariantParams{
		ID: uuid.New(), ShopID: shop.ID, ProductID: product.ID, Attributes: large, IsActive: true,
	}); err == nil {
		t.Fatal("want a uniqueness error inserting the same (product_id, attributes) twice, got none")
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
