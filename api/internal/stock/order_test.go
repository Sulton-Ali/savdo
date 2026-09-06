package stock_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/stock"
)

// setVariantCreatedAt back-dates a seeded variant's created_at directly —
// a test-only technique (CreateVariant has no created_at parameter,
// since nothing else ever needs to set it) to get three variants with
// distinct, controlled created_at values for D-92's order assertions.
func setVariantCreatedAt(ctx context.Context, t *testing.T, pool *pgxpool.Pool, variantID uuid.UUID, at time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `UPDATE product_variants SET created_at = $1 WHERE id = $2`, at, variantID); err != nil {
		t.Fatalf("setVariantCreatedAt: %v", err)
	}
}

// walkLevels drives GET /stock/levels through the handler with the given
// pageSize, following NextCursor until it is exhausted, and returns every
// item in the order the pages produced them. Unlike a single big-limit
// call, this exercises decodeLevelCursor/levelCursorPtr's actual codec
// (all three OR branches of the cursor predicate can fire across a
// multi-page walk), not just the SQL underneath.
func walkLevels(ctx context.Context, t *testing.T, h *stock.Handler, params gen.ListStockLevelsParams, pageSize int) []gen.StockLevel {
	t.Helper()
	p := params
	p.Limit = &pageSize
	p.Cursor = nil
	var all []gen.StockLevel
	for {
		resp, err := h.ListStockLevels(ctx, gen.ListStockLevelsRequestObject{Params: p})
		if err != nil {
			t.Fatalf("ListStockLevels: %v", err)
		}
		page, ok := resp.(gen.ListStockLevels200JSONResponse)
		if !ok {
			t.Fatalf("ListStockLevels response type = %T", resp)
		}
		all = append(all, page.Items...)
		if !page.NextCursor.IsSpecified() || page.NextCursor.IsNull() {
			return all
		}
		cursor := page.NextCursor.MustGet()
		p.Cursor = &cursor
	}
}

// walkLow is walkLevels for GET /stock/low.
func walkLow(ctx context.Context, t *testing.T, h *stock.Handler, pageSize int) []gen.StockLowItem {
	t.Helper()
	limit := pageSize
	var cursor *string
	var all []gen.StockLowItem
	for {
		resp, err := h.ListLowStock(ctx, gen.ListLowStockRequestObject{Params: gen.ListLowStockParams{Limit: &limit, Cursor: cursor}})
		if err != nil {
			t.Fatalf("ListLowStock: %v", err)
		}
		page, ok := resp.(gen.ListLowStock200JSONResponse)
		if !ok {
			t.Fatalf("ListLowStock response type = %T", resp)
		}
		all = append(all, page.Items...)
		if !page.NextCursor.IsSpecified() || page.NextCursor.IsNull() {
			return all
		}
		c := page.NextCursor.MustGet()
		cursor = &c
	}
}

// levelKey identifies a stock_levels row by its (variant, location) pair
// — stock_levels' own primary key columns — so two item slices can be
// compared for "same rows, same order" without caring about qty
// formatting.
type levelKey struct {
	variant  uuid.UUID
	location uuid.UUID
}

func levelKeys(items []gen.StockLevel) []levelKey {
	keys := make([]levelKey, len(items))
	for i, it := range items {
		keys[i] = levelKey{variant: it.VariantId, location: it.LocationId}
	}
	return keys
}

// assertNoDuplicateLevelKeys fails the test if any (variant, location)
// pair appears more than once — the "no duplicate" half of the review's
// tie-break check.
func assertNoDuplicateLevelKeys(t *testing.T, keys []levelKey) {
	t.Helper()
	seen := map[levelKey]bool{}
	for _, k := range keys {
		if seen[k] {
			t.Fatalf("duplicate row across pages: variant=%s location=%s", k.variant, k.location)
		}
		seen[k] = true
	}
}

