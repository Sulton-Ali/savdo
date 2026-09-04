package db_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

// assertJSONEqual compares two jsonb values by decoded content, not raw
// bytes: Postgres reformats jsonb on the way back out (e.g. a space after
// ':'), so a byte-for-byte comparison would fail even for equal values.
func assertJSONEqual(t *testing.T, label string, want, got []byte) {
	t.Helper()
	var wantV, gotV any
	if err := json.Unmarshal(want, &wantV); err != nil {
		t.Fatalf("unmarshal want %s: %v", label, err)
	}
	if err := json.Unmarshal(got, &gotV); err != nil {
		t.Fatalf("unmarshal got %s: %v", label, err)
	}
	if !reflect.DeepEqual(wantV, gotV) {
		t.Fatalf("want %s %s, got %s", label, want, got)
	}
}

// purchaseSupplier creates a supplier for the purchase tests below.
func purchaseSupplier(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, name string) db.Supplier {
	t.Helper()
	s, err := q.CreateSupplier(ctx, db.CreateSupplierParams{ID: uuid.New(), ShopID: shopID, Name: name})
	if err != nil {
		t.Fatalf("CreateSupplier(%q): %v", name, err)
	}
	return s
}

// draftPurchase claims the next purchase number and creates a draft
// purchase from it, the way the service will: NextPurchaseNumber under
// row lock, then format "P-%06d" (D-45).
func draftPurchase(ctx context.Context, t *testing.T, q *db.Queries, shopID, supplierID, locationID uuid.UUID) db.Purchase {
	t.Helper()
	num, err := q.NextPurchaseNumber(ctx, shopID)
	if err != nil {
		t.Fatalf("NextPurchaseNumber: %v", err)
	}
	p, err := q.CreatePurchase(ctx, db.CreatePurchaseParams{
		ID: uuid.New(), ShopID: shopID, SupplierID: supplierID, LocationID: locationID,
		Number: fmt.Sprintf("P-%06d", num),
	})
	if err != nil {
		t.Fatalf("CreatePurchase: %v", err)
	}
	return p
}

func TestPurchaseItems_qtyMustBePositive(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-purchase-items-qty")
	unit := catalogUnit(ctx, t, q, shop.ID, "pcs")
	product := catalogProduct(ctx, t, q, shop.ID, unit.ID, "hoodie")
	variant := stockVariant(ctx, t, q, shop.ID, product.ID, "{}")
	supplier := purchaseSupplier(ctx, t, q, shop.ID, "Acme")
	loc := stockLocation(ctx, t, q, shop.ID, "Main")
	purchase := draftPurchase(ctx, t, q, shop.ID, supplier.ID, loc.ID)

	newItem := func(qty string) error {
		_, err := q.CreatePurchaseItem(ctx, db.CreatePurchaseItemParams{
			ID: uuid.New(), ShopID: shop.ID, PurchaseID: purchase.ID, VariantID: variant.ID,
			Qty: numeric(t, qty), UnitCost: numeric(t, "1000.00"), LineTotal: numeric(t, "1000.00"),
		})
		return err
	}

	if err := newItem("0.000"); err == nil {
		t.Error("want a check-constraint error for qty = 0, got none")
	}
	if err := newItem("-1.000"); err == nil {
		t.Error("want a check-constraint error for a negative qty, got none")
	}
	if err := newItem("1.000"); err != nil {
		t.Errorf("want a positive qty to succeed, got: %v", err)
	}
}

func TestNextPurchaseNumber_incrementsAtomicallyUnderConcurrency(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-purchase-number-concurrency")

	const n = 20
	results := make([]int64, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = q.NextPurchaseNumber(ctx, shop.ID)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("NextPurchaseNumber goroutine %d: %v", i, err)
		}
	}

	sorted := append([]int64(nil), results...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	for i, got := range sorted {
		want := int64(1 + i) // shops.next_purchase_number defaults to 1.
		if got != want {
			t.Fatalf("want %d consecutive numbers starting at 1 with no gaps or duplicates, got %v", n, sorted)
		}
	}
}

