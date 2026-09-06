package sales_test

// Tests for the Phase 5 draft-sales endpoints (D-87..D-89): create/list/
// get/patch/delete/complete on api/internal/sales/drafts*.go. Fixtures
// (seedShop, seedUser, createSale, etc.) are shared with the rest of this
// package (sales_test.go); createDraft/updateDraft/deleteDraft/
// completeDraft below mirror createSale's own "run on a transaction this
// test opens and commits/rolls back" shape — the same shape
// httpx/sales.go gives each _Tx method in production. Idempotency-Key
// replay behaviour for POST /sales/drafts/{id}/complete is tested at the
// httpx level instead (internal/httpx/sales_test.go), the same split
// CreateSale's own idempotency tests already use — this file exercises
// the business rules (ownership, pricing, discount cap, cross-shop 404,
// cost hiding), not the replay wrapper.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oapi-codegen/nullable"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/sales"
)

// draftBody builds a one-line SaleDraftCreate — the common case most
// tests in this file need, with room to override discount/customer/note
// per call site (mirrors saleBody, sales_test.go).
func draftBody(locationID, variantID uuid.UUID, qty string) *gen.SaleDraftCreate {
	return &gen.SaleDraftCreate{
		LocationId: locationID,
		Items:      []gen.SaleItemCreate{{VariantId: variantID, Qty: qty}},
	}
}

// createDraft runs CreateSaleDraftTx on its own transaction, committing
// on success and rolling back on error — the shape httpx/sales.go's
// CreateSaleDraft gives it in production (mirrors createSale,
// sales_test.go).
func createDraft(ctx context.Context, t *testing.T, h *sales.Handler, pool *pgxpool.Pool, q *db.Queries, body *gen.SaleDraftCreate) (gen.SaleDraft, error) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("createDraft: begin: %v", err)
	}
	qtx := q.WithTx(tx)
	resp, err := h.CreateSaleDraftTx(ctx, qtx, body)
	if err != nil {
		_ = tx.Rollback(ctx)
		return gen.SaleDraft{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("createDraft: commit: %v", err)
	}
	return resp, nil
}

// updateDraft is createDraft for UpdateSaleDraftTx.
func updateDraft(ctx context.Context, t *testing.T, h *sales.Handler, pool *pgxpool.Pool, q *db.Queries, id uuid.UUID, body *gen.SaleDraftPatch) (gen.SaleDraft, error) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("updateDraft: begin: %v", err)
	}
	qtx := q.WithTx(tx)
	resp, err := h.UpdateSaleDraftTx(ctx, qtx, id, body)
	if err != nil {
		_ = tx.Rollback(ctx)
		return gen.SaleDraft{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("updateDraft: commit: %v", err)
	}
	return resp, nil
}

// deleteDraft is createDraft for DeleteSaleDraftTx.
func deleteDraft(ctx context.Context, t *testing.T, h *sales.Handler, pool *pgxpool.Pool, q *db.Queries, id uuid.UUID) error {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("deleteDraft: begin: %v", err)
	}
	qtx := q.WithTx(tx)
	if err := h.DeleteSaleDraftTx(ctx, qtx, id); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("deleteDraft: commit: %v", err)
	}
	return nil
}

// completeDraft is createDraft for CompleteSaleDraftTx.
func completeDraft(ctx context.Context, t *testing.T, h *sales.Handler, pool *pgxpool.Pool, q *db.Queries, id uuid.UUID, body *gen.SaleDraftComplete) (gen.Sale, error) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("completeDraft: begin: %v", err)
	}
	qtx := q.WithTx(tx)
	resp, err := h.CompleteSaleDraftTx(ctx, qtx, id, body)
	if err != nil {
		_ = tx.Rollback(ctx)
		return gen.Sale{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("completeDraft: commit: %v", err)
	}
	return resp, nil
}

func TestCreateSaleDraft_savesLinesWithNoStockMovement(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "draft-create")
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "draft-create-product", "100.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	cashierCtx := ctxAs(shop.ID, cashier)
	// Deliberately no stock at all — a draft never checks or moves it
	// (D-88): CreateSaleDraftTx must succeed anyway.

	draft, err := createDraft(cashierCtx, t, h, pool, q, draftBody(loc.ID, variant.ID, "3.000"))
	if err != nil {
		t.Fatalf("createDraft: %v", err)
	}
	if draft.Subtotal != "300.00" || draft.EstimatedTotal != "300.00" || draft.DiscountAmount != "0.00" {
		t.Fatalf("Subtotal/EstimatedTotal/DiscountAmount = %s/%s/%s, want 300.00/300.00/0.00",
			draft.Subtotal, draft.EstimatedTotal, draft.DiscountAmount)
	}
	if len(draft.Items) != 1 || draft.Items[0].UnitPrice != "100.00" || draft.Items[0].LineTotal != "300.00" || draft.Items[0].Qty != "3.000" {
		t.Fatalf("Items = %+v, want one line qty=3.000 unitPrice=100.00 lineTotal=300.00", draft.Items)
	}
	if !draft.CreatedBy.IsSpecified() || draft.CreatedBy.IsNull() || draft.CreatedBy.MustGet() != cashier.ID {
		t.Fatalf("CreatedBy = %+v, want %s", draft.CreatedBy, cashier.ID)
	}
	if len(draft.Items) != 1 || !draft.Items[0].Available {
		t.Fatalf("Items[0].Available = %+v, want true", draft.Items)
	}

	if n := countMovements(ctx, t, pool, shop.ID, variant.ID, loc.ID, ""); n != 0 {
		t.Fatalf("stock_movements = %d, want 0 (a draft never moves stock, D-88)", n)
	}
}

