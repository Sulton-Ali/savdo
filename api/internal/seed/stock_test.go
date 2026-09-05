package seed_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/internal/crm"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
	"github.com/Sulton-Ali/savdo/api/internal/money"
	"github.com/Sulton-Ali/savdo/api/internal/seed"
	"github.com/Sulton-Ali/savdo/api/internal/stock"
)

// findLocation locates name among locations, failing the test if it's not
// there — every caller here expects Seed's own two locations to exist.
func findLocation(t *testing.T, locations []db.Location, name string) uuid.UUID {
	t.Helper()
	for _, l := range locations {
		if l.Name == name {
			return l.ID
		}
	}
	t.Fatalf("location %q not found among %d seeded locations", name, len(locations))
	return uuid.UUID{}
}

// decimalFromNumeric converts a scanned stock_levels.qty column to a
// decimal.Decimal for assertions, failing the test on an invalid (NULL)
// value.
func decimalFromNumeric(t *testing.T, n pgtype.Numeric) (decimal.Decimal, error) {
	t.Helper()
	return money.FromNumeric(n)
}

func TestSeedStock_seedsSuppliersPurchasesAndATransferIdempotently(t *testing.T) {
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

	if _, err := seed.Catalog(ctx, q, catalogHandler, mediaSvc, shopReport.ShopID, owner.ID); err != nil {
		t.Fatalf("Catalog() error = %v", err)
	}

	products, err := q.ListProductsForStaff(ctx, db.ListProductsForStaffParams{
		Locale: "uz", ShopID: shopReport.ShopID, IncludeInactive: true, Limit: 1000,
	})
	if err != nil {
		t.Fatalf("ListProductsForStaff() error = %v", err)
	}
	wantVariants := 0
	for _, p := range products {
		variants, err := q.ListVariantsForStaff(ctx, db.ListVariantsForStaffParams{ShopID: shopReport.ShopID, ProductID: p.ID})
		if err != nil {
			t.Fatalf("ListVariantsForStaff(%s) error = %v", p.Slug, err)
		}
		wantVariants += len(variants)
	}
	if wantVariants == 0 {
		t.Fatal("catalog seeded 0 variants, nothing for Stock to buy")
	}

	crmHandler := crm.NewHandler(crm.NewService(q))
	stockHandler := stock.NewHandler(stock.NewService(pool, q))

	first, err := seed.Stock(ctx, pool, q, crmHandler, stockHandler, shopReport.ShopID, owner.ID)
	if err != nil {
		t.Fatalf("first Stock() error = %v", err)
	}
	if first.Skipped {
		t.Fatal("first Stock() Skipped = true, want false on a fresh catalogue")
	}
	if first.SuppliersCreated != 3 {
		t.Errorf("SuppliersCreated = %d, want 3", first.SuppliersCreated)
	}
	if first.PurchasesCreated == 0 {
		t.Fatal("PurchasesCreated = 0, want > 0")
	}
	if first.PurchaseItemsCreated != wantVariants {
		t.Errorf("PurchaseItemsCreated = %d, want %d (one purchase item per variant)", first.PurchaseItemsCreated, wantVariants)
	}
	const wantTransfers = 10
	if first.TransfersCreated != wantTransfers {
		t.Errorf("TransfersCreated = %d, want %d", first.TransfersCreated, wantTransfers)
	}

	suppliers, err := q.ListSuppliers(ctx, db.ListSuppliersParams{ShopID: shopReport.ShopID, Limit: 100})
	if err != nil {
		t.Fatalf("ListSuppliers() error = %v", err)
	}
	if len(suppliers) != 3 {
		t.Errorf("len(suppliers) = %d, want 3", len(suppliers))
	}

	locations, err := q.ListLocations(ctx, db.ListLocationsParams{ShopID: shopReport.ShopID, Limit: 10})
	if err != nil {
		t.Fatalf("ListLocations() error = %v", err)
	}
	mainID := findLocation(t, locations, seed.LocationStoreName)
	storeroomID := findLocation(t, locations, seed.LocationWarehouseName)

	// Every variant has a positive opening stock level in the main
	// location.
	mainLevels, err := q.ListLevels(ctx, db.ListLevelsParams{ShopID: shopReport.ShopID, LocationID: &mainID, Limit: 1000})
	if err != nil {
		t.Fatalf("ListLevels(main) error = %v", err)
	}
	if len(mainLevels) != wantVariants {
		t.Errorf("len(mainLevels) = %d, want %d (one level row per variant)", len(mainLevels), wantVariants)
	}
	for _, l := range mainLevels {
		qty, err := decimalFromNumeric(t, l.Qty)
		if err != nil {
			t.Fatalf("main level qty for variant %s: %v", l.VariantID, err)
		}
		if !qty.IsPositive() {
			t.Errorf("main level qty for variant %s = %s, want > 0", l.VariantID, qty)
		}
	}

	// The transferred variants show up in the storeroom too, each at
	// exactly transferQty (2).
	storeroomLevels, err := q.ListLevels(ctx, db.ListLevelsParams{ShopID: shopReport.ShopID, LocationID: &storeroomID, Limit: 1000})
	if err != nil {
		t.Fatalf("ListLevels(storeroom) error = %v", err)
	}
	if len(storeroomLevels) != wantTransfers {
		t.Errorf("len(storeroomLevels) = %d, want %d", len(storeroomLevels), wantTransfers)
	}
	for _, l := range storeroomLevels {
		qty, err := decimalFromNumeric(t, l.Qty)
		if err != nil {
			t.Fatalf("storeroom level qty for variant %s: %v", l.VariantID, err)
		}
		if qty.String() != "2" {
			t.Errorf("storeroom level qty for variant %s = %s, want 2", l.VariantID, qty)
		}
	}

	// The seed never wrote a stock_levels row except through Move: the
	// movement count is exactly one purchase_in per purchase item plus two
	// (transfer_out + transfer_in) per transfer.
	movementCount, err := q.CountMovementsForShop(ctx, shopReport.ShopID)
	if err != nil {
		t.Fatalf("CountMovementsForShop() error = %v", err)
	}
	wantMovements := int64(first.PurchaseItemsCreated + 2*first.TransfersCreated)
	if movementCount != wantMovements {
		t.Errorf("CountMovementsForShop() = %d, want %d (%d purchase items + 2*%d transfers)",
			movementCount, wantMovements, first.PurchaseItemsCreated, first.TransfersCreated)
	}

	// stock.Rebuild agrees with the levels Move already wrote.
	rebuildResult, err := stock.Rebuild(ctx, pool, seed.DefaultShopSlug)
	if err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	if rebuildResult.Movements != movementCount {
		t.Errorf("Rebuild() Movements = %d, want %d", rebuildResult.Movements, movementCount)
	}
	wantLevels := int64(wantVariants + wantTransfers)
	if rebuildResult.Levels != wantLevels {
		t.Errorf("Rebuild() Levels = %d, want %d", rebuildResult.Levels, wantLevels)
	}

	mainLevelsAfterRebuild, err := q.ListLevels(ctx, db.ListLevelsParams{ShopID: shopReport.ShopID, LocationID: &mainID, Limit: 1000})
	if err != nil {
		t.Fatalf("ListLevels(main) after rebuild error = %v", err)
	}
	if len(mainLevelsAfterRebuild) != len(mainLevels) {
		t.Errorf("len(mainLevels) after rebuild = %d, want %d (unchanged)", len(mainLevelsAfterRebuild), len(mainLevels))
	}

	// Running the seed again creates nothing new: the shop already has a
	// purchase, so Stock is a pure no-op.
	second, err := seed.Stock(ctx, pool, q, crmHandler, stockHandler, shopReport.ShopID, owner.ID)
	if err != nil {
		t.Fatalf("second Stock() error = %v", err)
	}
	if !second.Skipped {
		t.Error("second Stock() Skipped = false, want true (a purchase already exists)")
	}
	if second != (seed.StockReport{Skipped: true}) {
		t.Errorf("second Stock() = %+v, want only Skipped: true set", second)
	}

	suppliersAfter, err := q.ListSuppliers(ctx, db.ListSuppliersParams{ShopID: shopReport.ShopID, Limit: 100})
	if err != nil {
		t.Fatalf("ListSuppliers() after second Stock() error = %v", err)
	}
	if len(suppliersAfter) != 3 {
		t.Errorf("len(suppliers) after second Stock() = %d, want 3 (no duplicates)", len(suppliersAfter))
	}

	movementCountAfter, err := q.CountMovementsForShop(ctx, shopReport.ShopID)
	if err != nil {
		t.Fatalf("CountMovementsForShop() after second Stock() error = %v", err)
	}
	if movementCountAfter != movementCount {
		t.Errorf("CountMovementsForShop() after second Stock() = %d, want %d (unchanged)", movementCountAfter, movementCount)
	}
}
