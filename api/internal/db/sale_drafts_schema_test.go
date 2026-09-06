package db_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

// saleDraftsFixture is shared setup for the tests below: a shop, a
// location, a customer, a creator user and one active variant — enough to
// exercise every FK sale_drafts/sale_draft_items carry (0017_sale_drafts.sql).
type saleDraftsFixture struct {
	pool                                              *pgxpool.Pool
	q                                                 *db.Queries
	shopID, locationID, customerID, userID, variantID uuid.UUID
}

func newSaleDraftsFixture(t *testing.T, shopSlug string) saleDraftsFixture {
	t.Helper()
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, shopSlug)
	unit := catalogUnit(ctx, t, q, shop.ID, "pcs")
	product := catalogProduct(ctx, t, q, shop.ID, unit.ID, "hoodie")
	variant := stockVariant(ctx, t, q, shop.ID, product.ID, "{}")
	loc := stockLocation(ctx, t, q, shop.ID, "Main")
	customer := salesCustomer(ctx, t, q, shop.ID, "Nodira Karimova")
	user := salesUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)

	return saleDraftsFixture{
		pool: pool, q: q,
		shopID: shop.ID, locationID: loc.ID, customerID: customer.ID, userID: user.ID, variantID: variant.ID,
	}
}

// newDraft creates a draft header with an optional discount and inserts
// the given item quantities in order, mirroring how the future
// sales.Service draft path will call these queries: CreateSaleDraft, then
// one InsertSaleDraftItem per line with position = its index.
func newDraft(ctx context.Context, t *testing.T, q *db.Queries, f saleDraftsFixture, qtys ...string) (db.SaleDraft, []db.SaleDraftItem) {
	t.Helper()
	draft, err := q.CreateSaleDraft(ctx, db.CreateSaleDraftParams{
		ID: uuid.New(), ShopID: f.shopID, LocationID: f.locationID, CustomerID: &f.customerID,
		CreatedBy: &f.userID,
	})
	if err != nil {
		t.Fatalf("CreateSaleDraft: %v", err)
	}
	items := make([]db.SaleDraftItem, 0, len(qtys))
	for i, qty := range qtys {
		item, err := q.InsertSaleDraftItem(ctx, db.InsertSaleDraftItemParams{
			ID: uuid.New(), ShopID: f.shopID, SaleDraftID: draft.ID, VariantID: f.variantID,
			Qty: numeric(t, qty), Position: int32(i),
		})
		if err != nil {
			t.Fatalf("InsertSaleDraftItem: %v", err)
		}
		items = append(items, item)
	}
	return draft, items
}

func TestSaleDrafts_insertDraftAndItems(t *testing.T) {
	f := newSaleDraftsFixture(t, "shop-drafts-insert")
	ctx := context.Background()

	discountType := db.DiscountTypePercent
	draft, err := f.q.CreateSaleDraft(ctx, db.CreateSaleDraftParams{
		ID: uuid.New(), ShopID: f.shopID, LocationID: f.locationID, CustomerID: &f.customerID,
		DiscountType: &discountType, DiscountValue: numeric(t, "10.00"), CreatedBy: &f.userID,
	})
	if err != nil {
		t.Fatalf("CreateSaleDraft: %v", err)
	}
	if draft.LocationID != f.locationID {
		t.Errorf("want location_id %s, got %s", f.locationID, draft.LocationID)
	}
	if draft.DiscountType == nil || *draft.DiscountType != db.DiscountTypePercent {
		t.Errorf("want discount_type percent, got %v", draft.DiscountType)
	}
	if numericString(t, draft.DiscountValue) != "10.00" {
		t.Errorf("want discount_value 10.00, got %s", numericString(t, draft.DiscountValue))
	}

	item, err := f.q.InsertSaleDraftItem(ctx, db.InsertSaleDraftItemParams{
		ID: uuid.New(), ShopID: f.shopID, SaleDraftID: draft.ID, VariantID: f.variantID,
		Qty: numeric(t, "2.000"), Position: 0,
	})
	if err != nil {
		t.Fatalf("InsertSaleDraftItem: %v", err)
	}
	if item.VariantID != f.variantID {
		t.Errorf("want variant_id %s, got %s", f.variantID, item.VariantID)
	}
	if numericString(t, item.Qty) != "2.000" {
		t.Errorf("want qty 2.000, got %s", numericString(t, item.Qty))
	}

	// No price/cost column at all (D-87) — enforced at compile time by
	// db.SaleDraftItem's own field set, not checked here at runtime.
}