func TestListSaleDrafts_newestFirstWithCursorAndCreatedByFilter(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "draft-list")
	cashier1 := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	cashier2 := seedUser(ctx, t, q, shop.ID, "cashier2", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "draft-list-product", "10.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	cashier1Ctx := ctxAs(shop.ID, cashier1)
	cashier2Ctx := ctxAs(shop.ID, cashier2)

	d1, err := createDraft(cashier1Ctx, t, h, pool, q, draftBody(loc.ID, variant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createDraft d1: %v", err)
	}
	d2, err := createDraft(cashier2Ctx, t, h, pool, q, draftBody(loc.ID, variant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createDraft d2: %v", err)
	}
	d3, err := createDraft(cashier1Ctx, t, h, pool, q, draftBody(loc.ID, variant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createDraft d3: %v", err)
	}

	resp, err := h.ListSaleDrafts(cashier1Ctx, gen.ListSaleDraftsRequestObject{})
	if err != nil {
		t.Fatalf("ListSaleDrafts: %v", err)
	}
	list, ok := resp.(gen.ListSaleDrafts200JSONResponse)
	if !ok {
		t.Fatalf("ListSaleDrafts response type = %T", resp)
	}
	if len(list.Items) != 3 || list.Items[0].Id != d3.Id || list.Items[1].Id != d2.Id || list.Items[2].Id != d1.Id {
		t.Fatalf("Items order = %+v, want [d3, d2, d1] (newest first)", list.Items)
	}

	// Cursor pagination: limit 1 returns just d3 plus a cursor; the next
	// page (from that cursor) returns d2.
	limit := 1
	page1, err := h.ListSaleDrafts(cashier1Ctx, gen.ListSaleDraftsRequestObject{Params: gen.ListSaleDraftsParams{Limit: &limit}})
	if err != nil {
		t.Fatalf("ListSaleDrafts (page1): %v", err)
	}
	list1, ok := page1.(gen.ListSaleDrafts200JSONResponse)
	if !ok || len(list1.Items) != 1 || list1.Items[0].Id != d3.Id || !list1.NextCursor.IsSpecified() || list1.NextCursor.IsNull() {
		t.Fatalf("page1 = %+v, want [d3] with a next cursor", list1)
	}
	cursor := list1.NextCursor.MustGet()
	page2, err := h.ListSaleDrafts(cashier1Ctx, gen.ListSaleDraftsRequestObject{Params: gen.ListSaleDraftsParams{Limit: &limit, Cursor: &cursor}})
	if err != nil {
		t.Fatalf("ListSaleDrafts (page2): %v", err)
	}
	list2, ok := page2.(gen.ListSaleDrafts200JSONResponse)
	if !ok || len(list2.Items) != 1 || list2.Items[0].Id != d2.Id {
		t.Fatalf("page2 = %+v, want [d2]", list2)
	}

	// createdBy narrows to one creator's drafts (d3, d1 — newest first).
	filtered, err := h.ListSaleDrafts(cashier1Ctx, gen.ListSaleDraftsRequestObject{Params: gen.ListSaleDraftsParams{CreatedBy: &cashier1.ID}})
	if err != nil {
		t.Fatalf("ListSaleDrafts (createdBy): %v", err)
	}
	listFiltered, ok := filtered.(gen.ListSaleDrafts200JSONResponse)
	if !ok || len(listFiltered.Items) != 2 || listFiltered.Items[0].Id != d3.Id || listFiltered.Items[1].Id != d1.Id {
		t.Fatalf("createdBy-filtered Items = %+v, want [d3, d1]", listFiltered.Items)
	}
}

func TestGetSaleDraft_promoPricePrecedenceAndDiscountCap(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "draft-get-promo")
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	cashierCtx := ctxAs(shop.ID, cashier)

	shopRow, err := q.GetShop(ctx, shop.ID)
	if err != nil {
		t.Fatalf("GetShop: %v", err)
	}
	tz, err := time.LoadLocation(shopRow.Timezone)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", shopRow.Timezone, err)
	}
	now := time.Now().In(tz)
	todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, tz)

	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "draft-promo-product", "100.00", productOpts{
		promoPrice: strPtr("60.00"), promoFrom: &todayMidnight, promoTo: &todayMidnight,
	})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)

	draft, err := createDraft(cashierCtx, t, h, pool, q, &gen.SaleDraftCreate{
		LocationId: loc.ID,
		Items:      []gen.SaleItemCreate{{VariantId: variant.ID, Qty: "2.000"}},
		Discount:   &gen.SaleDiscount{Type: gen.Fixed, Value: "100.00"},
	})
	if err != nil {
		t.Fatalf("createDraft: %v", err)
	}
	// 2 * 60.00 (promo active today, D-67) = 120.00 subtotal; a 100.00
	// fixed discount is within it.
	if draft.Items[0].UnitPrice != "60.00" || draft.Subtotal != "120.00" {
		t.Fatalf("UnitPrice/Subtotal = %s/%s, want 60.00/120.00 (promo active today)", draft.Items[0].UnitPrice, draft.Subtotal)
	}
	if draft.DiscountAmount != "100.00" || draft.EstimatedTotal != "20.00" {
		t.Fatalf("DiscountAmount/EstimatedTotal = %s/%s, want 100.00/20.00", draft.DiscountAmount, draft.EstimatedTotal)
	}

	// Simulate the catalogue changing after the draft was created — the
	// promo ends and the base price drops — so the recomputed subtotal
	// (2 * 30.00 = 60.00) now falls below the already-stored 100.00
	// discount. A plain GET must cap discountAmount at the new subtotal,
	// never fail or go negative.
	if _, err := pool.Exec(ctx,
		`UPDATE products SET promo_price = NULL, promo_from = NULL, promo_to = NULL, base_price = '30.00' WHERE id = $1`,
		product.ID); err != nil {
		t.Fatalf("update product: %v", err)
	}

	resp, err := h.GetSaleDraft(cashierCtx, gen.GetSaleDraftRequestObject{Id: draft.Id})
	if err != nil {
		t.Fatalf("GetSaleDraft: %v", err)
	}
	got, ok := resp.(gen.GetSaleDraft200JSONResponse)
	if !ok {
		t.Fatalf("GetSaleDraft response type = %T", resp)
	}
	if got.Items[0].UnitPrice != "30.00" || got.Subtotal != "60.00" {
		t.Fatalf("UnitPrice/Subtotal = %s/%s, want 30.00/60.00 (repriced at read time)", got.Items[0].UnitPrice, got.Subtotal)
	}
	if got.DiscountAmount != "60.00" {
		t.Fatalf("DiscountAmount = %q, want 60.00 (capped at the new subtotal, not the stored 100.00)", got.DiscountAmount)
	}
	if got.EstimatedTotal != "0.00" {
		t.Fatalf("EstimatedTotal = %q, want 0.00", got.EstimatedTotal)
	}
}