// TestListStockLevels_ordersNewestVariantFirstThenLocation is D-92: newest
// variant first, then location. Three variants get distinct created_at
// (oldest to newest v1, v2, v3), each stocked at one location; the list
// must come back v3, v2, v1. Page 2 (limit 2) must continue from the
// cursor without repeating or skipping a variant.
func TestListStockLevels_ordersNewestVariantFirstThenLocation(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()

	shopRow := seedShop(ctx, t, q, "levels-order-shop")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")
	product := seedProduct(ctx, t, q, shopRow.ID, unit.ID, "order-product")
	location := seedLocation(ctx, t, q, shopRow.ID, "Main")

	v1 := seedVariant(ctx, t, q, shopRow.ID, product.ID, `{"n":1}`)
	v2 := seedVariant(ctx, t, q, shopRow.ID, product.ID, `{"n":2}`)
	v3 := seedVariant(ctx, t, q, shopRow.ID, product.ID, `{"n":3}`)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	setVariantCreatedAt(ctx, t, pool, v1.ID, base)
	setVariantCreatedAt(ctx, t, pool, v2.ID, base.Add(time.Hour))
	setVariantCreatedAt(ctx, t, pool, v3.ID, base.Add(2*time.Hour))

	stockSvc := stock.NewService(pool, q)
	stockHandler := stock.NewHandler(stockSvc)
	for _, v := range []uuid.UUID{v1.ID, v2.ID, v3.ID} {
		if _, err := stockSvc.MoveInTx(ctx, stock.MoveParams{
			ShopID: shopRow.ID, VariantID: v, LocationID: location.ID,
			Kind: db.StockMovementKindPurchaseIn, Qty: d(t, "5.000"),
		}); err != nil {
			t.Fatalf("seed opening stock for %s: %v", v, err)
		}
	}

	authCtx := ctxAs(shopRow.ID, seedUser(ctx, t, q, shopRow.ID, "levels-order-user", db.UserRoleOwner))

	limit := 2
	first, err := stockHandler.ListStockLevels(authCtx, gen.ListStockLevelsRequestObject{Params: gen.ListStockLevelsParams{Limit: &limit}})
	if err != nil {
		t.Fatalf("ListStockLevels (page 1): %v", err)
	}
	page1, ok := first.(gen.ListStockLevels200JSONResponse)
	if !ok {
		t.Fatalf("ListStockLevels (page 1) response type = %T", first)
	}
	if len(page1.Items) != 2 {
		t.Fatalf("page 1 items = %d, want 2", len(page1.Items))
	}
	if page1.Items[0].VariantId != v3.ID || page1.Items[1].VariantId != v2.ID {
		t.Fatalf("page 1 order = [%s, %s], want [v3, v2] (newest first)", page1.Items[0].VariantId, page1.Items[1].VariantId)
	}
	if !page1.NextCursor.IsSpecified() || page1.NextCursor.IsNull() {
		t.Fatalf("page 1 NextCursor = %+v, want a cursor", page1.NextCursor)
	}
	cursor := page1.NextCursor.MustGet()

	second, err := stockHandler.ListStockLevels(authCtx, gen.ListStockLevelsRequestObject{Params: gen.ListStockLevelsParams{Limit: &limit, Cursor: &cursor}})
	if err != nil {
		t.Fatalf("ListStockLevels (page 2): %v", err)
	}
	page2, ok := second.(gen.ListStockLevels200JSONResponse)
	if !ok {
		t.Fatalf("ListStockLevels (page 2) response type = %T", second)
	}
	if len(page2.Items) != 1 {
		t.Fatalf("page 2 items = %d, want 1", len(page2.Items))
	}
	if page2.Items[0].VariantId != v1.ID {
		t.Fatalf("page 2 item = %s, want v1 (oldest)", page2.Items[0].VariantId)
	}
	if page2.NextCursor.IsSpecified() && !page2.NextCursor.IsNull() {
		t.Fatalf("page 2 NextCursor = %+v, want none (last page)", page2.NextCursor)
	}
}

// TestListStockLevels_ordersLocationWithinVariant checks the tie-break:
// within one variant, locations sort ascending by id (D-92's "then
// location").
func TestListStockLevels_ordersLocationWithinVariant(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()

	shopRow := seedShop(ctx, t, q, "levels-location-order-shop")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")
	product := seedProduct(ctx, t, q, shopRow.ID, unit.ID, "location-order-product")
	variant := seedVariant(ctx, t, q, shopRow.ID, product.ID, "{}")
	locA := seedLocation(ctx, t, q, shopRow.ID, "A")
	locB := seedLocation(ctx, t, q, shopRow.ID, "B")

	stockSvc := stock.NewService(pool, q)
	stockHandler := stock.NewHandler(stockSvc)
	for _, loc := range []uuid.UUID{locA.ID, locB.ID} {
		if _, err := stockSvc.MoveInTx(ctx, stock.MoveParams{
			ShopID: shopRow.ID, VariantID: variant.ID, LocationID: loc,
			Kind: db.StockMovementKindPurchaseIn, Qty: d(t, "1.000"),
		}); err != nil {
			t.Fatalf("seed opening stock at %s: %v", loc, err)
		}
	}

	authCtx := ctxAs(shopRow.ID, seedUser(ctx, t, q, shopRow.ID, "levels-location-order-user", db.UserRoleOwner))

	resp, err := stockHandler.ListStockLevels(authCtx, gen.ListStockLevelsRequestObject{})
	if err != nil {
		t.Fatalf("ListStockLevels: %v", err)
	}
	page, ok := resp.(gen.ListStockLevels200JSONResponse)
	if !ok {
		t.Fatalf("ListStockLevels response type = %T", resp)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(page.Items))
	}
	// Compare the raw 16-byte UUIDs, the same comparison Postgres's
	// location_id > $cursor uses — not their string form, which would
	// happen to agree for hex UUIDs but documents the wrong operation.
	a, b := page.Items[0].LocationId, page.Items[1].LocationId
	if bytes.Compare(a[:], b[:]) >= 0 {
		t.Fatalf("locations not ascending: %s then %s", a, b)
	}
}

