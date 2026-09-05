package sales_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/sales"
)

// TestListSales_dateFilterExcludesOutOfRangeDays is the integration half
// of the shop-timezone date-filter acceptance check: the precise "instant
// near a shop-timezone day boundary" case is covered deterministically,
// without depending on wall-clock time, by the pure saleDateRange/
// dayBounds unit test in pricing_test.go (package sales, white-box) —
// sales.completed_at is set by the migration's own `now()` default and
// (like every other sales column) is immutable after insert, so this test
// cannot pin it to an exact instant to probe the boundary itself. This
// test instead confirms the query wiring end to end: a freshly completed
// sale is included when `from`/`to` cover today (shop timezone) and
// excluded when they name a day safely in the past.
func TestListSales_dateFilterExcludesOutOfRangeDays(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "list-sale-date")
	owner := seedUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "list-date-product", "10.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	ownerCtx := ctxAs(shop.ID, owner)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "5.000")

	if _, err := createSale(ownerCtx, t, h, pool, q, saleBody(loc.ID, variant.ID, "1.000")); err != nil {
		t.Fatalf("createSale: %v", err)
	}

	today := openapiDate(t, mustNow(t, q, shop.ID))
	past := openapiDate(t, mustNow(t, q, shop.ID).AddDate(0, 0, -30))

	resp, err := h.ListSales(ownerCtx, gen.ListSalesRequestObject{Params: gen.ListSalesParams{From: &today, To: &today}})
	if err != nil {
		t.Fatalf("ListSales (today): %v", err)
	}
	list, ok := resp.(gen.ListSales200JSONResponse)
	if !ok {
		t.Fatalf("ListSales response type = %T", resp)
	}
	if len(list.Items) != 1 {
		t.Fatalf("Items (today) = %+v, want exactly 1", list.Items)
	}

	resp, err = h.ListSales(ownerCtx, gen.ListSalesRequestObject{Params: gen.ListSalesParams{From: &past, To: &past}})
	if err != nil {
		t.Fatalf("ListSales (30 days ago): %v", err)
	}
	list, ok = resp.(gen.ListSales200JSONResponse)
	if !ok {
		t.Fatalf("ListSales response type = %T", resp)
	}
	if len(list.Items) != 0 {
		t.Fatalf("Items (30 days ago) = %+v, want 0", list.Items)
	}
}