func TestUpdateSaleDraft_replacesItemsAndOwnershipRules(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "draft-update")
	cashier1 := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	cashier2 := seedUser(ctx, t, q, shop.ID, "cashier2", db.UserRoleCashier)
	manager := seedUser(ctx, t, q, shop.ID, "manager1", db.UserRoleManager)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	productA := seedProduct(ctx, t, q, shop.ID, unit.ID, "draft-update-a", "10.00", productOpts{})
	variantA := seedVariant(ctx, t, q, shop.ID, productA.ID)
	productB := seedProduct(ctx, t, q, shop.ID, unit.ID, "draft-update-b", "20.00", productOpts{})
	variantB := seedVariant(ctx, t, q, shop.ID, productB.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	cashier1Ctx := ctxAs(shop.ID, cashier1)
	cashier2Ctx := ctxAs(shop.ID, cashier2)
	managerCtx := ctxAs(shop.ID, manager)

	draft, err := createDraft(cashier1Ctx, t, h, pool, q, draftBody(loc.ID, variantA.ID, "1.000"))
	if err != nil {
		t.Fatalf("createDraft: %v", err)
	}

	// Another cashier may not edit it (D-89): 403, not 404 — the draft
	// exists, this cashier just isn't its creator or manager+.
	_, err = updateDraft(cashier2Ctx, t, h, pool, q, draft.Id, &gen.SaleDraftPatch{Note: nullable.NewNullableWithValue("nope")})
	assertAPIErr(t, "another cashier edits", err, 403, gen.FORBIDDEN)

	// The owning cashier may replace the whole line set.
	newItems := []gen.SaleItemCreate{{VariantId: variantB.ID, Qty: "2.000"}}
	updated, err := updateDraft(cashier1Ctx, t, h, pool, q, draft.Id, &gen.SaleDraftPatch{Items: &newItems})
	if err != nil {
		t.Fatalf("updateDraft (own): %v", err)
	}
	if len(updated.Items) != 1 || updated.Items[0].VariantId != variantB.ID || updated.Subtotal != "40.00" {
		t.Fatalf("Items/Subtotal after replace = %+v/%s, want [variantB] / 40.00", updated.Items, updated.Subtotal)
	}

	// A manager may edit any cashier's draft.
	note := "manager edit"
	updatedByManager, err := updateDraft(managerCtx, t, h, pool, q, draft.Id, &gen.SaleDraftPatch{Note: nullable.NewNullableWithValue(note)})
	if err != nil {
		t.Fatalf("updateDraft (manager): %v", err)
	}
	if !updatedByManager.Note.IsSpecified() || updatedByManager.Note.IsNull() || updatedByManager.Note.MustGet() != note {
		t.Fatalf("Note = %+v, want %q", updatedByManager.Note, note)
	}

	// A discount that would exceed the (now 40.00) subtotal is rejected.
	badDiscount := "100.00"
	_, err = updateDraft(cashier1Ctx, t, h, pool, q, draft.Id, &gen.SaleDraftPatch{
		DiscountType: nullable.NewNullableWithValue("fixed"), DiscountValue: nullable.NewNullableWithValue(badDiscount),
	})
	assertAPIErr(t, "discount exceeds subtotal", err, 409, gen.DISCOUNTEXCEEDSSUBTOTAL)
}