// TestListStockLevels_sameCreatedAtPaginatesAllRowsOnceEach is the review
// fix for D-92: two variants share one created_at (a tie the SQL's
// pv.created_at = cursor branches must handle), each stocked at two
// locations. Walking with limit=1 forces the cursor through both
// tie-break branches — pv.id < cursor_variant_id (moving to the next
// variant at the same created_at) and location_id > cursor_location_id
// (moving to the next location within the same variant) — and must land
// on the same 4 rows, in the same order, as a single unpaginated call.
func TestListStockLevels_sameCreatedAtPaginatesAllRowsOnceEach(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()

	shopRow := seedShop(ctx, t, q, "levels-tie-shop")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")
	product := seedProduct(ctx, t, q, shopRow.ID, unit.ID, "tie-product")
	locA := seedLocation(ctx, t, q, shopRow.ID, "A")
	locB := seedLocation(ctx, t, q, shopRow.ID, "B")

	v1 := seedVariant(ctx, t, q, shopRow.ID, product.ID, `{"n":1}`)
	v2 := seedVariant(ctx, t, q, shopRow.ID, product.ID, `{"n":2}`)

	tied := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	setVariantCreatedAt(ctx, t, pool, v1.ID, tied)
	setVariantCreatedAt(ctx, t, pool, v2.ID, tied)

	stockSvc := stock.NewService(pool, q)
	stockHandler := stock.NewHandler(stockSvc)
	for _, v := range []uuid.UUID{v1.ID, v2.ID} {
		for _, loc := range []uuid.UUID{locA.ID, locB.ID} {
			if _, err := stockSvc.MoveInTx(ctx, stock.MoveParams{
				ShopID: shopRow.ID, VariantID: v, LocationID: loc,
				Kind: db.StockMovementKindPurchaseIn, Qty: d(t, "1.000"),
			}); err != nil {
				t.Fatalf("seed opening stock for %s at %s: %v", v, loc, err)
			}
		}
	}

	authCtx := ctxAs(shopRow.ID, seedUser(ctx, t, q, shopRow.ID, "levels-tie-user", db.UserRoleOwner))

	single, err := stockHandler.ListStockLevels(authCtx, gen.ListStockLevelsRequestObject{})
	if err != nil {
		t.Fatalf("ListStockLevels (single page): %v", err)
	}
	singlePage, ok := single.(gen.ListStockLevels200JSONResponse)
	if !ok {
		t.Fatalf("ListStockLevels (single page) response type = %T", single)
	}
	if len(singlePage.Items) != 4 {
		t.Fatalf("single-page items = %d, want 4", len(singlePage.Items))
	}

	walked := walkLevels(authCtx, t, stockHandler, gen.ListStockLevelsParams{}, 1)
	if len(walked) != 4 {
		t.Fatalf("walked items = %d, want 4", len(walked))
	}
	assertNoDuplicateLevelKeys(t, levelKeys(walked))

	wantKeys, gotKeys := levelKeys(singlePage.Items), levelKeys(walked)
	for i := range wantKeys {
		if wantKeys[i] != gotKeys[i] {
			t.Fatalf("walked order[%d] = %+v, want %+v (single-page order)", i, gotKeys[i], wantKeys[i])
		}
	}
}

