package db_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

func TestSuppliers_nameReusableAfterSoftDeleteButNotWhileActive(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-suppliers")

	first, err := q.CreateSupplier(ctx, db.CreateSupplierParams{ID: uuid.New(), ShopID: shop.ID, Name: "Acme"})
	if err != nil {
		t.Fatalf("CreateSupplier: %v", err)
	}

	// Same name, still active: rejected by the partial unique index.
	if _, err := q.CreateSupplier(ctx, db.CreateSupplierParams{ID: uuid.New(), ShopID: shop.ID, Name: "Acme"}); err == nil {
		t.Fatal("want a uniqueness error creating a second active supplier with the same name, got none")
	}

	if err := q.SoftDeleteSupplier(ctx, db.SoftDeleteSupplierParams{ShopID: shop.ID, ID: first.ID}); err != nil {
		t.Fatalf("SoftDeleteSupplier: %v", err)
	}

	// Name is free again: the partial index only covers deleted_at IS NULL.
	second, err := q.CreateSupplier(ctx, db.CreateSupplierParams{ID: uuid.New(), ShopID: shop.ID, Name: "Acme"})
	if err != nil {
		t.Fatalf("want the name to be reusable after soft delete, got: %v", err)
	}
	if second.ID == first.ID {
		t.Fatal("want a new supplier row, got the same id back")
	}

	// The soft-deleted supplier no longer appears in GetSupplier/ListSuppliers.
	if _, err := q.GetSupplier(ctx, db.GetSupplierParams{ShopID: shop.ID, ID: first.ID}); err == nil {
		t.Fatal("want GetSupplier to not find a soft-deleted supplier, got a row")
	}
}