func TestListSales_newestFirstAndFilters(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "list-sale-filters")
	owner := seedUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	cashierA := seedUser(ctx, t, q, shop.ID, "cashier-a", db.UserRoleCashier)
	cashierB := seedUser(ctx, t, q, shop.ID, "cashier-b", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "list-filters-product", "10.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "10.000")

	saleA, err := createSale(ctxAs(shop.ID, cashierA), t, h, pool, q, saleBody(loc.ID, variant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createSale (cashier A): %v", err)
	}
	saleB, err := createSale(ctxAs(shop.ID, cashierB), t, h, pool, q, saleBody(loc.ID, variant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createSale (cashier B): %v", err)
	}
	if saleA.Number == saleB.Number {
		t.Fatalf("sale numbers must differ: %d == %d", saleA.Number, saleB.Number)
	}

	ownerCtx := ctxAs(shop.ID, owner)

	// No filter: both, newest (B) first.
	resp, err := h.ListSales(ownerCtx, gen.ListSalesRequestObject{})
	if err != nil {
		t.Fatalf("ListSales: %v", err)
	}
	list, ok := resp.(gen.ListSales200JSONResponse)
	if !ok {
		t.Fatalf("ListSales response type = %T", resp)
	}
	if len(list.Items) != 2 || list.Items[0].Id != saleB.Id || list.Items[1].Id != saleA.Id {
		t.Fatalf("Items = %+v, want [B, A] newest first", list.Items)
	}

	// cashierId filter narrows to cashier A's own sale.
	cashierAID := cashierA.ID
	resp, err = h.ListSales(ownerCtx, gen.ListSalesRequestObject{Params: gen.ListSalesParams{CashierId: &cashierAID}})
	if err != nil {
		t.Fatalf("ListSales (cashierId filter): %v", err)
	}
	list, ok = resp.(gen.ListSales200JSONResponse)
	if !ok {
		t.Fatalf("ListSales response type = %T", resp)
	}
	if len(list.Items) != 1 || list.Items[0].Id != saleA.Id {
		t.Fatalf("Items (cashierId=A) = %+v, want exactly A's sale", list.Items)
	}

	// A cashier (D-63) sees every sale for the whole shop too, not just
	// their own.
	respCashier, err := h.ListSales(ctxAs(shop.ID, cashierB), gen.ListSalesRequestObject{})
	if err != nil {
		t.Fatalf("ListSales (as cashier B): %v", err)
	}
	listCashier, ok := respCashier.(gen.ListSales200JSONResponse)
	if !ok {
		t.Fatalf("ListSales response type = %T", respCashier)
	}
	if len(listCashier.Items) != 2 {
		t.Fatalf("Items (cashier B, no filter) = %+v, want both sales (D-63)", listCashier.Items)
	}
}

// walkAllSalePages drives h.ListSales to exhaustion (limit per page,
// following nextCursor until it comes back null) and returns every id
// seen, in the order returned.
func walkAllSalePages(ctx context.Context, t *testing.T, h *sales.Handler, limit int) []uuid.UUID {
	t.Helper()
	var ids []uuid.UUID
	var cursor *string
	for {
		l := limit
		resp, err := h.ListSales(ctx, gen.ListSalesRequestObject{Params: gen.ListSalesParams{Limit: &l, Cursor: cursor}})
		if err != nil {
			t.Fatalf("ListSales: %v", err)
		}
		list, ok := resp.(gen.ListSales200JSONResponse)
		if !ok {
			t.Fatalf("ListSales response type = %T", resp)
		}
		if len(list.Items) > limit {
			t.Fatalf("page size = %d, want at most %d", len(list.Items), limit)
		}
		for _, item := range list.Items {
			ids = append(ids, item.Id)
		}
		if !list.NextCursor.IsSpecified() || list.NextCursor.IsNull() {
			break
		}
		next := list.NextCursor.MustGet()
		cursor = &next
		if len(list.Items) == 0 {
			t.Fatal("nextCursor set but the page was empty — would loop forever")
		}
	}
	return ids
}

// TestListSales_cursorPaginationWalksAllPagesNoRepeats seeds more sales
// than one page holds and walks every page, for both the staff
// (cost.read) and cashier query, asserting the union of ids seen equals
// exactly the seeded set, with no repeats and nextCursor null on the last
// page. Each createSale call runs in its own transaction, so
// completed_at (`now()`) is distinct per sale in practice — the
// `(completed_at, id)` tie-break itself, for two rows sharing one
// instant, is exercised deterministically by
// TestListSales_cursorPaginationBreaksTiesByIdDescending below instead,
// which pins both to the same value directly.
func TestListSales_cursorPaginationWalksAllPagesNoRepeats(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "list-sale-pagination")
	owner := seedUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "pagination-product", "10.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	ownerCtx := ctxAs(shop.ID, owner)
	cashierCtx := ctxAs(shop.ID, cashier)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "20.000")

	const total = 7
	const pageSize = 3 // does not divide total evenly, so the last page is partial
	want := make(map[uuid.UUID]bool, total)
	for i := 0; i < total; i++ {
		sale, err := createSale(cashierCtx, t, h, pool, q, saleBody(loc.ID, variant.ID, "1.000"))
		if err != nil {
			t.Fatalf("createSale #%d: %v", i, err)
		}
		want[sale.Id] = true
	}

	assertExactlyTheSeededSet := func(t *testing.T, label string, got []uuid.UUID) {
		t.Helper()
		if len(got) != total {
			t.Fatalf("%s: walked %d ids, want %d", label, len(got), total)
		}
		seen := make(map[uuid.UUID]bool, len(got))
		for _, id := range got {
			if seen[id] {
				t.Fatalf("%s: id %s repeated across pages", label, id)
			}
			seen[id] = true
			if !want[id] {
				t.Fatalf("%s: id %s was never seeded", label, id)
			}
		}
	}

	assertExactlyTheSeededSet(t, "staff (owner, cost.read)", walkAllSalePages(ownerCtx, t, h, pageSize))
	assertExactlyTheSeededSet(t, "cashier", walkAllSalePages(cashierCtx, t, h, pageSize))
}