// TestListStockLevels_variantFilterWalksLocationsAscending is the
// review's (b): one variant at three locations, `?variantId=` set,
// limit=1 forces three pages, each must land strictly ascending by
// location_id (D-92's "then location").
func TestListStockLevels_variantFilterWalksLocationsAscending(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()

	shopRow := seedShop(ctx, t, q, "levels-filter-walk-shop")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")
	product := seedProduct(ctx, t, q, shopRow.ID, unit.ID, "filter-walk-product")
	variant := seedVariant(ctx, t, q, shopRow.ID, product.ID, "{}")
	locA := seedLocation(ctx, t, q, shopRow.ID, "A")
	locB := seedLocation(ctx, t, q, shopRow.ID, "B")
	locC := seedLocation(ctx, t, q, shopRow.ID, "C")

	stockSvc := stock.NewService(pool, q)
	stockHandler := stock.NewHandler(stockSvc)
	for _, loc := range []uuid.UUID{locA.ID, locB.ID, locC.ID} {
		if _, err := stockSvc.MoveInTx(ctx, stock.MoveParams{
			ShopID: shopRow.ID, VariantID: variant.ID, LocationID: loc,
			Kind: db.StockMovementKindPurchaseIn, Qty: d(t, "1.000"),
		}); err != nil {
			t.Fatalf("seed opening stock at %s: %v", loc, err)
		}
	}

	authCtx := ctxAs(shopRow.ID, seedUser(ctx, t, q, shopRow.ID, "levels-filter-walk-user", db.UserRoleOwner))

	variantID := variant.ID
	walked := walkLevels(authCtx, t, stockHandler, gen.ListStockLevelsParams{VariantId: &variantID}, 1)
	if len(walked) != 3 {
		t.Fatalf("walked items = %d, want 3", len(walked))
	}
	assertNoDuplicateLevelKeys(t, levelKeys(walked))
	for i := 1; i < len(walked); i++ {
		prev, cur := walked[i-1].LocationId, walked[i].LocationId
		if bytes.Compare(prev[:], cur[:]) >= 0 {
			t.Fatalf("page %d location %s not strictly after page %d location %s", i, cur, i-1, prev)
		}
	}
}

// TestListLowStock_ordersNewestVariantFirst is D-92 for GET /stock/low:
// three variants, all left below the shop's default threshold, come back
// newest created_at first, and pagination continues correctly.
func TestListLowStock_ordersNewestVariantFirst(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()

	shopRow := seedShop(ctx, t, q, "low-order-shop")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")
	product := seedProduct(ctx, t, q, shopRow.ID, unit.ID, "low-order-product")
	location := seedLocation(ctx, t, q, shopRow.ID, "Main")

	v1 := seedVariant(ctx, t, q, shopRow.ID, product.ID, `{"n":1}`)
	v2 := seedVariant(ctx, t, q, shopRow.ID, product.ID, `{"n":2}`)
	v3 := seedVariant(ctx, t, q, shopRow.ID, product.ID, `{"n":3}`)

	base := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	setVariantCreatedAt(ctx, t, pool, v1.ID, base)
	setVariantCreatedAt(ctx, t, pool, v2.ID, base.Add(time.Hour))
	setVariantCreatedAt(ctx, t, pool, v3.ID, base.Add(2*time.Hour))

	stockSvc := stock.NewService(pool, q)
	stockHandler := stock.NewHandler(stockSvc)
	// Shop default low_stock_threshold is 2 (D-50); leave every variant
	// at qty 1 so all three are low.
	for _, v := range []uuid.UUID{v1.ID, v2.ID, v3.ID} {
		if _, err := stockSvc.MoveInTx(ctx, stock.MoveParams{
			ShopID: shopRow.ID, VariantID: v, LocationID: location.ID,
			Kind: db.StockMovementKindPurchaseIn, Qty: d(t, "1.000"),
		}); err != nil {
			t.Fatalf("seed opening stock for %s: %v", v, err)
		}
	}

	authCtx := ctxAs(shopRow.ID, seedUser(ctx, t, q, shopRow.ID, "low-order-user", db.UserRoleManager))

	limit := 2
	first, err := stockHandler.ListLowStock(authCtx, gen.ListLowStockRequestObject{Params: gen.ListLowStockParams{Limit: &limit}})
	if err != nil {
		t.Fatalf("ListLowStock (page 1): %v", err)
	}
	page1, ok := first.(gen.ListLowStock200JSONResponse)
	if !ok {
		t.Fatalf("ListLowStock (page 1) response type = %T", first)
	}
	if len(page1.Items) != 2 {
		t.Fatalf("page 1 items = %d, want 2", len(page1.Items))
	}
	if page1.Items[0].VariantId != v3.ID || page1.Items[1].VariantId != v2.ID {
		t.Fatalf("page 1 order = [%s, %s], want [v3, v2] (newest first)", page1.Items[0].VariantId, page1.Items[1].VariantId)
	}
	if !page1.NextCursor.IsSpecified() || page1.NextCursor.IsNull() {
		t.Fatalf("page 1 NextCursor = %+v, want a cursor", page1.NextCursor)
	}
	cursor := page1.NextCursor.MustGet()

	second, err := stockHandler.ListLowStock(authCtx, gen.ListLowStockRequestObject{Params: gen.ListLowStockParams{Limit: &limit, Cursor: &cursor}})
	if err != nil {
		t.Fatalf("ListLowStock (page 2): %v", err)
	}
	page2, ok := second.(gen.ListLowStock200JSONResponse)
	if !ok {
		t.Fatalf("ListLowStock (page 2) response type = %T", second)
	}
	if len(page2.Items) != 1 {
		t.Fatalf("page 2 items = %d, want 1", len(page2.Items))
	}
	if page2.Items[0].VariantId != v1.ID {
		t.Fatalf("page 2 item = %s, want v1 (oldest)", page2.Items[0].VariantId)
	}
	if page2.NextCursor.IsSpecified() && !page2.NextCursor.IsNull() {
		t.Fatalf("page 2 NextCursor = %+v, want none (last page)", page2.NextCursor)
	}
}