func TestUpdatePurchaseHeader_onlyAffectsDraft(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-purchase-header")
	supplier := purchaseSupplier(ctx, t, q, shop.ID, "Acme")
	loc := stockLocation(ctx, t, q, shop.ID, "Main")
	purchase := draftPurchase(ctx, t, q, shop.ID, supplier.ID, loc.ID)

	note := "handle with care"
	updated, err := q.UpdatePurchaseHeader(ctx, db.UpdatePurchaseHeaderParams{
		ShopID: shop.ID, ID: purchase.ID, Note: &note,
	})
	if err != nil {
		t.Fatalf("want UpdatePurchaseHeader to affect a draft purchase, got: %v", err)
	}
	if updated.Note == nil || *updated.Note != note {
		t.Fatalf("want note %q, got %v", note, updated.Note)
	}

	received, err := q.SetPurchaseReceived(ctx, db.SetPurchaseReceivedParams{
		ShopID: shop.ID, ID: purchase.ID, TotalCost: numeric(t, "0.00"),
	})
	if err != nil {
		t.Fatalf("SetPurchaseReceived: %v", err)
	}
	if received.Status != db.PurchaseStatusReceived {
		t.Fatalf("want status received, got %s", received.Status)
	}

	otherNote := "too late"
	if _, err := q.UpdatePurchaseHeader(ctx, db.UpdatePurchaseHeaderParams{
		ShopID: shop.ID, ID: purchase.ID, Note: &otherNote,
	}); err == nil {
		t.Fatal("want UpdatePurchaseHeader on a received purchase to return no row, got success")
	} else if err != pgx.ErrNoRows {
		t.Fatalf("want pgx.ErrNoRows, got: %v", err)
	}
}

func TestDeletePurchaseItems_onlyAffectsDraft(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-purchase-replace-items")
	unit := catalogUnit(ctx, t, q, shop.ID, "pcs")
	product := catalogProduct(ctx, t, q, shop.ID, unit.ID, "hoodie")
	variant := stockVariant(ctx, t, q, shop.ID, product.ID, "{}")
	supplier := purchaseSupplier(ctx, t, q, shop.ID, "Acme")
	loc := stockLocation(ctx, t, q, shop.ID, "Main")
	purchase := draftPurchase(ctx, t, q, shop.ID, supplier.ID, loc.ID)

	if _, err := q.CreatePurchaseItem(ctx, db.CreatePurchaseItemParams{
		ID: uuid.New(), ShopID: shop.ID, PurchaseID: purchase.ID, VariantID: variant.ID,
		Qty: numeric(t, "2.000"), UnitCost: numeric(t, "1000.00"), LineTotal: numeric(t, "2000.00"),
	}); err != nil {
		t.Fatalf("CreatePurchaseItem: %v", err)
	}

	// Draft: replace-items (delete) affects the row.
	affected, err := q.DeletePurchaseItems(ctx, db.DeletePurchaseItemsParams{ShopID: shop.ID, PurchaseID: purchase.ID})
	if err != nil {
		t.Fatalf("DeletePurchaseItems on a draft: %v", err)
	}
	if affected != 1 {
		t.Fatalf("want 1 row affected deleting a draft's items, got %d", affected)
	}

	// Re-add an item, then receive the purchase.
	if _, err := q.CreatePurchaseItem(ctx, db.CreatePurchaseItemParams{
		ID: uuid.New(), ShopID: shop.ID, PurchaseID: purchase.ID, VariantID: variant.ID,
		Qty: numeric(t, "2.000"), UnitCost: numeric(t, "1000.00"), LineTotal: numeric(t, "2000.00"),
	}); err != nil {
		t.Fatalf("CreatePurchaseItem: %v", err)
	}
	if _, err := q.SetPurchaseReceived(ctx, db.SetPurchaseReceivedParams{
		ShopID: shop.ID, ID: purchase.ID, TotalCost: numeric(t, "2000.00"),
	}); err != nil {
		t.Fatalf("SetPurchaseReceived: %v", err)
	}

	// Received: replace-items (delete) affects 0 rows — the status guard
	// blocks it even though the purchase still has an item to delete.
	affected, err = q.DeletePurchaseItems(ctx, db.DeletePurchaseItemsParams{ShopID: shop.ID, PurchaseID: purchase.ID})
	if err != nil {
		t.Fatalf("DeletePurchaseItems on a received purchase: %v", err)
	}
	if affected != 0 {
		t.Fatalf("want 0 rows affected deleting a received purchase's items, got %d", affected)
	}

	items, err := q.ListPurchaseItems(ctx, db.ListPurchaseItemsParams{ShopID: shop.ID, PurchaseID: purchase.ID})
	if err != nil {
		t.Fatalf("ListPurchaseItems: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want the received purchase's item to survive the blocked delete, got %d items", len(items))
	}
}