// TestListSales_cursorPaginationBreaksTiesByIdDescending pins two sales to
// the exact same completed_at (seedSaleRow, a raw INSERT — completed_at
// cannot be set to a chosen value any other way, since CreateSaleTx
// always uses the column's own `now()` default and it is immutable
// afterwards) and pages through them one at a time: with completed_at
// tied, `ORDER BY completed_at DESC, id DESC` must fall back to id, so the
// higher id is always page 1 and the lower id is always page 2 — never
// both on the same page, never the same one twice, and never dropped.
func TestListSales_cursorPaginationBreaksTiesByIdDescending(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "list-sale-tie-break")
	owner := seedUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	ownerCtx := ctxAs(shop.ID, owner)

	tied := mustNow(t, q, shop.ID)
	idOne := seedSaleRow(ctx, t, pool, shop.ID, loc.ID, cashier.ID, 1, tied)
	idTwo := seedSaleRow(ctx, t, pool, shop.ID, loc.ID, cashier.ID, 2, tied)
	idHigh, idLow := idOne, idTwo
	if idLow.String() > idHigh.String() {
		idHigh, idLow = idLow, idHigh
	}

	l := 1
	resp, err := h.ListSales(ownerCtx, gen.ListSalesRequestObject{Params: gen.ListSalesParams{Limit: &l}})
	if err != nil {
		t.Fatalf("ListSales (page 1): %v", err)
	}
	page1, ok := resp.(gen.ListSales200JSONResponse)
	if !ok {
		t.Fatalf("ListSales response type = %T", resp)
	}
	if len(page1.Items) != 1 || page1.Items[0].Id != idHigh {
		t.Fatalf("page 1 = %+v, want exactly the higher id %s (tied completed_at breaks by id DESC)", page1.Items, idHigh)
	}
	if !page1.NextCursor.IsSpecified() || page1.NextCursor.IsNull() {
		t.Fatalf("page 1 NextCursor = %+v, want set (a second tied row remains)", page1.NextCursor)
	}
	cursor := page1.NextCursor.MustGet()

	resp, err = h.ListSales(ownerCtx, gen.ListSalesRequestObject{Params: gen.ListSalesParams{Limit: &l, Cursor: &cursor}})
	if err != nil {
		t.Fatalf("ListSales (page 2): %v", err)
	}
	page2, ok := resp.(gen.ListSales200JSONResponse)
	if !ok {
		t.Fatalf("ListSales response type = %T", resp)
	}
	if len(page2.Items) != 1 || page2.Items[0].Id != idLow {
		t.Fatalf("page 2 = %+v, want exactly the lower id %s, not a repeat of page 1", page2.Items, idLow)
	}
	if !page2.NextCursor.IsSpecified() || !page2.NextCursor.IsNull() {
		t.Fatalf("page 2 NextCursor = %+v, want null (no rows left)", page2.NextCursor)
	}
}

func TestListSales_malformedCursorIs400(t *testing.T) {
	_, q := newTestQueries(t)
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(context.Background(), t, q, "list-sale-bad-cursor")
	owner := seedUser(context.Background(), t, q, shop.ID, "owner1", db.UserRoleOwner)
	bogus := "not-a-valid-cursor"

	_, err := h.ListSales(ctxAs(shop.ID, owner), gen.ListSalesRequestObject{Params: gen.ListSalesParams{Cursor: &bogus}})
	if err == nil {
		t.Fatal("want 400 VALIDATION_FAILED for a malformed cursor, got no error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 400 {
		t.Fatalf("error = %v, want 400 VALIDATION_FAILED", err)
	}
	if got := apiErr.Details["fields"].(map[string]string)["cursor"]; got != "invalid" {
		t.Fatalf("details.fields.cursor = %q, want invalid", got)
	}
}