// TestSaleDrafts_discountTypeAndValueMustBeBothOrNeither pins the CHECK
// this migration adds: discount_type and discount_value are one concept
// (mirrors DiscountType+value pairing in the contract's SaleDiscount
// schema) — a draft cannot carry one without the other.
func TestSaleDrafts_discountTypeAndValueMustBeBothOrNeither(t *testing.T) {
	f := newSaleDraftsFixture(t, "shop-drafts-discount-check")
	ctx := context.Background()

	discountType := db.DiscountTypeFixed
	if _, err := f.q.CreateSaleDraft(ctx, db.CreateSaleDraftParams{
		ID: uuid.New(), ShopID: f.shopID, LocationID: f.locationID, DiscountType: &discountType,
	}); err == nil {
		t.Error("want a check-constraint error for discount_type set without discount_value, got none")
	}
	if _, err := f.q.CreateSaleDraft(ctx, db.CreateSaleDraftParams{
		ID: uuid.New(), ShopID: f.shopID, LocationID: f.locationID, DiscountValue: numeric(t, "5.00"),
	}); err == nil {
		t.Error("want a check-constraint error for discount_value set without discount_type, got none")
	}
	if _, err := f.q.CreateSaleDraft(ctx, db.CreateSaleDraftParams{
		ID: uuid.New(), ShopID: f.shopID, LocationID: f.locationID,
	}); err != nil {
		t.Errorf("want neither discount column set to succeed, got: %v", err)
	}
}

func TestGetSaleDraft_withItemsOrderedByPosition(t *testing.T) {
	f := newSaleDraftsFixture(t, "shop-drafts-get")
	ctx := context.Background()

	draft, items := newDraft(ctx, t, f.q, f, "1.000", "2.000", "3.000")

	got, err := f.q.GetSaleDraft(ctx, db.GetSaleDraftParams{ShopID: f.shopID, ID: draft.ID})
	if err != nil {
		t.Fatalf("GetSaleDraft: %v", err)
	}
	if got.ID != draft.ID {
		t.Errorf("want draft id %s, got %s", draft.ID, got.ID)
	}
	if got.CustomerID == nil || *got.CustomerID != f.customerID {
		t.Errorf("want customer_id %s, got %v", f.customerID, got.CustomerID)
	}

	listed, err := f.q.ListSaleDraftItems(ctx, db.ListSaleDraftItemsParams{ShopID: f.shopID, SaleDraftID: draft.ID})
	if err != nil {
		t.Fatalf("ListSaleDraftItems: %v", err)
	}
	if len(listed) != 3 {
		t.Fatalf("want 3 items, got %d", len(listed))
	}
	for i, want := range items {
		if listed[i].ID != want.ID {
			t.Errorf("want item %d to be %s (position %d), got %s", i, want.ID, i, listed[i].ID)
		}
	}

	// A draft belonging to another shop must not resolve.
	other := catalogShop(ctx, t, f.q, "shop-drafts-get-other")
	if _, err := f.q.GetSaleDraft(ctx, db.GetSaleDraftParams{ShopID: other.ID, ID: draft.ID}); err != pgx.ErrNoRows {
		t.Fatalf("want pgx.ErrNoRows fetching a draft under the wrong shop_id, got: %v", err)
	}
}

