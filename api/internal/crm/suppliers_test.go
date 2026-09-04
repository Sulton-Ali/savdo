package crm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/crm"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

// newTestHandler builds a crm.Handler backed by a real (testcontainers)
// Postgres, truncated for isolation.
func newTestHandler(t *testing.T) (*crm.Handler, *db.Queries) {
	t.Helper()
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	q := db.New(pool)
	return crm.NewHandler(crm.NewService(q)), q
}

func seedShop(ctx context.Context, t *testing.T, q *db.Queries, slug string) db.Shop {
	t.Helper()
	shopRow, err := q.CreateShop(ctx, db.CreateShopParams{ID: uuid.New(), Slug: slug, Name: "Crm Shop " + slug})
	if err != nil {
		t.Fatalf("seedShop(%q): %v", slug, err)
	}
	return shopRow
}

// ctxAs builds a context carrying the auth.Context a real request would
// have after auth.Service.Middleware ran, for shopID as role — mirrors
// catalog_test.go's own ctxAs.
func ctxAs(shopID uuid.UUID, role db.UserRole) context.Context {
	return auth.WithContext(context.Background(), auth.Context{
		ShopID: shopID, UserID: uuid.New(), Role: role, SessionID: uuid.New(), Client: db.SessionClientWeb,
	})
}

func strPtr(s string) *string { return &s }

func mustCreateSupplier(ctx context.Context, t *testing.T, h *crm.Handler, name string) gen.Supplier {
	t.Helper()
	resp, err := h.CreateSupplier(ctx, gen.CreateSupplierRequestObject{Body: &gen.SupplierCreate{Name: name}})
	if err != nil {
		t.Fatalf("CreateSupplier(%q): %v", name, err)
	}
	created, ok := resp.(gen.CreateSupplier201JSONResponse)
	if !ok {
		t.Fatalf("CreateSupplier(%q) response type = %T", name, resp)
	}
	return gen.Supplier(created)
}

func TestCreateSupplier_minimalAndFullFields(t *testing.T) {
	h, q := newTestHandler(t)
	shop := seedShop(context.Background(), t, q, "create-supplier")
	ctx := ctxAs(shop.ID, db.UserRoleManager)

	minimal := mustCreateSupplier(ctx, t, h, "Bare Supplier")
	if minimal.Name != "Bare Supplier" {
		t.Fatalf("Name = %q, want %q", minimal.Name, "Bare Supplier")
	}
	if minimal.ContactName.IsSpecified() && !minimal.ContactName.IsNull() {
		t.Fatalf("ContactName = %+v, want unset/null", minimal.ContactName)
	}

	resp, err := h.CreateSupplier(ctx, gen.CreateSupplierRequestObject{Body: &gen.SupplierCreate{
		Name: "Full Supplier", ContactName: strPtr("Aziz"), Phone: strPtr("+998901234567"),
		TelegramUsername: strPtr("aziz_supplier"), Note: strPtr("Reliable"),
	}})
	if err != nil {
		t.Fatalf("CreateSupplier(full): %v", err)
	}
	full, ok := resp.(gen.CreateSupplier201JSONResponse)
	if !ok {
		t.Fatalf("CreateSupplier(full) response type = %T", resp)
	}
	if !full.ContactName.IsSpecified() || full.ContactName.IsNull() || full.ContactName.MustGet() != "Aziz" {
		t.Fatalf("ContactName = %+v, want \"Aziz\"", full.ContactName)
	}
	if !full.Phone.IsSpecified() || full.Phone.MustGet() != "+998901234567" {
		t.Fatalf("Phone = %+v, want +998901234567", full.Phone)
	}
}