func TestSetPurchaseReceived_onlyFromDraft(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-purchase-receive")
	supplier := purchaseSupplier(ctx, t, q, shop.ID, "Acme")
	loc := stockLocation(ctx, t, q, shop.ID, "Main")
	purchase := draftPurchase(ctx, t, q, shop.ID, supplier.ID, loc.ID)

	if _, err := q.SetPurchaseReceived(ctx, db.SetPurchaseReceivedParams{
		ShopID: shop.ID, ID: purchase.ID, TotalCost: numeric(t, "0.00"),
	}); err != nil {
		t.Fatalf("want SetPurchaseReceived to affect a draft purchase, got: %v", err)
	}

	// Already received: a second receive returns no row.
	if _, err := q.SetPurchaseReceived(ctx, db.SetPurchaseReceivedParams{
		ShopID: shop.ID, ID: purchase.ID, TotalCost: numeric(t, "0.00"),
	}); err != pgx.ErrNoRows {
		t.Fatalf("want pgx.ErrNoRows receiving an already-received purchase, got: %v", err)
	}

	// A cancelled purchase cannot be received either.
	purchase2 := draftPurchase(ctx, t, q, shop.ID, supplier.ID, loc.ID)
	if _, err := q.SetPurchaseCancelled(ctx, db.SetPurchaseCancelledParams{ShopID: shop.ID, ID: purchase2.ID}); err != nil {
		t.Fatalf("SetPurchaseCancelled: %v", err)
	}
	if _, err := q.SetPurchaseReceived(ctx, db.SetPurchaseReceivedParams{
		ShopID: shop.ID, ID: purchase2.ID, TotalCost: numeric(t, "0.00"),
	}); err != pgx.ErrNoRows {
		t.Fatalf("want pgx.ErrNoRows receiving a cancelled purchase, got: %v", err)
	}
}

func TestSetPurchaseCancelled_fromDraftOrReceivedNotFromCancelled(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-purchase-cancel")
	supplier := purchaseSupplier(ctx, t, q, shop.ID, "Acme")
	loc := stockLocation(ctx, t, q, shop.ID, "Main")

	// From draft: succeeds.
	draft := draftPurchase(ctx, t, q, shop.ID, supplier.ID, loc.ID)
	if _, err := q.SetPurchaseCancelled(ctx, db.SetPurchaseCancelledParams{ShopID: shop.ID, ID: draft.ID}); err != nil {
		t.Fatalf("want cancel from draft to succeed, got: %v", err)
	}

	// From received: succeeds.
	received := draftPurchase(ctx, t, q, shop.ID, supplier.ID, loc.ID)
	if _, err := q.SetPurchaseReceived(ctx, db.SetPurchaseReceivedParams{
		ShopID: shop.ID, ID: received.ID, TotalCost: numeric(t, "0.00"),
	}); err != nil {
		t.Fatalf("SetPurchaseReceived: %v", err)
	}
	if _, err := q.SetPurchaseCancelled(ctx, db.SetPurchaseCancelledParams{ShopID: shop.ID, ID: received.ID}); err != nil {
		t.Fatalf("want cancel from received to succeed, got: %v", err)
	}

	// From cancelled again: no row.
	if _, err := q.SetPurchaseCancelled(ctx, db.SetPurchaseCancelledParams{ShopID: shop.ID, ID: draft.ID}); err != pgx.ErrNoRows {
		t.Fatalf("want pgx.ErrNoRows cancelling an already-cancelled purchase, got: %v", err)
	}
}