func TestDeleteSaleDraft_ownershipCascadeAnd404(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "draft-delete")
	cashier1 := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	cashier2 := seedUser(ctx, t, q, shop.ID, "cashier2", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "draft-delete-product", "10.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	cashier1Ctx := ctxAs(shop.ID, cashier1)
	cashier2Ctx := ctxAs(shop.ID, cashier2)

	draft, err := createDraft(cashier1Ctx, t, h, pool, q, draftBody(loc.ID, variant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createDraft: %v", err)
	}

	assertAPIErr(t, "another cashier deletes", deleteDraft(cashier2Ctx, t, h, pool, q, draft.Id), 403, gen.FORBIDDEN)

	if err := deleteDraft(cashier1Ctx, t, h, pool, q, draft.Id); err != nil {
		t.Fatalf("deleteDraft (own): %v", err)
	}

	var draftCount, itemCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sale_drafts WHERE shop_id = $1`, shop.ID).Scan(&draftCount); err != nil {
		t.Fatalf("count sale_drafts: %v", err)
	}
	if draftCount != 0 {
		t.Fatalf("sale_drafts rows = %d, want 0", draftCount)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sale_draft_items WHERE shop_id = $1`, shop.ID).Scan(&itemCount); err != nil {
		t.Fatalf("count sale_draft_items: %v", err)
	}
	if itemCount != 0 {
		t.Fatalf("sale_draft_items rows = %d, want 0 (ON DELETE CASCADE)", itemCount)
	}

	assertAPIErr(t, "re-delete already-deleted draft", deleteDraft(cashier1Ctx, t, h, pool, q, draft.Id), 404, gen.NOTFOUND)
}

func TestCompleteSaleDraftTx_createsSaleAndDeletesDraft(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "draft-complete")
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "draft-complete-product", "50.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	cashierCtx := ctxAs(shop.ID, cashier)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "5.000")

	draft, err := createDraft(cashierCtx, t, h, pool, q, draftBody(loc.ID, variant.ID, "2.000"))
	if err != nil {
		t.Fatalf("createDraft: %v", err)
	}

	sale, err := completeDraft(cashierCtx, t, h, pool, q, draft.Id, &gen.SaleDraftComplete{PaymentMethod: gen.Cash})
	if err != nil {
		t.Fatalf("completeDraft: %v", err)
	}
	if sale.Total != "100.00" || sale.Number == 0 {
		t.Fatalf("Total/Number = %s/%d, want 100.00/nonzero (a completed draft gets a real sale number, D-87)", sale.Total, sale.Number)
	}

	if n := countMovements(ctx, t, pool, shop.ID, variant.ID, loc.ID, db.StockMovementKindSaleOut); n != 1 {
		t.Fatalf("sale_out movements = %d, want 1", n)
	}
	if avail, exists := readLevel(ctx, t, pool, shop.ID, variant.ID, loc.ID); !exists || !avail.Equal(d(t, "3.000")) {
		t.Fatalf("stock level = %v (exists=%v), want 3.000", avail, exists)
	}

	var draftCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sale_drafts WHERE shop_id = $1`, shop.ID).Scan(&draftCount); err != nil {
		t.Fatalf("count sale_drafts: %v", err)
	}
	if draftCount != 0 {
		t.Fatalf("sale_drafts rows = %d, want 0 (completion deletes the draft in the same transaction)", draftCount)
	}
}

func TestCompleteSaleDraftTx_insufficientStockLeavesNoSaleDraftIntact(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "draft-complete-insufficient")
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "draft-insufficient-product", "10.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	cashierCtx := ctxAs(shop.ID, cashier)
	// No stock at all — draft creation never checks it (D-88); only
	// completion does, through the reused CreateSaleTx path.

	draft, err := createDraft(cashierCtx, t, h, pool, q, draftBody(loc.ID, variant.ID, "5.000"))
	if err != nil {
		t.Fatalf("createDraft: %v", err)
	}

	_, err = completeDraft(cashierCtx, t, h, pool, q, draft.Id, &gen.SaleDraftComplete{PaymentMethod: gen.Cash})
	assertAPIErr(t, "complete with insufficient stock", err, 409, gen.STOCKINSUFFICIENT)

	if n := countSales(ctx, t, pool, shop.ID); n != 0 {
		t.Fatalf("sales rows = %d, want 0 (a failed completion must not create one)", n)
	}
	var draftCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sale_drafts WHERE shop_id = $1`, shop.ID).Scan(&draftCount); err != nil {
		t.Fatalf("count sale_drafts: %v", err)
	}
	if draftCount != 1 {
		t.Fatalf("sale_drafts rows = %d, want 1 (the failed completion must not delete it)", draftCount)
	}
}

