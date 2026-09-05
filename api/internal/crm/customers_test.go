package crm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oapi-codegen/nullable"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/crm"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// mustCreateCustomer mirrors suppliers_test.go's mustCreateSupplier.
func mustCreateCustomer(ctx context.Context, t *testing.T, h *crm.Handler, fullName string) gen.Customer {
	t.Helper()
	resp, err := h.CreateCustomer(ctx, gen.CreateCustomerRequestObject{Body: &gen.CustomerCreate{FullName: fullName}})
	if err != nil {
		t.Fatalf("CreateCustomer(%q): %v", fullName, err)
	}
	created, ok := resp.(gen.CreateCustomer201JSONResponse)
	if !ok {
		t.Fatalf("CreateCustomer(%q) response type = %T", fullName, resp)
	}
	return gen.Customer(created)
}

func TestCreateCustomer_minimalAndFullFields(t *testing.T) {
	h, q := newTestHandler(t)
	shop := seedShop(context.Background(), t, q, "create-customer")
	ctx := ctxAs(shop.ID, db.UserRoleManager)

	minimal := mustCreateCustomer(ctx, t, h, "  Bare Customer  ")
	if minimal.FullName != "Bare Customer" {
		t.Fatalf("FullName = %q, want trimmed %q", minimal.FullName, "Bare Customer")
	}
	if minimal.Phone.IsSpecified() && !minimal.Phone.IsNull() {
		t.Fatalf("Phone = %+v, want unset/null", minimal.Phone)
	}
	if minimal.Tags == nil || len(minimal.Tags) != 0 {
		t.Fatalf("Tags = %+v, want non-nil empty slice", minimal.Tags)
	}

	resp, err := h.CreateCustomer(ctx, gen.CreateCustomerRequestObject{Body: &gen.CustomerCreate{
		FullName: "Full Customer", Phone: strPtr("  +998901234567  "),
		TelegramUsername: strPtr("full_customer"), Note: strPtr("VIP"),
		Tags: &[]string{"vip", "wholesale"},
	}})
	if err != nil {
		t.Fatalf("CreateCustomer(full): %v", err)
	}
	full, ok := resp.(gen.CreateCustomer201JSONResponse)
	if !ok {
		t.Fatalf("CreateCustomer(full) response type = %T", resp)
	}
	// Phone is trimmed but otherwise stored exactly as given — no
	// digit-only normalisation.
	if !full.Phone.IsSpecified() || full.Phone.MustGet() != "+998901234567" {
		t.Fatalf("Phone = %+v, want trimmed +998901234567", full.Phone)
	}
	if len(full.Tags) != 2 || full.Tags[0] != "vip" || full.Tags[1] != "wholesale" {
		t.Fatalf("Tags = %+v, want [vip wholesale]", full.Tags)
	}
}