func TestListSaleDrafts_newestFirstWithKeysetCursorScopedByShop(t *testing.T) {
	f := newSaleDraftsFixture(t, "shop-drafts-list")
	ctx := context.Background()
	other := catalogShop(ctx, t, f.q, "shop-drafts-list-other")

	draftA, _ := newDraft(ctx, t, f.q, f, "1.000")
	draftB, _ := newDraft(ctx, t, f.q, f, "1.000")
	draftC, _ := newDraft(ctx, t, f.q, f, "1.000")
	// A draft under a different shop must never leak into this shop's list.
	otherLoc := stockLocation(ctx, t, f.q, other.ID, "Other Main")
	otherDraft, err := f.q.CreateSaleDraft(ctx, db.CreateSaleDraftParams{
		ID: uuid.New(), ShopID: other.ID, LocationID: otherLoc.ID,
	})
	if err != nil {
		t.Fatalf("CreateSaleDraft (other shop): %v", err)
	}

	all, err := f.q.ListSaleDrafts(ctx, db.ListSaleDraftsParams{ShopID: f.shopID, Limit: 100})
	if err != nil {
		t.Fatalf("ListSaleDrafts: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("want 3 drafts for this shop, got %d", len(all))
	}
	// Newest first: draftC, draftB, draftA (creation order reversed).
	if all[0].ID != draftC.ID || all[1].ID != draftB.ID || all[2].ID != draftA.ID {
		t.Fatalf("want newest-first order [C, B, A], got [%s, %s, %s]", all[0].ID, all[1].ID, all[2].ID)
	}
	for _, r := range all {
		if r.ID == otherDraft.ID {
			t.Fatal("want the other shop's draft to never appear in this shop's list")
		}
	}

	// createdBy filter: none of these drafts were created by a second user,
	// so filtering by a fresh user id excludes everything.
	otherUser := salesUser(ctx, t, f.q, f.shopID, "cashier2", db.UserRoleCashier)
	byOtherCreator, err := f.q.ListSaleDrafts(ctx, db.ListSaleDraftsParams{ShopID: f.shopID, CreatedBy: &otherUser.ID, Limit: 100})
	if err != nil {
		t.Fatalf("ListSaleDrafts (by createdBy): %v", err)
	}
	if len(byOtherCreator) != 0 {
		t.Fatalf("want 0 drafts for an unrelated createdBy, got %d", len(byOtherCreator))
	}

	// Keyset cursor pagination (one page at a time) reproduces the full
	// unfiltered, newest-first list, same convention as
	// ListSalesForStaff/ListPurchases.
	var paginated []uuid.UUID
	p := db.ListSaleDraftsParams{ShopID: f.shopID, Limit: 1}
	for {
		page, err := f.q.ListSaleDrafts(ctx, p)
		if err != nil {
			t.Fatalf("ListSaleDrafts (paginated): %v", err)
		}
		if len(page) == 0 {
			break
		}
		for _, r := range page {
			paginated = append(paginated, r.ID)
		}
		last := page[len(page)-1]
		ca, id := last.CreatedAt, last.ID
		p.CursorCreatedAt, p.CursorID = &ca, &id
	}
	if len(paginated) != len(all) {
		t.Fatalf("want pagination to reproduce all %d drafts, got %d", len(all), len(paginated))
	}
	for i := range all {
		if all[i].ID != paginated[i] {
			t.Fatalf("pagination order mismatch at index %d: want %s, got %s", i, all[i].ID, paginated[i])
		}
	}
}

// TestUpdateSaleDraft_headerPatchAndReplaceItems exercises the PATCH shape
// § 05-API.md describes: header fields patch independently of items
// (clear flags for nullable columns), and `items` replaces the whole line
// set via DeleteSaleDraftItems + InsertSaleDraftItem, the same pattern
// purchase_items uses for a draft purchase.
func TestUpdateSaleDraft_headerPatchAndReplaceItems(t *testing.T) {
	f := newSaleDraftsFixture(t, "shop-drafts-update")
	ctx := context.Background()

	discountType := db.DiscountTypePercent
	draft, err := f.q.CreateSaleDraft(ctx, db.CreateSaleDraftParams{
		ID: uuid.New(), ShopID: f.shopID, LocationID: f.locationID, CustomerID: &f.customerID,
		DiscountType: &discountType, DiscountValue: numeric(t, "10.00"),
		DiscountReason: strPtr("loyal customer"), Note: strPtr("gift wrap"), CreatedBy: &f.userID,
	})
	if err != nil {
		t.Fatalf("CreateSaleDraft: %v", err)
	}
	_, err = f.q.InsertSaleDraftItem(ctx, db.InsertSaleDraftItemParams{
		ID: uuid.New(), ShopID: f.shopID, SaleDraftID: draft.ID, VariantID: f.variantID, Qty: numeric(t, "1.000"), Position: 0,
	})
	if err != nil {
		t.Fatalf("InsertSaleDraftItem: %v", err)
	}

	// Clear the customer, the discount pair and the note; leave discount_reason.
	updated, err := f.q.UpdateSaleDraft(ctx, db.UpdateSaleDraftParams{
		ShopID: f.shopID, ID: draft.ID,
		ClearCustomer: true, ClearDiscount: true, ClearNote: true,
	})
	if err != nil {
		t.Fatalf("UpdateSaleDraft: %v", err)
	}
	if updated.CustomerID != nil {
		t.Errorf("want customer_id cleared, got %v", updated.CustomerID)
	}
	if updated.DiscountType != nil || updated.DiscountValue.Valid {
		t.Errorf("want discount_type/discount_value both cleared, got type=%v value.Valid=%v", updated.DiscountType, updated.DiscountValue.Valid)
	}
	if updated.Note != nil {
		t.Errorf("want note cleared, got %v", updated.Note)
	}
	if updated.DiscountReason == nil || *updated.DiscountReason != "loyal customer" {
		t.Errorf("want discount_reason left untouched, got %v", updated.DiscountReason)
	}
	if !updated.UpdatedAt.After(draft.UpdatedAt) && updated.UpdatedAt != draft.UpdatedAt {
		// updated_at is set to now() unconditionally by UpdateSaleDraft; a
		// strict equality check would be flaky under coarse clock
		// resolution, so this only asserts it did not go backwards.
		t.Errorf("want updated_at >= the draft's original updated_at")
	}

	// Replace the whole item set: delete then insert new lines.
	affected, err := f.q.DeleteSaleDraftItems(ctx, db.DeleteSaleDraftItemsParams{ShopID: f.shopID, SaleDraftID: draft.ID})
	if err != nil {
		t.Fatalf("DeleteSaleDraftItems: %v", err)
	}
	if affected != 1 {
		t.Fatalf("want 1 item deleted, got %d", affected)
	}
	newItem, err := f.q.InsertSaleDraftItem(ctx, db.InsertSaleDraftItemParams{
		ID: uuid.New(), ShopID: f.shopID, SaleDraftID: draft.ID, VariantID: f.variantID, Qty: numeric(t, "5.000"), Position: 0,
	})
	if err != nil {
		t.Fatalf("InsertSaleDraftItem (replacement): %v", err)
	}

	items, err := f.q.ListSaleDraftItems(ctx, db.ListSaleDraftItemsParams{ShopID: f.shopID, SaleDraftID: draft.ID})
	if err != nil {
		t.Fatalf("ListSaleDraftItems: %v", err)
	}
	if len(items) != 1 || items[0].ID != newItem.ID {
		t.Fatalf("want exactly the replacement item, got %+v", items)
	}
}

// TestDeleteSaleDraft_cascadesItems proves ON DELETE CASCADE
// (0017_sale_drafts.sql): deleting the header removes its lines in the
// same statement, with no separate DeleteSaleDraftItems call needed.
func TestDeleteSaleDraft_cascadesItems(t *testing.T) {
	f := newSaleDraftsFixture(t, "shop-drafts-delete-cascade")
	ctx := context.Background()

	draft, items := newDraft(ctx, t, f.q, f, "1.000", "2.000")

	affected, err := f.q.DeleteSaleDraft(ctx, db.DeleteSaleDraftParams{ShopID: f.shopID, ID: draft.ID})
	if err != nil {
		t.Fatalf("DeleteSaleDraft: %v", err)
	}
	if affected != 1 {
		t.Fatalf("want 1 draft deleted, got %d", affected)
	}

	if _, err := f.q.GetSaleDraft(ctx, db.GetSaleDraftParams{ShopID: f.shopID, ID: draft.ID}); err != pgx.ErrNoRows {
		t.Fatalf("want pgx.ErrNoRows fetching a deleted draft, got: %v", err)
	}

	remaining, err := f.q.ListSaleDraftItems(ctx, db.ListSaleDraftItemsParams{ShopID: f.shopID, SaleDraftID: draft.ID})
	if err != nil {
		t.Fatalf("ListSaleDraftItems (after cascade delete): %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("want 0 items left after the draft's cascade delete, got %d (items were %v)", len(remaining), items)
	}

	// Deleting an already-gone draft affects 0 rows, not an error.
	affected2, err := f.q.DeleteSaleDraft(ctx, db.DeleteSaleDraftParams{ShopID: f.shopID, ID: draft.ID})
	if err != nil {
		t.Fatalf("DeleteSaleDraft (second call): %v", err)
	}
	if affected2 != 0 {
		t.Fatalf("want 0 rows affected deleting an already-deleted draft, got %d", affected2)
	}
}

// TestSaleDrafts_foreignKeys proves every FK sale_drafts/sale_draft_items
// carry (location_id, customer_id, created_by, sale_draft_id, variant_id) is
// actually enforced by Postgres, not just assumed from the migration
// text.
func TestSaleDrafts_foreignKeys(t *testing.T) {
	f := newSaleDraftsFixture(t, "shop-drafts-fk")
	ctx := context.Background()
	bogus := uuid.New()

	if _, err := f.q.CreateSaleDraft(ctx, db.CreateSaleDraftParams{
		ID: uuid.New(), ShopID: f.shopID, LocationID: bogus,
	}); !isForeignKeyViolation(err) {
		t.Errorf("want a foreign key violation for a nonexistent location_id, got: %v", err)
	}

	if _, err := f.q.CreateSaleDraft(ctx, db.CreateSaleDraftParams{
		ID: uuid.New(), ShopID: f.shopID, LocationID: f.locationID, CustomerID: &bogus,
	}); !isForeignKeyViolation(err) {
		t.Errorf("want a foreign key violation for a nonexistent customer_id, got: %v", err)
	}

	if _, err := f.q.CreateSaleDraft(ctx, db.CreateSaleDraftParams{
		ID: uuid.New(), ShopID: f.shopID, LocationID: f.locationID, CreatedBy: &bogus,
	}); !isForeignKeyViolation(err) {
		t.Errorf("want a foreign key violation for a nonexistent created_by, got: %v", err)
	}

	draft, err := f.q.CreateSaleDraft(ctx, db.CreateSaleDraftParams{ID: uuid.New(), ShopID: f.shopID, LocationID: f.locationID})
	if err != nil {
		t.Fatalf("CreateSaleDraft: %v", err)
	}

	if _, err := f.q.InsertSaleDraftItem(ctx, db.InsertSaleDraftItemParams{
		ID: uuid.New(), ShopID: f.shopID, SaleDraftID: bogus, VariantID: f.variantID, Qty: numeric(t, "1.000"), Position: 0,
	}); !isForeignKeyViolation(err) {
		t.Errorf("want a foreign key violation for a nonexistent sale_draft_id, got: %v", err)
	}

	if _, err := f.q.InsertSaleDraftItem(ctx, db.InsertSaleDraftItemParams{
		ID: uuid.New(), ShopID: f.shopID, SaleDraftID: draft.ID, VariantID: bogus, Qty: numeric(t, "1.000"), Position: 0,
	}); !isForeignKeyViolation(err) {
		t.Errorf("want a foreign key violation for a nonexistent variant_id, got: %v", err)
	}

	// A well-formed item, for contrast: all the same FKs, real ids, succeeds.
	if _, err := f.q.InsertSaleDraftItem(ctx, db.InsertSaleDraftItemParams{
		ID: uuid.New(), ShopID: f.shopID, SaleDraftID: draft.ID, VariantID: f.variantID, Qty: numeric(t, "1.000"), Position: 0,
	}); err != nil {
		t.Errorf("want a well-formed item to succeed, got: %v", err)
	}
}

// TestSaleDraftItems_qtyMustBePositive pins the qty > 0 CHECK, the same
// rule sale_items.qty and purchase_items.qty already carry.
func TestSaleDraftItems_qtyMustBePositive(t *testing.T) {
	f := newSaleDraftsFixture(t, "shop-drafts-qty-check")
	ctx := context.Background()
	draft, err := f.q.CreateSaleDraft(ctx, db.CreateSaleDraftParams{ID: uuid.New(), ShopID: f.shopID, LocationID: f.locationID})
	if err != nil {
		t.Fatalf("CreateSaleDraft: %v", err)
	}

	newItem := func(qty string) error {
		_, err := f.q.InsertSaleDraftItem(ctx, db.InsertSaleDraftItemParams{
			ID: uuid.New(), ShopID: f.shopID, SaleDraftID: draft.ID, VariantID: f.variantID, Qty: numeric(t, qty), Position: 0,
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

func strPtr(s string) *string { return &s }

// isForeignKeyViolation reports whether err is a Postgres foreign_key_violation
// (SQLSTATE 23503), the same check sqlc's pgconn error exposes for every FK
// test in this package.
func isForeignKeyViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "SQLSTATE 23503")
}