func TestSaleDrafts_crossShopAccessIs404(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shopA := seedShop(ctx, t, q, "draft-shop-a")
	shopB := seedShop(ctx, t, q, "draft-shop-b")
	cashierA := seedUser(ctx, t, q, shopA.ID, "cashier1", db.UserRoleCashier)
	cashierB := seedUser(ctx, t, q, shopB.ID, "cashier1", db.UserRoleCashier)
	unitA := seedUnit(ctx, t, q, shopA.ID, "pcs")
	productA := seedProduct(ctx, t, q, shopA.ID, unitA.ID, "cross-shop-product", "10.00", productOpts{})
	variantA := seedVariant(ctx, t, q, shopA.ID, productA.ID)
	locA := seedLocation(ctx, t, q, shopA.ID, "Main")
	cashierACtx := ctxAs(shopA.ID, cashierA)
	cashierBCtx := ctxAs(shopB.ID, cashierB)

	draft, err := createDraft(cashierACtx, t, h, pool, q, draftBody(locA.ID, variantA.ID, "1.000"))
	if err != nil {
		t.Fatalf("createDraft: %v", err)
	}

	_, err = h.GetSaleDraft(cashierBCtx, gen.GetSaleDraftRequestObject{Id: draft.Id})
	assertAPIErr(t, "get across shops", err, 404, gen.NOTFOUND)

	_, err = updateDraft(cashierBCtx, t, h, pool, q, draft.Id, &gen.SaleDraftPatch{Note: nullable.NewNullableWithValue("x")})
	assertAPIErr(t, "patch across shops", err, 404, gen.NOTFOUND)

	assertAPIErr(t, "delete across shops", deleteDraft(cashierBCtx, t, h, pool, q, draft.Id), 404, gen.NOTFOUND)

	_, err = completeDraft(cashierBCtx, t, h, pool, q, draft.Id, &gen.SaleDraftComplete{PaymentMethod: gen.Cash})
	assertAPIErr(t, "complete across shops", err, 404, gen.NOTFOUND)
}

func TestGetSaleDraft_responseHasNoCostFields(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "draft-no-cost")
	owner := seedUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "draft-no-cost-product", "100.00", productOpts{costPrice: strPtr("40.00")})
	variant := seedVariantWithCostOverride(ctx, t, q, shop.ID, product.ID, "65.00")
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	ownerCtx := ctxAs(shop.ID, owner)

	// The owner has cost.read — if a draft response ever leaked cost, it
	// would show up here first.
	draft, err := createDraft(ownerCtx, t, h, pool, q, draftBody(loc.ID, variant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createDraft: %v", err)
	}

	resp, err := h.GetSaleDraft(ownerCtx, gen.GetSaleDraftRequestObject{Id: draft.Id})
	if err != nil {
		t.Fatalf("GetSaleDraft: %v", err)
	}
	got, ok := resp.(gen.GetSaleDraft200JSONResponse)
	if !ok {
		t.Fatalf("GetSaleDraft response type = %T", resp)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "unitCost") || strings.Contains(string(raw), "costPrice") ||
		strings.Contains(string(raw), "65.00") || strings.Contains(string(raw), "40.00") {
		t.Fatalf("draft response leaks a cost value, even for an owner (hard rule 5): %s", raw)
	}
}

