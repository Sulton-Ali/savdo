package db_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

func TestCustomers_phoneReusableAfterSoftDeleteButNotWhileActive(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-customers-phone")
	phone := "+998901234567"

	first, err := q.CreateCustomer(ctx, db.CreateCustomerParams{ID: uuid.New(), ShopID: shop.ID, FullName: "Aziz", Phone: &phone})
	if err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}

	// Same phone, still active: rejected by the partial unique index.
	if _, err := q.CreateCustomer(ctx, db.CreateCustomerParams{ID: uuid.New(), ShopID: shop.ID, FullName: "Bekzod", Phone: &phone}); err == nil {
		t.Fatal("want a uniqueness error creating a second active customer with the same phone, got none")
	}

	if err := q.SoftDeleteCustomer(ctx, db.SoftDeleteCustomerParams{ShopID: shop.ID, ID: first.ID}); err != nil {
		t.Fatalf("SoftDeleteCustomer: %v", err)
	}

	// Phone is free again: the partial index only covers deleted_at IS NULL.
	second, err := q.CreateCustomer(ctx, db.CreateCustomerParams{ID: uuid.New(), ShopID: shop.ID, FullName: "Bekzod", Phone: &phone})
	if err != nil {
		t.Fatalf("want the phone to be reusable after soft delete, got: %v", err)
	}
	if second.ID == first.ID {
		t.Fatal("want a new customer row, got the same id back")
	}

	if _, err := q.GetCustomer(ctx, db.GetCustomerParams{ShopID: shop.ID, ID: first.ID}); err == nil {
		t.Fatal("want GetCustomer to not find a soft-deleted customer, got a row")
	}
}

func TestCreateCustomer_tagsDefaultToEmptyArray(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-customers-tags")

	// No Tags supplied (nil slice): COALESCE in CreateCustomer falls back
	// to '{}', matching the column's own default.
	c, err := q.CreateCustomer(ctx, db.CreateCustomerParams{ID: uuid.New(), ShopID: shop.ID, FullName: "No Tags"})
	if err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}
	if len(c.Tags) != 0 {
		t.Fatalf("want an empty tags array by default, got %v", c.Tags)
	}

	withTags, err := q.CreateCustomer(ctx, db.CreateCustomerParams{
		ID: uuid.New(), ShopID: shop.ID, FullName: "VIP", Tags: []string{"vip", "wholesale"},
	})
	if err != nil {
		t.Fatalf("CreateCustomer with tags: %v", err)
	}
	if len(withTags.Tags) != 2 || withTags.Tags[0] != "vip" || withTags.Tags[1] != "wholesale" {
		t.Fatalf("want tags [vip wholesale], got %v", withTags.Tags)
	}
}

func TestUpdateCustomer_clearFlagsAndTagsReplace(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-customers-update")
	phone := "+998901112233"
	note := "prefers cash"
	c, err := q.CreateCustomer(ctx, db.CreateCustomerParams{
		ID: uuid.New(), ShopID: shop.ID, FullName: "Dilnoza", Phone: &phone, Note: &note, Tags: []string{"regular"},
	})
	if err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}

	// Clear phone and note, replace tags, leave full_name alone.
	updated, err := q.UpdateCustomer(ctx, db.UpdateCustomerParams{
		ShopID: shop.ID, ID: c.ID,
		ClearPhone: true, ClearNote: true, Tags: []string{"vip"},
	})
	if err != nil {
		t.Fatalf("UpdateCustomer: %v", err)
	}
	if updated.Phone != nil {
		t.Errorf("want phone cleared, got %v", updated.Phone)
	}
	if updated.Note != nil {
		t.Errorf("want note cleared, got %v", updated.Note)
	}
	if updated.FullName != "Dilnoza" {
		t.Errorf("want full_name unchanged, got %q", updated.FullName)
	}
	if len(updated.Tags) != 1 || updated.Tags[0] != "vip" {
		t.Errorf("want tags replaced with [vip], got %v", updated.Tags)
	}
}

func TestListCustomers_searchByNameOrPhone(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-customers-search")
	phone := "+998907778899"
	if _, err := q.CreateCustomer(ctx, db.CreateCustomerParams{ID: uuid.New(), ShopID: shop.ID, FullName: "Nodira Karimova", Phone: &phone}); err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}
	otherPhone := "+998901000000"
	if _, err := q.CreateCustomer(ctx, db.CreateCustomerParams{ID: uuid.New(), ShopID: shop.ID, FullName: "Jasur Odilov", Phone: &otherPhone}); err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}

	byName := "Nodira"
	rows, err := q.ListCustomers(ctx, db.ListCustomersParams{ShopID: shop.ID, Q: &byName, Limit: 10})
	if err != nil {
		t.Fatalf("ListCustomers by name: %v", err)
	}
	if len(rows) != 1 || rows[0].FullName != "Nodira Karimova" {
		t.Fatalf("want exactly Nodira Karimova matching %q, got %+v", byName, rows)
	}

	byPhone := "7778899"
	rows, err = q.ListCustomers(ctx, db.ListCustomersParams{ShopID: shop.ID, Q: &byPhone, Limit: 10})
	if err != nil {
		t.Fatalf("ListCustomers by phone: %v", err)
	}
	if len(rows) != 1 || rows[0].FullName != "Nodira Karimova" {
		t.Fatalf("want exactly Nodira Karimova matching phone %q, got %+v", byPhone, rows)
	}
}