func TestInsertAuditLog_readBack(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-audit")
	actor, err := q.CreateUser(ctx, db.CreateUserParams{
		ID: uuid.New(), ShopID: shop.ID, Username: "owner1", PasswordHash: "hash",
		FullName: "Owner", Role: db.UserRoleOwner, Locale: "uz",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	entityID := uuid.New()
	before := []byte(`{"qty":"5.000"}`)
	after := []byte(`{"qty":"3.000"}`)
	row, err := q.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ID: uuid.New(), ShopID: shop.ID, ActorID: actor.ID,
		Action: "stock.adjust", EntityType: "stock_movement", EntityID: entityID,
		Before: before, After: after,
	})
	if err != nil {
		t.Fatalf("InsertAuditLog: %v", err)
	}

	if row.ActorID != actor.ID || row.Action != "stock.adjust" || row.EntityType != "stock_movement" || row.EntityID != entityID {
		t.Fatalf("InsertAuditLog returned unexpected row: %+v", row)
	}
	// jsonb round-trips through Postgres with its own formatting (e.g. a
	// space after ':'), so compare decoded values, not raw bytes.
	assertJSONEqual(t, "before", before, row.Before)
	assertJSONEqual(t, "after", after, row.After)
}

func TestIdempotencyKeys_pkConflictAndShopScoping(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shopA := catalogShop(ctx, t, q, "shop-idem-a")
	shopB := catalogShop(ctx, t, q, "shop-idem-b")

	if _, err := q.InsertIdempotencyKey(ctx, db.InsertIdempotencyKeyParams{
		ShopID: shopA.ID, Key: "abc", RequestHash: "hash-a", ResponseStatus: 201, ResponseBody: []byte(`{"id":"a"}`),
	}); err != nil {
		t.Fatalf("InsertIdempotencyKey shop A: %v", err)
	}

	// Same (shop_id, key) again: PK conflict.
	if _, err := q.InsertIdempotencyKey(ctx, db.InsertIdempotencyKeyParams{
		ShopID: shopA.ID, Key: "abc", RequestHash: "hash-a-2", ResponseStatus: 201, ResponseBody: []byte(`{"id":"a2"}`),
	}); err == nil {
		t.Fatal("want a primary-key conflict inserting the same (shop_id, key) twice, got none")
	}

	// Same key text, different shop: allowed, and scoped independently.
	if _, err := q.InsertIdempotencyKey(ctx, db.InsertIdempotencyKeyParams{
		ShopID: shopB.ID, Key: "abc", RequestHash: "hash-b", ResponseStatus: 201, ResponseBody: []byte(`{"id":"b"}`),
	}); err != nil {
		t.Fatalf("want the same key text to succeed in a different shop, got: %v", err)
	}

	gotA, err := q.GetIdempotencyKey(ctx, db.GetIdempotencyKeyParams{ShopID: shopA.ID, Key: "abc"})
	if err != nil {
		t.Fatalf("GetIdempotencyKey shop A: %v", err)
	}
	if gotA.RequestHash != "hash-a" {
		t.Fatalf("want shop A's original row (hash-a), got %q", gotA.RequestHash)
	}

	gotB, err := q.GetIdempotencyKey(ctx, db.GetIdempotencyKeyParams{ShopID: shopB.ID, Key: "abc"})
	if err != nil {
		t.Fatalf("GetIdempotencyKey shop B: %v", err)
	}
	if gotB.RequestHash != "hash-b" {
		t.Fatalf("want shop B's own row (hash-b), got %q", gotB.RequestHash)
	}

	if _, err := q.GetIdempotencyKey(ctx, db.GetIdempotencyKeyParams{ShopID: shopA.ID, Key: "does-not-exist"}); err != pgx.ErrNoRows {
		t.Fatalf("want pgx.ErrNoRows for a missing key, got: %v", err)
	}
}