// TestSaleDraft_unavailableLineRendersZeroAndBlocksCompletion is the
// review CRITICAL fix's own test: a variant soft-deleted after its line
// was added to a draft must not 500 or 404 a read — GET, List and a
// PATCH that leaves it alone all render the line with available=false,
// priced at zero and excluded from the subtotal, so the draft stays
// editable; only completion actually rejects it, 422 naming the line
// (D-88).
func TestSaleDraft_unavailableLineRendersZeroAndBlocksCompletion(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "draft-unavailable")
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	healthyProduct := seedProduct(ctx, t, q, shop.ID, unit.ID, "draft-unavailable-healthy", "10.00", productOpts{})
	healthyVariant := seedVariant(ctx, t, q, shop.ID, healthyProduct.ID)
	poisonedProduct := seedProduct(ctx, t, q, shop.ID, unit.ID, "draft-unavailable-poisoned", "50.00", productOpts{})
	poisonedVariant := seedVariant(ctx, t, q, shop.ID, poisonedProduct.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	cashierCtx := ctxAs(shop.ID, cashier)

	draft, err := createDraft(cashierCtx, t, h, pool, q, &gen.SaleDraftCreate{
		LocationId: loc.ID,
		Items: []gen.SaleItemCreate{
			{VariantId: healthyVariant.ID, Qty: "1.000"},
			{VariantId: poisonedVariant.ID, Qty: "2.000"},
		},
	})
	if err != nil {
		t.Fatalf("createDraft: %v", err)
	}
	if draft.Subtotal != "110.00" {
		t.Fatalf("Subtotal = %q, want 110.00 (10 + 2*50) before the poisoned variant is soft-deleted", draft.Subtotal)
	}

	// Soft-delete the second variant after the draft was created — the
	// real-world case this line's `available` flag renders.
	if _, err := pool.Exec(ctx, `UPDATE product_variants SET deleted_at = now() WHERE id = $1`, poisonedVariant.ID); err != nil {
		t.Fatalf("soft-delete variant: %v", err)
	}

	// GET: the poisoned line renders available=false, priced at zero,
	// excluded from the subtotal; the healthy line is unaffected.
	resp, err := h.GetSaleDraft(cashierCtx, gen.GetSaleDraftRequestObject{Id: draft.Id})
	if err != nil {
		t.Fatalf("GetSaleDraft: %v", err)
	}
	got, ok := resp.(gen.GetSaleDraft200JSONResponse)
	if !ok {
		t.Fatalf("GetSaleDraft response type = %T", resp)
	}
	if got.Subtotal != "10.00" {
		t.Fatalf("Subtotal = %q, want 10.00 (poisoned line excluded)", got.Subtotal)
	}
	var healthyItem, poisonedItem *gen.SaleDraftItem
	for i := range got.Items {
		switch got.Items[i].VariantId {
		case healthyVariant.ID:
			healthyItem = &got.Items[i]
		case poisonedVariant.ID:
			poisonedItem = &got.Items[i]
		}
	}
	if healthyItem == nil || !healthyItem.Available || healthyItem.UnitPrice != "10.00" {
		t.Fatalf("healthy item = %+v, want available=true unitPrice=10.00", healthyItem)
	}
	if poisonedItem == nil || poisonedItem.Available || poisonedItem.UnitPrice != "0.00" || poisonedItem.LineTotal != "0.00" {
		t.Fatalf("poisoned item = %+v, want available=false unitPrice=0.00 lineTotal=0.00", poisonedItem)
	}

	// List renders the same draft the same way.
	listResp, err := h.ListSaleDrafts(cashierCtx, gen.ListSaleDraftsRequestObject{})
	if err != nil {
		t.Fatalf("ListSaleDrafts: %v", err)
	}
	list, ok := listResp.(gen.ListSaleDrafts200JSONResponse)
	if !ok || len(list.Items) != 1 || list.Items[0].Subtotal != "10.00" {
		t.Fatalf("ListSaleDrafts = %+v, want one draft with subtotal 10.00", list)
	}

	// A PATCH touching an unrelated field (note) still succeeds and
	// still renders the poisoned line as unavailable.
	patched, err := updateDraft(cashierCtx, t, h, pool, q, draft.Id, &gen.SaleDraftPatch{Note: nullable.NewNullableWithValue("still editable")})
	if err != nil {
		t.Fatalf("updateDraft (note only): %v", err)
	}
	if patched.Subtotal != "10.00" {
		t.Fatalf("Subtotal after patch = %q, want 10.00", patched.Subtotal)
	}

	// Completion fails outright, naming the poisoned line, and leaves
	// the draft, stock and sales untouched.
	stockIn(ctx, t, pool, q, shop.ID, healthyVariant.ID, loc.ID, "5.000")
	_, err = completeDraft(cashierCtx, t, h, pool, q, draft.Id, &gen.SaleDraftComplete{PaymentMethod: gen.Cash})
	if err == nil {
		t.Fatal("want 422 VALIDATION_FAILED for the unavailable line, got no error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 422 || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("error = %v, want 422 VALIDATION_FAILED", err)
	}
	fields, ok := apiErr.Details["fields"].(map[string]string)
	if !ok || len(fields) != 1 {
		t.Fatalf("Details.fields = %+v, want exactly one field naming the poisoned line", apiErr.Details)
	}

	if n := countSales(ctx, t, pool, shop.ID); n != 0 {
		t.Fatalf("sales rows = %d, want 0", n)
	}
	var draftCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sale_drafts WHERE shop_id = $1`, shop.ID).Scan(&draftCount); err != nil {
		t.Fatalf("count sale_drafts: %v", err)
	}
	if draftCount != 1 {
		t.Fatalf("sale_drafts rows = %d, want 1 (a failed completion must not delete it)", draftCount)
	}
}

// TestListSaleDrafts_mixedHealthyAndPoisonedDraftsBothRender proves the
// batched pricing (priceDraftItemsBatch, review MAJOR N+1 fix) renders
// every draft on a page correctly even when one of them has an
// unavailable line — a poisoned draft must never take the whole page
// down, nor silently disappear from it.
func TestListSaleDrafts_mixedHealthyAndPoisonedDraftsBothRender(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "draft-list-mixed")
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	healthyProduct := seedProduct(ctx, t, q, shop.ID, unit.ID, "draft-list-mixed-healthy", "10.00", productOpts{})
	healthyVariant := seedVariant(ctx, t, q, shop.ID, healthyProduct.ID)
	poisonedProduct := seedProduct(ctx, t, q, shop.ID, unit.ID, "draft-list-mixed-poisoned", "20.00", productOpts{})
	poisonedVariant := seedVariant(ctx, t, q, shop.ID, poisonedProduct.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	cashierCtx := ctxAs(shop.ID, cashier)

	healthyDraft, err := createDraft(cashierCtx, t, h, pool, q, draftBody(loc.ID, healthyVariant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createDraft (healthy): %v", err)
	}
	poisonedDraft, err := createDraft(cashierCtx, t, h, pool, q, draftBody(loc.ID, poisonedVariant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createDraft (poisoned): %v", err)
	}
	// Deactivated, not soft-deleted this time — the other half of
	// rowAvailable's check (product_available, not variant_available).
	if _, err := pool.Exec(ctx, `UPDATE products SET is_active = false WHERE id = $1`, poisonedProduct.ID); err != nil {
		t.Fatalf("deactivate product: %v", err)
	}

	resp, err := h.ListSaleDrafts(cashierCtx, gen.ListSaleDraftsRequestObject{})
	if err != nil {
		t.Fatalf("ListSaleDrafts: %v", err)
	}
	list, ok := resp.(gen.ListSaleDrafts200JSONResponse)
	if !ok || len(list.Items) != 2 {
		t.Fatalf("ListSaleDrafts = %+v, want exactly 2 drafts", list)
	}

	byID := map[uuid.UUID]gen.SaleDraft{}
	for _, dr := range list.Items {
		byID[dr.Id] = dr
	}
	healthy, ok := byID[healthyDraft.Id]
	if !ok || len(healthy.Items) != 1 || !healthy.Items[0].Available || healthy.Subtotal != "10.00" {
		t.Fatalf("healthy draft = %+v, want available=true subtotal=10.00", healthy)
	}
	poisoned, ok := byID[poisonedDraft.Id]
	if !ok || len(poisoned.Items) != 1 || poisoned.Items[0].Available || poisoned.Subtotal != "0.00" {
		t.Fatalf("poisoned draft = %+v, want available=false subtotal=0.00", poisoned)
	}
}

// TestUpdateSaleDraft_partialDiscountPairIsRejected is review MAJOR's own
// test: naming only one of discountType/discountValue — with or without
// an explicit `null` on the other — must 400 VALIDATION_FAILED before
// any UPDATE runs, never surface the sale_drafts CHECK constraint as an
// opaque 500.
func TestUpdateSaleDraft_partialDiscountPairIsRejected(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "draft-partial-discount")
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "draft-partial-discount-product", "100.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	cashierCtx := ctxAs(shop.ID, cashier)

	draft, err := createDraft(cashierCtx, t, h, pool, q, draftBody(loc.ID, variant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createDraft: %v", err)
	}
	// The draft has no stored discount at all.

	assertPartialDiscount400 := func(label string, patch *gen.SaleDraftPatch) {
		t.Helper()
		_, err := updateDraft(cashierCtx, t, h, pool, q, draft.Id, patch)
		var apiErr *apierr.Error
		if !errors.As(err, &apiErr) || apiErr.Status != 400 || apiErr.Code != gen.VALIDATIONFAILED {
			t.Fatalf("%s: error = %v, want 400 VALIDATION_FAILED", label, err)
		}
	}

	assertPartialDiscount400("discountType only", &gen.SaleDraftPatch{
		DiscountType: nullable.NewNullableWithValue("fixed"),
	})
	assertPartialDiscount400("discountValue only", &gen.SaleDraftPatch{
		DiscountValue: nullable.NewNullableWithValue("10.00"),
	})
	// Explicit null on discountType while giving a real discountValue —
	// a contradictory pair, also 400 (not silently "clear both").
	assertPartialDiscount400("null type + real value", &gen.SaleDraftPatch{
		DiscountType: nullable.NewNullNullable[string](), DiscountValue: nullable.NewNullableWithValue("100.00"),
	})

	// None of the rejected attempts touched the draft.
	resp, err := h.GetSaleDraft(cashierCtx, gen.GetSaleDraftRequestObject{Id: draft.Id})
	if err != nil {
		t.Fatalf("GetSaleDraft: %v", err)
	}
	got := resp.(gen.GetSaleDraft200JSONResponse)
	if !got.Discount.IsNull() {
		t.Fatalf("Discount = %+v, want still null", got.Discount)
	}
}

// TestUpdateSaleDraft_noteOnlyPatchIgnoresStaleDiscount is review MINOR's
// own test: a PATCH touching neither `items` nor the discount must
// succeed even when the already-stored discount has since gone stale
// against the current subtotal (a price drop after the draft was
// created) — only a PATCH that touches items or the discount itself
// re-validates and rejects that state.
func TestUpdateSaleDraft_noteOnlyPatchIgnoresStaleDiscount(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "draft-stale-discount")
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "draft-stale-discount-product", "100.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	cashierCtx := ctxAs(shop.ID, cashier)

	draft, err := createDraft(cashierCtx, t, h, pool, q, &gen.SaleDraftCreate{
		LocationId: loc.ID,
		Items:      []gen.SaleItemCreate{{VariantId: variant.ID, Qty: "1.000"}},
		Discount:   &gen.SaleDiscount{Type: gen.Fixed, Value: "90.00"},
	})
	if err != nil {
		t.Fatalf("createDraft: %v", err)
	}
	// 90.00 discount on a 100.00 subtotal — fine at creation.

	// The price drops after the draft was created, so the stored
	// discount would now exceed the new (30.00) subtotal.
	if _, err := pool.Exec(ctx, `UPDATE products SET base_price = '30.00' WHERE id = $1`, product.ID); err != nil {
		t.Fatalf("update product: %v", err)
	}

	updated, err := updateDraft(cashierCtx, t, h, pool, q, draft.Id, &gen.SaleDraftPatch{Note: nullable.NewNullableWithValue("gift wrap")})
	if err != nil {
		t.Fatalf("updateDraft (note only, stale discount): %v", err)
	}
	if !updated.Note.IsSpecified() || updated.Note.IsNull() || updated.Note.MustGet() != "gift wrap" {
		t.Fatalf("Note = %+v, want gift wrap", updated.Note)
	}
	// The response still reflects reality: the discount is capped at
	// the new (smaller) subtotal, not rejected.
	if updated.Subtotal != "30.00" || updated.DiscountAmount != "30.00" || updated.EstimatedTotal != "0.00" {
		t.Fatalf("Subtotal/DiscountAmount/EstimatedTotal = %s/%s/%s, want 30.00/30.00/0.00",
			updated.Subtotal, updated.DiscountAmount, updated.EstimatedTotal)
	}
}

// TestCompleteSaleDraftTx_anyCashierMayCompleteAnyDraft is D-96's own
// test: completing a draft is not restricted to its own creator — any
// staff who may create a sale may complete any draft, and the resulting
// sale is booked under the completing cashier, not the draft's creator.
func TestCompleteSaleDraftTx_anyCashierMayCompleteAnyDraft(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "draft-cross-cashier-complete")
	creator := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	completer := seedUser(ctx, t, q, shop.ID, "cashier2", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "draft-cross-cashier-product", "10.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	creatorCtx := ctxAs(shop.ID, creator)
	completerCtx := ctxAs(shop.ID, completer)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "5.000")

	draft, err := createDraft(creatorCtx, t, h, pool, q, draftBody(loc.ID, variant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createDraft: %v", err)
	}

	sale, err := completeDraft(completerCtx, t, h, pool, q, draft.Id, &gen.SaleDraftComplete{PaymentMethod: gen.Cash})
	if err != nil {
		t.Fatalf("completeDraft (different cashier, D-96): %v", err)
	}
	if sale.CashierId != completer.ID {
		t.Fatalf("CashierId = %s, want %s (the sale is booked under the completing cashier, D-96)", sale.CashierId, completer.ID)
	}
}

// TestCompleteSaleDraftTx_concurrentCompletesYieldOneSaleOneMovementLoser404
// is Opus' own probe turned into a real test: two transactions racing to
// complete the same draft both call GetSaleDraftForUpdate, which
// Postgres row-locks — the loser blocks until the winner's commit
// deletes the draft, then simply finds no row (404 "draft"), never a
// second sale or a second sale_out movement.
func TestCompleteSaleDraftTx_concurrentCompletesYieldOneSaleOneMovementLoser404(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := sales.NewHandler(sales.NewService(q))

	shop := seedShop(ctx, t, q, "draft-concurrent-complete")
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "draft-concurrent-product", "10.00", productOpts{})
	variant := seedVariant(ctx, t, q, shop.ID, product.ID)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	cashierCtx := ctxAs(shop.ID, cashier)
	stockIn(ctx, t, pool, q, shop.ID, variant.ID, loc.ID, "5.000")

	draft, err := createDraft(cashierCtx, t, h, pool, q, draftBody(loc.ID, variant.ID, "1.000"))
	if err != nil {
		t.Fatalf("createDraft: %v", err)
	}

	run := func() error {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		qtx := q.WithTx(tx)
		if _, err := h.CompleteSaleDraftTx(cashierCtx, qtx, draft.Id, &gen.SaleDraftComplete{PaymentMethod: gen.Cash}); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		return tx.Commit(ctx)
	}

	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			errs <- run()
		}()
	}
	wg.Wait()
	close(errs)

	var successes, failures int
	var failErr error
	for err := range errs {
		if err == nil {
			successes++
		} else {
			failures++
			failErr = err
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("want exactly one success and one failure, got %d successes, %d failures (last failure: %v)", successes, failures, failErr)
	}
	var apiErr *apierr.Error
	if !errors.As(failErr, &apiErr) || apiErr.Status != 404 {
		t.Fatalf("loser error = %v, want 404 NOT_FOUND", failErr)
	}

	if n := countSales(ctx, t, pool, shop.ID); n != 1 {
		t.Fatalf("sales rows = %d, want exactly 1", n)
	}
	if n := countMovements(ctx, t, pool, shop.ID, variant.ID, loc.ID, db.StockMovementKindSaleOut); n != 1 {
		t.Fatalf("sale_out movements = %d, want exactly 1", n)
	}
}
