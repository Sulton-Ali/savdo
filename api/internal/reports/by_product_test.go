package reports_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/reports"
)

// byProductParams builds a manager+ ListSalesByProductRequestObject for
// [from, to] inclusive.
func byProductParams(from, to time.Time, limit *int, cursor *string) gen.ListSalesByProductRequestObject {
	return gen.ListSalesByProductRequestObject{Params: gen.ListSalesByProductParams{
		From: *dateParam(from), To: *dateParam(to), Limit: limit, Cursor: cursor,
	}}
}

func TestListSalesByProduct_managerD64(t *testing.T) {
	_, q := newTestQueries(t)
	ctx := context.Background()
	h := reports.NewHandler(reports.NewService(q))

	f := newD64Fixture(ctx, t, q, "byproduct-d64")
	owner := seedUser(ctx, t, q, f.shopID, "owner1", db.UserRoleOwner)

	now := time.Now()
	resp, err := h.ListSalesByProduct(ctxAs(f.shopID, owner), byProductParams(now.AddDate(0, 0, -1), now.AddDate(0, 0, 1), nil, nil))
	if err != nil {
		t.Fatalf("ListSalesByProduct: %v", err)
	}
	body := resp.(gen.ListSalesByProduct200JSONResponse)

	if len(body.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(body.Items))
	}
	row := body.Items[0]
	if row.ProductId != f.productID {
		t.Fatalf("productId = %s, want %s", row.ProductId, f.productID)
	}
	assertDecimal(t, "qtySold", row.QtySold, "2.000")
	assertDecimal(t, "qtyReturned", row.QtyReturned, "1.000")
	assertDecimal(t, "revenue", row.Revenue, "45.00")
	if row.Cost == nil {
		t.Fatal("want cost present for manager+, got nil")
	}
	assertDecimal(t, "cost", *row.Cost, "10.00")
	if row.Margin == nil {
		t.Fatal("want margin present for manager+, got nil")
	}
	assertDecimal(t, "margin", *row.Margin, "35.00")
}

// TestListSalesByProduct_voidedSaleExcluded proves a voided sale's items
// never contribute to another product's by-product row (they simply never
// appear as their own row either, since SalesByProduct filters on
// status = 'completed' like every other report query).
func TestListSalesByProduct_voidedSaleExcluded(t *testing.T) {
	_, q := newTestQueries(t)
	ctx := context.Background()
	h := reports.NewHandler(reports.NewService(q))

	f := newD64Fixture(ctx, t, q, "byproduct-voided")
	owner := seedUser(ctx, t, q, f.shopID, "owner1", db.UserRoleOwner)
	unit := seedUnit(ctx, t, q, f.shopID, "kg")
	otherProduct := seedProduct(ctx, t, q, f.shopID, unit.ID, "byproduct-voided-other")
	otherVariant := seedVariant(ctx, t, q, f.shopID, otherProduct.ID)

	extra, _ := newSale(ctx, t, q, f.shopID, f.locationID, f.cashierID, db.SaleKindSale, nil,
		"20.00", "0.00", "20.00", []saleItemSpec{
			{variantID: otherVariant.ID, qty: "1.000", unitPrice: "20.00", unitCost: "5.00", lineTotal: "20.00"},
		})
	reason := "test void"
	if _, err := q.VoidSale(ctx, db.VoidSaleParams{ShopID: f.shopID, ID: extra.ID, VoidedBy: &owner.ID, VoidReason: &reason}); err != nil {
		t.Fatalf("VoidSale: %v", err)
	}

	now := time.Now()
	resp, err := h.ListSalesByProduct(ctxAs(f.shopID, owner), byProductParams(now.AddDate(0, 0, -1), now.AddDate(0, 0, 1), nil, nil))
	if err != nil {
		t.Fatalf("ListSalesByProduct: %v", err)
	}
	body := resp.(gen.ListSalesByProduct200JSONResponse)

	// Only the D-64 fixture's own product — the voided sale's product
	// never appears as a row at all.
	if len(body.Items) != 1 {
		t.Fatalf("items = %d, want 1 (voided sale's product excluded)", len(body.Items))
	}
	if body.Items[0].ProductId != f.productID {
		t.Fatalf("productId = %s, want %s", body.Items[0].ProductId, f.productID)
	}
}

func TestListSalesByProduct_cashierForbidden(t *testing.T) {
	_, q := newTestQueries(t)
	ctx := context.Background()
	h := reports.NewHandler(reports.NewService(q))

	shopRow := seedShop(ctx, t, q, "byproduct-cashier")
	cashier := seedUser(ctx, t, q, shopRow.ID, "cashier1", db.UserRoleCashier)

	now := time.Now()
	_, err := h.ListSalesByProduct(ctxAs(shopRow.ID, cashier), byProductParams(now, now, nil, nil))
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 403 {
		t.Fatalf("error = %v, want 403 FORBIDDEN", err)
	}
}

// TestListSalesByProduct_cursorPagination walks three products with equal
// revenue (10.00 each) one page at a time (limit=1), proving the
// (revenue, product_id) keyset never skips or repeats a row when the
// primary sort key ties — product_id DESC is what breaks the tie
// (api/db/queries/reports.sql's SalesByProduct ORDER BY).
func TestListSalesByProduct_cursorPagination(t *testing.T) {
	_, q := newTestQueries(t)
	ctx := context.Background()
	h := reports.NewHandler(reports.NewService(q))

	shopRow := seedShop(ctx, t, q, "byproduct-cursor")
	owner := seedUser(ctx, t, q, shopRow.ID, "owner1", db.UserRoleOwner)
	cashier := seedUser(ctx, t, q, shopRow.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")
	loc := seedLocation(ctx, t, q, shopRow.ID, "Main")

	want := map[uuid.UUID]bool{}
	for i := 0; i < 3; i++ {
		product := seedProduct(ctx, t, q, shopRow.ID, unit.ID, "byproduct-cursor-"+uuid.NewString())
		variant := seedVariant(ctx, t, q, shopRow.ID, product.ID)
		newSale(ctx, t, q, shopRow.ID, loc.ID, cashier.ID, db.SaleKindSale, nil,
			"10.00", "0.00", "10.00", []saleItemSpec{
				{variantID: variant.ID, qty: "1.000", unitPrice: "10.00", unitCost: "1.00", lineTotal: "10.00"},
			})
		want[product.ID] = true
	}

	now := time.Now()
	from, to := now.AddDate(0, 0, -1), now.AddDate(0, 0, 1)
	limit := 1
	got := map[uuid.UUID]bool{}
	var cursor *string
	for page := 0; page < 10; page++ {
		resp, err := h.ListSalesByProduct(ctxAs(shopRow.ID, owner), byProductParams(from, to, &limit, cursor))
		if err != nil {
			t.Fatalf("ListSalesByProduct page %d: %v", page, err)
		}
		body := resp.(gen.ListSalesByProduct200JSONResponse)
		if len(body.Items) != 1 {
			t.Fatalf("page %d: items = %d, want 1", page, len(body.Items))
		}
		id := body.Items[0].ProductId
		if got[id] {
			t.Fatalf("page %d: product %s returned twice", page, id)
		}
		got[id] = true

		next, err := body.NextCursor.Get()
		if err != nil || next == "" {
			break
		}
		cursor = &next
	}

	if len(got) != len(want) {
		t.Fatalf("walked %d distinct products, want %d", len(got), len(want))
	}
	for id := range want {
		if !got[id] {
			t.Fatalf("product %s never returned by any page", id)
		}
	}
}