func TestCreateCustomer_fullNameValidation(t *testing.T) {
	h, q := newTestHandler(t)
	shop := seedShop(context.Background(), t, q, "customer-validation")
	ctx := ctxAs(shop.ID, db.UserRoleManager)

	_, err := h.CreateCustomer(ctx, gen.CreateCustomerRequestObject{Body: &gen.CustomerCreate{FullName: "   "}})
	if err == nil {
		t.Fatal("CreateCustomer(blank fullName): want 400 VALIDATION_FAILED, got nil error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 400 || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("error = %v, want 400 VALIDATION_FAILED", err)
	}
}

func TestCreateCustomer_duplicateActivePhoneConflicts(t *testing.T) {
	h, q := newTestHandler(t)
	shop := seedShop(context.Background(), t, q, "dup-customer-phone")
	ctxOwner := ctxAs(shop.ID, db.UserRoleOwner)

	_, err := h.CreateCustomer(ctxOwner, gen.CreateCustomerRequestObject{Body: &gen.CustomerCreate{
		FullName: "First Customer", Phone: strPtr("+998900000001"),
	}})
	if err != nil {
		t.Fatalf("CreateCustomer(first): %v", err)
	}

	_, err = h.CreateCustomer(ctxOwner, gen.CreateCustomerRequestObject{Body: &gen.CustomerCreate{
		FullName: "Second Customer", Phone: strPtr("+998900000001"),
	}})
	if err == nil {
		t.Fatal("second CreateCustomer with the same phone: want 409 CONFLICT, got nil error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 409 || apiErr.Code != gen.CONFLICT {
		t.Fatalf("error = %v, want 409 CONFLICT", err)
	}
	if apiErr.Details["field"] != "phone" {
		t.Fatalf("details.field = %v, want phone", apiErr.Details["field"])
	}
}

func TestCreateCustomer_samePhoneAcrossShopsOk(t *testing.T) {
	h, q := newTestHandler(t)
	shopA := seedShop(context.Background(), t, q, "customer-phone-shop-a")
	shopB := seedShop(context.Background(), t, q, "customer-phone-shop-b")
	ctxA := ctxAs(shopA.ID, db.UserRoleOwner)
	ctxB := ctxAs(shopB.ID, db.UserRoleOwner)

	if _, err := h.CreateCustomer(ctxA, gen.CreateCustomerRequestObject{Body: &gen.CustomerCreate{
		FullName: "Shop A Customer", Phone: strPtr("+998900000002"),
	}}); err != nil {
		t.Fatalf("CreateCustomer(shop A): %v", err)
	}
	if _, err := h.CreateCustomer(ctxB, gen.CreateCustomerRequestObject{Body: &gen.CustomerCreate{
		FullName: "Shop B Customer", Phone: strPtr("+998900000002"),
	}}); err != nil {
		t.Fatalf("CreateCustomer(shop B, same phone): want ok, got %v", err)
	}
}

func TestCreateCustomer_phoneFreedAfterSoftDelete(t *testing.T) {
	h, q := newTestHandler(t)
	shop := seedShop(context.Background(), t, q, "reuse-customer-phone")
	ctx := ctxAs(shop.ID, db.UserRoleOwner)

	resp, err := h.CreateCustomer(ctx, gen.CreateCustomerRequestObject{Body: &gen.CustomerCreate{
		FullName: "Reusable Phone", Phone: strPtr("+998900000003"),
	}})
	if err != nil {
		t.Fatalf("CreateCustomer(first): %v", err)
	}
	first := gen.Customer(resp.(gen.CreateCustomer201JSONResponse))

	if _, err := h.DeleteCustomer(ctx, gen.DeleteCustomerRequestObject{Id: first.Id}); err != nil {
		t.Fatalf("DeleteCustomer: %v", err)
	}

	// The partial unique index only covers live rows (deleted_at IS
	// NULL), so the phone is free again — same reasoning as
	// suppliers_shop_id_name_key.
	if _, err := h.CreateCustomer(ctx, gen.CreateCustomerRequestObject{Body: &gen.CustomerCreate{
		FullName: "New Owner Of Phone", Phone: strPtr("+998900000003"),
	}}); err != nil {
		t.Fatalf("CreateCustomer(reuse phone after delete): want ok, got %v", err)
	}
}

func TestUpdateCustomer_nullClearsNullableFields(t *testing.T) {
	h, q := newTestHandler(t)
	shop := seedShop(context.Background(), t, q, "update-customer")
	ctx := ctxAs(shop.ID, db.UserRoleManager)

	created := mustCreateCustomer(ctx, t, h, "Updatable")
	if _, err := h.UpdateCustomer(ctx, gen.UpdateCustomerRequestObject{Id: created.Id, Body: &gen.CustomerPatch{
		Phone: nullable.NewNullableWithValue("+998900000004"), Note: nullable.NewNullableWithValue("first note"),
		Tags: &[]string{"a", "b"},
	}}); err != nil {
		t.Fatalf("UpdateCustomer (set): %v", err)
	}

	// Explicit null clears note; fullName and phone (absent) stay
	// unchanged (D-35).
	resp, err := h.UpdateCustomer(ctx, gen.UpdateCustomerRequestObject{Id: created.Id, Body: &gen.CustomerPatch{
		Note: nullable.NewNullNullable[string](),
	}})
	if err != nil {
		t.Fatalf("UpdateCustomer (clear): %v", err)
	}
	updated, ok := resp.(gen.UpdateCustomer200JSONResponse)
	if !ok {
		t.Fatalf("UpdateCustomer response type = %T", resp)
	}
	if updated.Note.IsSpecified() && !updated.Note.IsNull() {
		t.Fatalf("Note = %+v, want cleared to null", updated.Note)
	}
	if !updated.Phone.IsSpecified() || updated.Phone.MustGet() != "+998900000004" {
		t.Fatalf("Phone = %+v, want unchanged +998900000004", updated.Phone)
	}
	if updated.FullName != "Updatable" {
		t.Fatalf("FullName = %q, want unchanged \"Updatable\"", updated.FullName)
	}
	if len(updated.Tags) != 2 || updated.Tags[0] != "a" || updated.Tags[1] != "b" {
		t.Fatalf("Tags = %+v, want unchanged [a b]", updated.Tags)
	}
}

func TestUpdateCustomer_blankFullNameRejected(t *testing.T) {
	h, q := newTestHandler(t)
	shop := seedShop(context.Background(), t, q, "update-customer-blank-name")
	ctx := ctxAs(shop.ID, db.UserRoleOwner)

	created := mustCreateCustomer(ctx, t, h, "Named Customer")
	blank := "   "
	_, err := h.UpdateCustomer(ctx, gen.UpdateCustomerRequestObject{Id: created.Id, Body: &gen.CustomerPatch{FullName: &blank}})
	if err == nil {
		t.Fatal("UpdateCustomer(blank fullName): want 400 VALIDATION_FAILED, got nil error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 400 || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("error = %v, want 400 VALIDATION_FAILED", err)
	}
}

func TestGetCustomer_notFoundForOtherShopOrDeleted(t *testing.T) {
	h, q := newTestHandler(t)
	shopA := seedShop(context.Background(), t, q, "get-customer-a")
	shopB := seedShop(context.Background(), t, q, "get-customer-b")
	ctxA := ctxAs(shopA.ID, db.UserRoleManager)
	ctxB := ctxAs(shopB.ID, db.UserRoleManager)

	created := mustCreateCustomer(ctxA, t, h, "Shop A Customer")

	if _, err := h.GetCustomer(ctxB, gen.GetCustomerRequestObject{Id: created.Id}); err == nil {
		t.Fatal("GetCustomer from another shop: want 404, got nil error")
	} else {
		var apiErr *apierr.Error
		if !errors.As(err, &apiErr) || apiErr.Status != 404 {
			t.Fatalf("error = %v, want 404 NOT_FOUND", err)
		}
	}

	if _, err := h.DeleteCustomer(ctxA, gen.DeleteCustomerRequestObject{Id: created.Id}); err != nil {
		t.Fatalf("DeleteCustomer: %v", err)
	}
	if _, err := h.GetCustomer(ctxA, gen.GetCustomerRequestObject{Id: created.Id}); err == nil {
		t.Fatal("GetCustomer after delete: want 404, got nil error")
	} else {
		var apiErr *apierr.Error
		if !errors.As(err, &apiErr) || apiErr.Status != 404 {
			t.Fatalf("error = %v, want 404 NOT_FOUND", err)
		}
	}

	// A second delete on an already-deleted customer is also a 404.
	if _, err := h.DeleteCustomer(ctxA, gen.DeleteCustomerRequestObject{Id: created.Id}); err == nil {
		t.Fatal("DeleteCustomer twice: want 404, got nil error")
	} else {
		var apiErr *apierr.Error
		if !errors.As(err, &apiErr) || apiErr.Status != 404 {
			t.Fatalf("error = %v, want 404 NOT_FOUND", err)
		}
	}
}

func TestListCustomers_isolatedPerShopAndSearch(t *testing.T) {
	h, q := newTestHandler(t)
	shopA := seedShop(context.Background(), t, q, "list-customer-a")
	shopB := seedShop(context.Background(), t, q, "list-customer-b")
	ctxA := ctxAs(shopA.ID, db.UserRoleManager)
	ctxB := ctxAs(shopB.ID, db.UserRoleManager)

	mustCreateCustomer(ctxA, t, h, "Tashkent Textiles")
	mustCreateCustomer(ctxA, t, h, "Fergana Fabrics")
	mustCreateCustomer(ctxB, t, h, "Other Shop Customer")

	resp, err := h.ListCustomers(ctxA, gen.ListCustomersRequestObject{})
	if err != nil {
		t.Fatalf("ListCustomers(shop A): %v", err)
	}
	list, ok := resp.(gen.ListCustomers200JSONResponse)
	if !ok {
		t.Fatalf("ListCustomers response type = %T", resp)
	}
	if len(list.Items) != 2 {
		t.Fatalf("shop A customers = %d, want 2", len(list.Items))
	}
	for _, item := range list.Items {
		if item.FullName == "Other Shop Customer" {
			t.Fatalf("shop A list leaked a shop B customer: %+v", item)
		}
	}

	q2 := "tashkent"
	filtered, err := h.ListCustomers(ctxA, gen.ListCustomersRequestObject{Params: gen.ListCustomersParams{Q: &q2}})
	if err != nil {
		t.Fatalf("ListCustomers(q=tashkent): %v", err)
	}
	filteredList, ok := filtered.(gen.ListCustomers200JSONResponse)
	if !ok {
		t.Fatalf("ListCustomers(q) response type = %T", filtered)
	}
	if len(filteredList.Items) != 1 || filteredList.Items[0].FullName != "Tashkent Textiles" {
		t.Fatalf("ListCustomers(q=tashkent) = %+v, want exactly Tashkent Textiles", filteredList.Items)
	}
}

func TestListCustomers_cursorPaginates(t *testing.T) {
	h, q := newTestHandler(t)
	shop := seedShop(context.Background(), t, q, "cursor-customer")
	ctx := ctxAs(shop.ID, db.UserRoleManager)

	names := []string{"Customer One", "Customer Two", "Customer Three"}
	for _, n := range names {
		mustCreateCustomer(ctx, t, h, n)
	}

	limit := 2
	first, err := h.ListCustomers(ctx, gen.ListCustomersRequestObject{Params: gen.ListCustomersParams{Limit: &limit}})
	if err != nil {
		t.Fatalf("ListCustomers(page 1): %v", err)
	}
	page1, ok := first.(gen.ListCustomers200JSONResponse)
	if !ok {
		t.Fatalf("ListCustomers(page 1) response type = %T", first)
	}
	if len(page1.Items) != 2 {
		t.Fatalf("page 1 items = %d, want 2", len(page1.Items))
	}
	if !page1.NextCursor.IsSpecified() || page1.NextCursor.IsNull() {
		t.Fatalf("page 1 NextCursor = %+v, want a cursor", page1.NextCursor)
	}
	cursor := page1.NextCursor.MustGet()

	second, err := h.ListCustomers(ctx, gen.ListCustomersRequestObject{Params: gen.ListCustomersParams{Limit: &limit, Cursor: &cursor}})
	if err != nil {
		t.Fatalf("ListCustomers(page 2): %v", err)
	}
	page2, ok := second.(gen.ListCustomers200JSONResponse)
	if !ok {
		t.Fatalf("ListCustomers(page 2) response type = %T", second)
	}
	if len(page2.Items) != 1 {
		t.Fatalf("page 2 items = %d, want 1", len(page2.Items))
	}
	if page2.NextCursor.IsSpecified() && !page2.NextCursor.IsNull() {
		t.Fatalf("page 2 NextCursor = %+v, want none (last page)", page2.NextCursor)
	}

	seen := map[string]bool{}
	for _, item := range append(page1.Items, page2.Items...) {
		seen[item.FullName] = true
	}
	for _, n := range names {
		if !seen[n] {
			t.Fatalf("cursor pagination missed %q; seen = %+v", n, seen)
		}
	}
}

func TestCustomers_cashierCanCreateAndGetButNotPatchOrDelete(t *testing.T) {
	h, q := newTestHandler(t)
	shop := seedShop(context.Background(), t, q, "customer-cashier")
	owner := ctxAs(shop.ID, db.UserRoleOwner)
	cashier := ctxAs(shop.ID, db.UserRoleCashier)

	created := mustCreateCustomer(owner, t, h, "Cashier Managed")

	if _, err := h.ListCustomers(cashier, gen.ListCustomersRequestObject{}); err != nil {
		t.Fatalf("cashier ListCustomers: want ok, got %v", err)
	}
	if _, err := h.GetCustomer(cashier, gen.GetCustomerRequestObject{Id: created.Id}); err != nil {
		t.Fatalf("cashier GetCustomer: want ok, got %v", err)
	}
	if _, err := h.CreateCustomer(cashier, gen.CreateCustomerRequestObject{Body: &gen.CustomerCreate{FullName: "Cashier Created"}}); err != nil {
		t.Fatalf("cashier CreateCustomer: want ok, got %v", err)
	}

	assertForbidden(t, "cashier UpdateCustomer", func() error {
		_, err := h.UpdateCustomer(cashier, gen.UpdateCustomerRequestObject{Id: created.Id, Body: &gen.CustomerPatch{}})
		return err
	})
	assertForbidden(t, "cashier DeleteCustomer", func() error {
		_, err := h.DeleteCustomer(cashier, gen.DeleteCustomerRequestObject{Id: created.Id})
		return err
	})
}