// TestListLowStock_sameCreatedAtPaginatesAllRowsOnceEach is the review's
// ListLow tie-break check: three variants share one created_at, all low.
// Walking with limit=1 forces the cursor's variant_created_at = cursor
// tie-break (variant_id DESC) to fire on every step, and must land on the
// same 3 rows, in the same order, as a single unpaginated call.
func TestListLowStock_sameCreatedAtPaginatesAllRowsOnceEach(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()

	shopRow := seedShop(ctx, t, q, "low-tie-shop")
	unit := seedUnit(ctx, t, q, shopRow.ID, "pcs")
	product := seedProduct(ctx, t, q, shopRow.ID, unit.ID, "low-tie-product")
	location := seedLocation(ctx, t, q, shopRow.ID, "Main")

	v1 := seedVariant(ctx, t, q, shopRow.ID, product.ID, `{"n":1}`)
	v2 := seedVariant(ctx, t, q, shopRow.ID, product.ID, `{"n":2}`)
	v3 := seedVariant(ctx, t, q, shopRow.ID, product.ID, `{"n":3}`)

	tied := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	for _, v := range []uuid.UUID{v1.ID, v2.ID, v3.ID} {
		setVariantCreatedAt(ctx, t, pool, v, tied)
	}

	stockSvc := stock.NewService(pool, q)
	stockHandler := stock.NewHandler(stockSvc)
	// Shop default low_stock_threshold is 2 (D-50); qty 1 keeps every
	// variant low.
	for _, v := range []uuid.UUID{v1.ID, v2.ID, v3.ID} {
		if _, err := stockSvc.MoveInTx(ctx, stock.MoveParams{
			ShopID: shopRow.ID, VariantID: v, LocationID: location.ID,
			Kind: db.StockMovementKindPurchaseIn, Qty: d(t, "1.000"),
		}); err != nil {
			t.Fatalf("seed opening stock for %s: %v", v, err)
		}
	}

	authCtx := ctxAs(shopRow.ID, seedUser(ctx, t, q, shopRow.ID, "low-tie-user", db.UserRoleManager))

	single, err := stockHandler.ListLowStock(authCtx, gen.ListLowStockRequestObject{})
	if err != nil {
		t.Fatalf("ListLowStock (single page): %v", err)
	}
	singlePage, ok := single.(gen.ListLowStock200JSONResponse)
	if !ok {
		t.Fatalf("ListLowStock (single page) response type = %T", single)
	}
	if len(singlePage.Items) != 3 {
		t.Fatalf("single-page items = %d, want 3", len(singlePage.Items))
	}

	walked := walkLow(authCtx, t, stockHandler, 1)
	if len(walked) != 3 {
		t.Fatalf("walked items = %d, want 3", len(walked))
	}

	seen := map[uuid.UUID]bool{}
	for _, it := range walked {
		if seen[it.VariantId] {
			t.Fatalf("duplicate variant across pages: %s", it.VariantId)
		}
		seen[it.VariantId] = true
	}
	for i := range singlePage.Items {
		if singlePage.Items[i].VariantId != walked[i].VariantId {
			t.Fatalf("walked order[%d] = %s, want %s (single-page order)", i, walked[i].VariantId, singlePage.Items[i].VariantId)
		}
	}
}