func TestCreateSupplier_duplicateActiveNameConflicts(t *testing.T) {
	h, q := newTestHandler(t)
	shop := seedShop(context.Background(), t, q, "dup-supplier")
	ctx := ctxAs(shop.ID, db.UserRoleOwner)

	mustCreateSupplier(ctx, t, h, "Same Name")

	_, err := h.CreateSupplier(ctx, gen.CreateSupplierRequestObject{Body: &gen.SupplierCreate{Name: "Same Name"}})
	if err == nil {
		t.Fatal("second CreateSupplier with the same name: want 409 CONFLICT, got nil error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 409 || apiErr.Code != gen.CONFLICT {
		t.Fatalf("error = %v, want 409 CONFLICT", err)
	}
	if apiErr.Details["field"] != "name" {
		t.Fatalf("details.field = %v, want name", apiErr.Details["field"])
	}
}

func TestCreateSupplier_nameFreedAfterSoftDelete(t *testing.T) {
	h, q := newTestHandler(t)
	shop := seedShop(context.Background(), t, q, "reuse-supplier-name")
	ctx := ctxAs(shop.ID, db.UserRoleOwner)

	first := mustCreateSupplier(ctx, t, h, "Reusable Name")

	if _, err := h.DeleteSupplier(ctx, gen.DeleteSupplierRequestObject{Id: first.Id}); err != nil {
		t.Fatalf("DeleteSupplier: %v", err)
	}

	// The partial unique index only covers live rows (deleted_at IS NULL),
	// so the name is free again — same reasoning as
	// categories_shop_id_slug_key.
	second := mustCreateSupplier(ctx, t, h, "Reusable Name")
	if second.Id == first.Id {
		t.Fatal("second CreateSupplier reused the first's id")
	}
}

func TestUpdateSupplier_nullClearsNullableFields(t *testing.T) {
	h, q := newTestHandler(t)
	shop := seedShop(context.Background(), t, q, "update-supplier")
	ctx := ctxAs(shop.ID, db.UserRoleManager)

	created := mustCreateSupplier(ctx, t, h, "Updatable")
	if _, err := h.UpdateSupplier(ctx, gen.UpdateSupplierRequestObject{Id: created.Id, Body: &gen.SupplierPatch{
		ContactName: nullable.NewNullableWithValue("Dilnoza"), Phone: nullable.NewNullableWithValue("+998900000000"),
	}}); err != nil {
		t.Fatalf("UpdateSupplier (set): %v", err)
	}

	// Explicit null clears; name (absent) stays unchanged (D-35).
	resp, err := h.UpdateSupplier(ctx, gen.UpdateSupplierRequestObject{Id: created.Id, Body: &gen.SupplierPatch{
		ContactName: nullable.NewNullNullable[string](),
	}})
	if err != nil {
		t.Fatalf("UpdateSupplier (clear): %v", err)
	}
	updated, ok := resp.(gen.UpdateSupplier200JSONResponse)
	if !ok {
		t.Fatalf("UpdateSupplier response type = %T", resp)
	}
	if updated.ContactName.IsSpecified() && !updated.ContactName.IsNull() {
		t.Fatalf("ContactName = %+v, want cleared to null", updated.ContactName)
	}
	if !updated.Phone.IsSpecified() || updated.Phone.MustGet() != "+998900000000" {
		t.Fatalf("Phone = %+v, want unchanged +998900000000", updated.Phone)
	}
	if updated.Name != "Updatable" {
		t.Fatalf("Name = %q, want unchanged \"Updatable\"", updated.Name)
	}
}

func TestGetSupplier_notFoundForOtherShopOrDeleted(t *testing.T) {
	h, q := newTestHandler(t)
	shopA := seedShop(context.Background(), t, q, "get-supplier-a")
	shopB := seedShop(context.Background(), t, q, "get-supplier-b")
	ctxA := ctxAs(shopA.ID, db.UserRoleManager)
	ctxB := ctxAs(shopB.ID, db.UserRoleManager)

	created := mustCreateSupplier(ctxA, t, h, "Shop A Supplier")

	if _, err := h.GetSupplier(ctxB, gen.GetSupplierRequestObject{Id: created.Id}); err == nil {
		t.Fatal("GetSupplier from another shop: want 404, got nil error")
	} else {
		var apiErr *apierr.Error
		if !errors.As(err, &apiErr) || apiErr.Status != 404 {
			t.Fatalf("error = %v, want 404 NOT_FOUND", err)
		}
	}

	if _, err := h.DeleteSupplier(ctxA, gen.DeleteSupplierRequestObject{Id: created.Id}); err != nil {
		t.Fatalf("DeleteSupplier: %v", err)
	}
	if _, err := h.GetSupplier(ctxA, gen.GetSupplierRequestObject{Id: created.Id}); err == nil {
		t.Fatal("GetSupplier after delete: want 404, got nil error")
	} else {
		var apiErr *apierr.Error
		if !errors.As(err, &apiErr) || apiErr.Status != 404 {
			t.Fatalf("error = %v, want 404 NOT_FOUND", err)
		}
	}
}

func TestListSuppliers_isolatedPerShopAndSearch(t *testing.T) {
	h, q := newTestHandler(t)
	shopA := seedShop(context.Background(), t, q, "list-supplier-a")
	shopB := seedShop(context.Background(), t, q, "list-supplier-b")
	ctxA := ctxAs(shopA.ID, db.UserRoleManager)
	ctxB := ctxAs(shopB.ID, db.UserRoleManager)

	mustCreateSupplier(ctxA, t, h, "Tashkent Textiles")
	mustCreateSupplier(ctxA, t, h, "Fergana Fabrics")
	mustCreateSupplier(ctxB, t, h, "Other Shop Supplier")

	resp, err := h.ListSuppliers(ctxA, gen.ListSuppliersRequestObject{})
	if err != nil {
		t.Fatalf("ListSuppliers(shop A): %v", err)
	}
	list, ok := resp.(gen.ListSuppliers200JSONResponse)
	if !ok {
		t.Fatalf("ListSuppliers response type = %T", resp)
	}
	if len(list.Items) != 2 {
		t.Fatalf("shop A suppliers = %d, want 2", len(list.Items))
	}
	for _, item := range list.Items {
		if item.Name == "Other Shop Supplier" {
			t.Fatalf("shop A list leaked a shop B supplier: %+v", item)
		}
	}

	q2 := "tashkent"
	filtered, err := h.ListSuppliers(ctxA, gen.ListSuppliersRequestObject{Params: gen.ListSuppliersParams{Q: &q2}})
	if err != nil {
		t.Fatalf("ListSuppliers(q=tashkent): %v", err)
	}
	filteredList, ok := filtered.(gen.ListSuppliers200JSONResponse)
	if !ok {
		t.Fatalf("ListSuppliers(q) response type = %T", filtered)
	}
	if len(filteredList.Items) != 1 || filteredList.Items[0].Name != "Tashkent Textiles" {
		t.Fatalf("ListSuppliers(q=tashkent) = %+v, want exactly Tashkent Textiles", filteredList.Items)
	}
}

func TestSuppliers_cashierForbiddenOnEveryRoute(t *testing.T) {
	h, q := newTestHandler(t)
	shop := seedShop(context.Background(), t, q, "supplier-cashier")
	owner := ctxAs(shop.ID, db.UserRoleOwner)
	cashier := ctxAs(shop.ID, db.UserRoleCashier)

	created := mustCreateSupplier(owner, t, h, "Cashier Blocked")

	assertForbidden(t, "ListSuppliers", func() error {
		_, err := h.ListSuppliers(cashier, gen.ListSuppliersRequestObject{})
		return err
	})
	assertForbidden(t, "GetSupplier", func() error {
		_, err := h.GetSupplier(cashier, gen.GetSupplierRequestObject{Id: created.Id})
		return err
	})
	assertForbidden(t, "CreateSupplier", func() error {
		_, err := h.CreateSupplier(cashier, gen.CreateSupplierRequestObject{Body: &gen.SupplierCreate{Name: "Nope"}})
		return err
	})
	assertForbidden(t, "UpdateSupplier", func() error {
		_, err := h.UpdateSupplier(cashier, gen.UpdateSupplierRequestObject{Id: created.Id, Body: &gen.SupplierPatch{}})
		return err
	})
	assertForbidden(t, "DeleteSupplier", func() error {
		_, err := h.DeleteSupplier(cashier, gen.DeleteSupplierRequestObject{Id: created.Id})
		return err
	})
}

func assertForbidden(t *testing.T, label string, call func() error) {
	t.Helper()
	err := call()
	if err == nil {
		t.Fatalf("%s: want 403 FORBIDDEN, got no error", label)
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 403 {
		t.Fatalf("%s: error = %v, want 403 FORBIDDEN", label, err)
	}
}
