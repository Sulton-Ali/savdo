package stock_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oapi-codegen/nullable"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/stock"
)

func seedSupplier(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, name string) db.Supplier {
	t.Helper()
	s, err := q.CreateSupplier(ctx, db.CreateSupplierParams{ID: uuid.New(), ShopID: shopID, Name: name})
	if err != nil {
		t.Fatalf("seedSupplier(%q): %v", name, err)
	}
	return s
}

// purchaseCreateReq builds a CreatePurchaseRequestObject for one item.
func purchaseCreateReq(supplierID, locationID, variantID uuid.UUID, qty, unitCost string) gen.CreatePurchaseRequestObject {
	return gen.CreatePurchaseRequestObject{Body: &gen.PurchaseCreate{
		SupplierId: supplierID, LocationId: locationID,
		Items: []gen.PurchaseItemCreate{{VariantId: variantID, Qty: qty, UnitCost: unitCost}},
	}}
}

// receivePurchase runs ReceivePurchaseTx on its own transaction, committing
// on success and rolling back on error — the shape httpx.Idempotent gives
// it in production, reproduced directly here since this package's own
// tests call the handler method, not the httpx wrapper (that wrapper's own
// idempotency-replay tests live in internal/httpx/purchases_test.go).
func receivePurchase(ctx context.Context, t *testing.T, h *stock.Handler, pool *pgxpool.Pool, q *db.Queries, id uuid.UUID) (gen.Purchase, error) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("receivePurchase: begin: %v", err)
	}
	qtx := q.WithTx(tx)
	resp, err := h.ReceivePurchaseTx(ctx, qtx, id)
	if err != nil {
		_ = tx.Rollback(ctx)
		return gen.Purchase{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("receivePurchase: commit: %v", err)
	}
	return resp, nil
}

func mustCreatePurchase(t *testing.T, h *stock.Handler, ctx context.Context, supplierID, locationID, variantID uuid.UUID, qty, unitCost string) gen.Purchase {
	t.Helper()
	resp, err := h.CreatePurchase(ctx, purchaseCreateReq(supplierID, locationID, variantID, qty, unitCost))
	if err != nil {
		t.Fatalf("CreatePurchase: %v", err)
	}
	created, ok := resp.(gen.CreatePurchase201JSONResponse)
	if !ok {
		t.Fatalf("CreatePurchase response type = %T", resp)
	}
	return gen.Purchase(created)
}

func assertConflict(t *testing.T, label string, err error, code gen.ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: want 409 %s, got no error", label, code)
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 409 || apiErr.Code != code {
		t.Fatalf("%s: error = %v, want 409 %s", label, err, code)
	}
}

func TestCreatePurchase_computesTotalsAndSequentialNumbers(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := stock.NewHandler(stock.NewService(pool, q))

	shop := seedShop(ctx, t, q, "purchase-create")
	manager := seedUser(ctx, t, q, shop.ID, "manager1", db.UserRoleManager)
	supplier := seedSupplier(ctx, t, q, shop.ID, "Acme Supplier")
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "purchase-product")
	variant := seedVariant(ctx, t, q, shop.ID, product.ID, "{}")
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	managerCtx := ctxAs(shop.ID, manager)

	first := mustCreatePurchase(t, h, managerCtx, supplier.ID, loc.ID, variant.ID, "10.000", "1500.00")
	if first.Number != "P-000001" {
		t.Fatalf("first Number = %q, want P-000001", first.Number)
	}
	if first.TotalCost != "15000.00" {
		t.Fatalf("first TotalCost = %q, want 15000.00 (10 * 1500)", first.TotalCost)
	}
	if len(first.Items) != 1 || first.Items[0].UnitCost != "1500.00" || first.Items[0].Qty != "10.000" {
		t.Fatalf("first Items = %+v, want one item qty=10.000 unitCost=1500.00", first.Items)
	}
	if first.Status != gen.Draft {
		t.Fatalf("Status = %q, want draft", first.Status)
	}

	second := mustCreatePurchase(t, h, managerCtx, supplier.ID, loc.ID, variant.ID, "2.000", "1000.00")
	if second.Number != "P-000002" {
		t.Fatalf("second Number = %q, want P-000002 (sequential per shop)", second.Number)
	}
}

func TestCreatePurchase_itemProductNameVariantLabelSku(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := stock.NewHandler(stock.NewService(pool, q))

	shop := seedShop(ctx, t, q, "purchase-labels")
	manager := seedUser(ctx, t, q, shop.ID, "manager1", db.UserRoleManager)
	supplier := seedSupplier(ctx, t, q, shop.ID, "Label Supplier")
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "label-product")
	if err := q.UpsertProductTranslation(ctx, db.UpsertProductTranslationParams{
		ProductID: product.ID, Locale: "uz", Name: "Ko'ylak",
	}); err != nil {
		t.Fatalf("UpsertProductTranslation: %v", err)
	}
	sizeAttr := seedAttributeDefinition(ctx, t, q, shop.ID, "size", 1)
	variant, err := q.CreateVariant(ctx, db.CreateVariantParams{
		ID: uuid.New(), ShopID: shop.ID, ProductID: product.ID,
		Attributes: json.RawMessage(`{"` + sizeAttr.Code + `":"L"}`), IsActive: true,
	})
	if err != nil {
		t.Fatalf("CreateVariant: %v", err)
	}
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	managerCtx := ctxAs(shop.ID, manager)

	created := mustCreatePurchase(t, h, managerCtx, supplier.ID, loc.ID, variant.ID, "3.000", "500.00")
	if len(created.Items) != 1 {
		t.Fatalf("Items = %+v, want 1", created.Items)
	}
	item := created.Items[0]
	if item.ProductName != "Ko'ylak" {
		t.Fatalf("ProductName = %q, want Ko'ylak (shop default_locale uz)", item.ProductName)
	}
	if item.VariantLabel != "L" {
		t.Fatalf("VariantLabel = %q, want L (the variant's size attribute value)", item.VariantLabel)
	}
	if item.Sku.IsSpecified() && !item.Sku.IsNull() {
		t.Fatalf("Sku = %+v, want null (variant has no sku)", item.Sku)
	}
}

func TestCreatePurchase_foreignSupplierLocationVariant404(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := stock.NewHandler(stock.NewService(pool, q))

	shop := seedShop(ctx, t, q, "purchase-404")
	manager := seedUser(ctx, t, q, shop.ID, "manager1", db.UserRoleManager)
	supplier := seedSupplier(ctx, t, q, shop.ID, "Real Supplier")
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "p404")
	variant := seedVariant(ctx, t, q, shop.ID, product.ID, "{}")
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	managerCtx := ctxAs(shop.ID, manager)

	foreignID := uuid.New()

	assert404 := func(label string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: want 404, got no error", label)
		}
		var apiErr *apierr.Error
		if !errors.As(err, &apiErr) || apiErr.Status != 404 {
			t.Fatalf("%s: error = %v, want 404 NOT_FOUND", label, err)
		}
	}

	_, err := h.CreatePurchase(managerCtx, purchaseCreateReq(foreignID, loc.ID, variant.ID, "1.000", "10.00"))
	assert404("foreign supplierId", err)

	_, err = h.CreatePurchase(managerCtx, purchaseCreateReq(supplier.ID, foreignID, variant.ID, "1.000", "10.00"))
	assert404("foreign locationId", err)

	_, err = h.CreatePurchase(managerCtx, purchaseCreateReq(supplier.ID, loc.ID, foreignID, "1.000", "10.00"))
	assert404("foreign variantId", err)
}

func TestCreatePurchase_itemValidation(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := stock.NewHandler(stock.NewService(pool, q))

	shop := seedShop(ctx, t, q, "purchase-validation")
	manager := seedUser(ctx, t, q, shop.ID, "manager1", db.UserRoleManager)
	supplier := seedSupplier(ctx, t, q, shop.ID, "Validation Supplier")
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "pval")
	variant := seedVariant(ctx, t, q, shop.ID, product.ID, "{}")
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	managerCtx := ctxAs(shop.ID, manager)

	assert400 := func(label string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: want 400, got no error", label)
		}
		var apiErr *apierr.Error
		if !errors.As(err, &apiErr) || apiErr.Status != 400 {
			t.Fatalf("%s: error = %v, want 400 VALIDATION_FAILED", label, err)
		}
	}

	_, err := h.CreatePurchase(managerCtx, purchaseCreateReq(supplier.ID, loc.ID, variant.ID, "0.000", "10.00"))
	assert400("qty zero", err)

	_, err = h.CreatePurchase(managerCtx, purchaseCreateReq(supplier.ID, loc.ID, variant.ID, "-1.000", "10.00"))
	assert400("qty negative", err)

	_, err = h.CreatePurchase(managerCtx, purchaseCreateReq(supplier.ID, loc.ID, variant.ID, "1.000", "-10.00"))
	assert400("unitCost negative", err)

	_, err = h.CreatePurchase(managerCtx, gen.CreatePurchaseRequestObject{Body: &gen.PurchaseCreate{
		SupplierId: supplier.ID, LocationId: loc.ID, Items: []gen.PurchaseItemCreate{},
	}})
	assert400("empty items", err)
}

func TestUpdatePurchase_draftPatchReplacesItemsAndRecomputesTotal(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := stock.NewHandler(stock.NewService(pool, q))

	shop := seedShop(ctx, t, q, "purchase-patch")
	manager := seedUser(ctx, t, q, shop.ID, "manager1", db.UserRoleManager)
	supplier := seedSupplier(ctx, t, q, shop.ID, "Patch Supplier")
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "ppatch")
	variantA := seedVariant(ctx, t, q, shop.ID, product.ID, `{}`)
	variantB, err := q.CreateVariant(ctx, db.CreateVariantParams{
		ID: uuid.New(), ShopID: shop.ID, ProductID: product.ID, Attributes: json.RawMessage(`{"x":"1"}`), IsActive: true,
	})
	if err != nil {
		t.Fatalf("CreateVariant: %v", err)
	}
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	managerCtx := ctxAs(shop.ID, manager)

	created := mustCreatePurchase(t, h, managerCtx, supplier.ID, loc.ID, variantA.ID, "10.000", "100.00")
	if created.TotalCost != "1000.00" {
		t.Fatalf("initial TotalCost = %q, want 1000.00", created.TotalCost)
	}

	note := "updated note"
	resp, err := h.UpdatePurchase(managerCtx, gen.UpdatePurchaseRequestObject{Id: created.Id, Body: &gen.PurchasePatch{
		Note:  nullable.NewNullableWithValue(note),
		Items: &[]gen.PurchaseItemCreate{{VariantId: variantB.ID, Qty: "3.000", UnitCost: "50.00"}},
	}})
	if err != nil {
		t.Fatalf("UpdatePurchase: %v", err)
	}
	updated, ok := resp.(gen.UpdatePurchase200JSONResponse)
	if !ok {
		t.Fatalf("UpdatePurchase response type = %T", resp)
	}
	if updated.TotalCost != "150.00" {
		t.Fatalf("TotalCost after replace = %q, want 150.00 (3 * 50, the old item is gone)", updated.TotalCost)
	}
	if len(updated.Items) != 1 || updated.Items[0].VariantId != variantB.ID {
		t.Fatalf("Items after replace = %+v, want exactly variant B's line", updated.Items)
	}
	if !updated.Note.IsSpecified() || updated.Note.MustGet() != note {
		t.Fatalf("Note = %+v, want %q", updated.Note, note)
	}
}

func TestUpdatePurchase_notDraftIs409(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := stock.NewHandler(stock.NewService(pool, q))

	shop := seedShop(ctx, t, q, "purchase-not-draft")
	manager := seedUser(ctx, t, q, shop.ID, "manager1", db.UserRoleManager)
	supplier := seedSupplier(ctx, t, q, shop.ID, "Not Draft Supplier")
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "pnodraft")
	variant := seedVariant(ctx, t, q, shop.ID, product.ID, "{}")
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	managerCtx := ctxAs(shop.ID, manager)

	created := mustCreatePurchase(t, h, managerCtx, supplier.ID, loc.ID, variant.ID, "10.000", "100.00")
	if _, err := receivePurchase(managerCtx, t, h, pool, q, created.Id); err != nil {
		t.Fatalf("receivePurchase: %v", err)
	}

	_, err := h.UpdatePurchase(managerCtx, gen.UpdatePurchaseRequestObject{Id: created.Id, Body: &gen.PurchasePatch{
		Note: nullable.NewNullableWithValue("no"),
	}})
	assertConflict(t, "UpdatePurchase after receive", err, gen.PURCHASENOTDRAFT)
}

func TestReceivePurchaseTx_writesMovementAndLevel(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := stock.NewHandler(stock.NewService(pool, q))

	shop := seedShop(ctx, t, q, "purchase-receive")
	manager := seedUser(ctx, t, q, shop.ID, "manager1", db.UserRoleManager)
	supplier := seedSupplier(ctx, t, q, shop.ID, "Receive Supplier")
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "preceive")
	variant := seedVariant(ctx, t, q, shop.ID, product.ID, "{}")
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	managerCtx := ctxAs(shop.ID, manager)

	created := mustCreatePurchase(t, h, managerCtx, supplier.ID, loc.ID, variant.ID, "10.000", "250.00")

	received, err := receivePurchase(managerCtx, t, h, pool, q, created.Id)
	if err != nil {
		t.Fatalf("receivePurchase: %v", err)
	}
	if received.Status != gen.Received {
		t.Fatalf("Status = %q, want received", received.Status)
	}
	if !received.ReceivedAt.IsSpecified() || received.ReceivedAt.IsNull() {
		t.Fatalf("ReceivedAt = %+v, want set", received.ReceivedAt)
	}

	count := countMovements(ctx, t, pool, shop.ID, variant.ID, loc.ID, db.StockMovementKindPurchaseIn)
	if count != 1 {
		t.Fatalf("purchase_in movements = %d, want 1", count)
	}
	level, exists := readLevel(ctx, t, pool, shop.ID, variant.ID, loc.ID)
	if !exists || !level.Equal(d(t, "10.000")) {
		t.Fatalf("level = (%s, exists=%v), want (10.000, true)", level, exists)
	}
}

func TestReceivePurchaseTx_costOverrideOnAndOff(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := stock.NewHandler(stock.NewService(pool, q))

	// Setting on (the shop default, D-48): cost_override is set to the
	// line's unit_cost.
	shopOn := seedShop(ctx, t, q, "purchase-cost-on")
	managerOn := seedUser(ctx, t, q, shopOn.ID, "manager1", db.UserRoleManager)
	supplierOn := seedSupplier(ctx, t, q, shopOn.ID, "Cost On Supplier")
	unitOn := seedUnit(ctx, t, q, shopOn.ID, "pcs")
	productOn := seedProduct(ctx, t, q, shopOn.ID, unitOn.ID, "pcoston")
	variantOn := seedVariant(ctx, t, q, shopOn.ID, productOn.ID, "{}")
	locOn := seedLocation(ctx, t, q, shopOn.ID, "Main")
	ctxOn := ctxAs(shopOn.ID, managerOn)

	createdOn := mustCreatePurchase(t, h, ctxOn, supplierOn.ID, locOn.ID, variantOn.ID, "5.000", "777.00")
	if _, err := receivePurchase(ctxOn, t, h, pool, q, createdOn.Id); err != nil {
		t.Fatalf("receivePurchase (cost on): %v", err)
	}
	freshOn, err := q.GetVariantForStaff(ctx, db.GetVariantForStaffParams{ShopID: shopOn.ID, ID: variantOn.ID})
	if err != nil {
		t.Fatalf("GetVariantForStaff (cost on): %v", err)
	}
	if !freshOn.CostOverride.Valid {
		t.Fatal("cost_override = NULL, want set to 777.00 (update_cost_on_purchase defaults on)")
	}
	got, err := freshOn.CostOverride.Value()
	if err != nil {
		t.Fatalf("CostOverride.Value: %v", err)
	}
	if got != "777.00" {
		t.Fatalf("cost_override = %v, want 777.00", got)
	}

	// Setting off: cost_override stays untouched (nil).
	shopOff := seedShop(ctx, t, q, "purchase-cost-off")
	allowNeg := false
	updateOff := false
	if _, err := q.UpdateShop(ctx, db.UpdateShopParams{ID: shopOff.ID, AllowNegativeStock: &allowNeg, UpdateCostOnPurchase: &updateOff}); err != nil {
		t.Fatalf("UpdateShop (cost off): %v", err)
	}
	managerOff := seedUser(ctx, t, q, shopOff.ID, "manager1", db.UserRoleManager)
	supplierOff := seedSupplier(ctx, t, q, shopOff.ID, "Cost Off Supplier")
	unitOff := seedUnit(ctx, t, q, shopOff.ID, "pcs")
	productOff := seedProduct(ctx, t, q, shopOff.ID, unitOff.ID, "pcostoff")
	variantOff := seedVariant(ctx, t, q, shopOff.ID, productOff.ID, "{}")
	locOff := seedLocation(ctx, t, q, shopOff.ID, "Main")
	ctxOff := ctxAs(shopOff.ID, managerOff)

	createdOff := mustCreatePurchase(t, h, ctxOff, supplierOff.ID, locOff.ID, variantOff.ID, "5.000", "999.00")
	if _, err := receivePurchase(ctxOff, t, h, pool, q, createdOff.Id); err != nil {
		t.Fatalf("receivePurchase (cost off): %v", err)
	}
	freshOff, err := q.GetVariantForStaff(ctx, db.GetVariantForStaffParams{ShopID: shopOff.ID, ID: variantOff.ID})
	if err != nil {
		t.Fatalf("GetVariantForStaff (cost off): %v", err)
	}
	if freshOff.CostOverride.Valid {
		t.Fatalf("cost_override = %v, want left NULL (update_cost_on_purchase off)", freshOff.CostOverride)
	}
}

func TestReceivePurchaseTx_alreadyReceivedAndCancelled(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := stock.NewHandler(stock.NewService(pool, q))

	shop := seedShop(ctx, t, q, "purchase-receive-twice")
	manager := seedUser(ctx, t, q, shop.ID, "manager1", db.UserRoleManager)
	supplier := seedSupplier(ctx, t, q, shop.ID, "Twice Supplier")
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "ptwice")
	variant := seedVariant(ctx, t, q, shop.ID, product.ID, "{}")
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	managerCtx := ctxAs(shop.ID, manager)

	created := mustCreatePurchase(t, h, managerCtx, supplier.ID, loc.ID, variant.ID, "10.000", "100.00")
	if _, err := receivePurchase(managerCtx, t, h, pool, q, created.Id); err != nil {
		t.Fatalf("first receive: %v", err)
	}
	_, err := receivePurchase(managerCtx, t, h, pool, q, created.Id)
	assertConflict(t, "second receive", err, gen.PURCHASEALREADYRECEIVED)

	createdB := mustCreatePurchase(t, h, managerCtx, supplier.ID, loc.ID, variant.ID, "1.000", "1.00")
	if _, err := h.CancelPurchase(managerCtx, gen.CancelPurchaseRequestObject{Id: createdB.Id}); err != nil {
		t.Fatalf("cancel draft: %v", err)
	}
	_, err = receivePurchase(managerCtx, t, h, pool, q, createdB.Id)
	assertConflict(t, "receive a cancelled purchase", err, gen.PURCHASEALREADYCANCELLED)
}

func TestReceivePurchaseTx_writesAuditRow(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := stock.NewHandler(stock.NewService(pool, q))

	shop := seedShop(ctx, t, q, "purchase-receive-audit")
	manager := seedUser(ctx, t, q, shop.ID, "manager1", db.UserRoleManager)
	supplier := seedSupplier(ctx, t, q, shop.ID, "Audit Supplier")
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "paudit")
	variant := seedVariant(ctx, t, q, shop.ID, product.ID, "{}")
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	managerCtx := ctxAs(shop.ID, manager)

	created := mustCreatePurchase(t, h, managerCtx, supplier.ID, loc.ID, variant.ID, "4.000", "10.00")
	if _, err := receivePurchase(managerCtx, t, h, pool, q, created.Id); err != nil {
		t.Fatalf("receivePurchase: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_log WHERE shop_id = $1 AND entity_type = 'purchase' AND entity_id = $2 AND action = 'purchase.receive'`,
		shop.ID, created.Id).Scan(&count); err != nil {
		t.Fatalf("count audit_log: %v", err)
	}
	if count != 1 {
		t.Fatalf("audit_log rows for the receive = %d, want 1", count)
	}
}

func TestCancelPurchase_draftWritesNoMovement(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := stock.NewHandler(stock.NewService(pool, q))

	shop := seedShop(ctx, t, q, "purchase-cancel-draft")
	manager := seedUser(ctx, t, q, shop.ID, "manager1", db.UserRoleManager)
	supplier := seedSupplier(ctx, t, q, shop.ID, "Cancel Draft Supplier")
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "pcanceldraft")
	variant := seedVariant(ctx, t, q, shop.ID, product.ID, "{}")
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	managerCtx := ctxAs(shop.ID, manager)

	created := mustCreatePurchase(t, h, managerCtx, supplier.ID, loc.ID, variant.ID, "6.000", "50.00")

	resp, err := h.CancelPurchase(managerCtx, gen.CancelPurchaseRequestObject{Id: created.Id})
	if err != nil {
		t.Fatalf("CancelPurchase: %v", err)
	}
	cancelled, ok := resp.(gen.CancelPurchase200JSONResponse)
	if !ok {
		t.Fatalf("CancelPurchase response type = %T", resp)
	}
	if cancelled.Status != gen.Cancelled {
		t.Fatalf("Status = %q, want cancelled", cancelled.Status)
	}

	if count := countMovements(ctx, t, pool, shop.ID, variant.ID, loc.ID, ""); count != 0 {
		t.Fatalf("movements after cancelling a draft = %d, want 0", count)
	}
	if _, exists := readLevel(ctx, t, pool, shop.ID, variant.ID, loc.ID); exists {
		t.Fatal("a stock_levels row must not exist for a draft that was never received")
	}

	_, err = h.CancelPurchase(managerCtx, gen.CancelPurchaseRequestObject{Id: created.Id})
	assertConflict(t, "cancel an already-cancelled purchase", err, gen.PURCHASEALREADYCANCELLED)
}

func TestCancelPurchase_receivedWritesReversingMovementsAndRebuildMatches(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := stock.NewHandler(stock.NewService(pool, q))

	shop := seedShop(ctx, t, q, "purchase-cancel-received")
	manager := seedUser(ctx, t, q, shop.ID, "manager1", db.UserRoleManager)
	supplier := seedSupplier(ctx, t, q, shop.ID, "Cancel Received Supplier")
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "pcancelreceived")
	variant := seedVariant(ctx, t, q, shop.ID, product.ID, "{}")
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	managerCtx := ctxAs(shop.ID, manager)

	created := mustCreatePurchase(t, h, managerCtx, supplier.ID, loc.ID, variant.ID, "8.000", "20.00")
	if _, err := receivePurchase(managerCtx, t, h, pool, q, created.Id); err != nil {
		t.Fatalf("receivePurchase: %v", err)
	}
	before, exists := readLevel(ctx, t, pool, shop.ID, variant.ID, loc.ID)
	if !exists || !before.Equal(d(t, "8.000")) {
		t.Fatalf("level before cancel = (%s, exists=%v), want (8.000, true)", before, exists)
	}

	resp, err := h.CancelPurchase(managerCtx, gen.CancelPurchaseRequestObject{Id: created.Id})
	if err != nil {
		t.Fatalf("CancelPurchase: %v", err)
	}
	cancelled, ok := resp.(gen.CancelPurchase200JSONResponse)
	if !ok {
		t.Fatalf("CancelPurchase response type = %T", resp)
	}
	if cancelled.Status != gen.Cancelled {
		t.Fatalf("Status = %q, want cancelled", cancelled.Status)
	}

	after, exists := readLevel(ctx, t, pool, shop.ID, variant.ID, loc.ID)
	if !exists || !after.Equal(d(t, "0.000")) {
		t.Fatalf("level after cancel = (%s, exists=%v), want (0.000, true) — back to the pre-receive value", after, exists)
	}

	var reversalCount int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM stock_movements WHERE shop_id = $1 AND variant_id = $2 AND location_id = $3 AND kind = 'purchase_in' AND ref_type = 'purchase_cancel'`,
		shop.ID, variant.ID, loc.ID).Scan(&reversalCount); err != nil {
		t.Fatalf("count reversal movements: %v", err)
	}
	if reversalCount != 1 {
		t.Fatalf("purchase_in/purchase_cancel movements = %d, want 1", reversalCount)
	}
	var reversalQty string
	if err := pool.QueryRow(ctx,
		`SELECT qty FROM stock_movements WHERE shop_id = $1 AND variant_id = $2 AND location_id = $3 AND ref_type = 'purchase_cancel'`,
		shop.ID, variant.ID, loc.ID).Scan(&reversalQty); err != nil {
		t.Fatalf("read reversal qty: %v", err)
	}
	if reversalQty != "-8.000" {
		t.Fatalf("reversal qty = %q, want -8.000", reversalQty)
	}

	var auditCount int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_log WHERE shop_id = $1 AND entity_type = 'purchase' AND entity_id = $2 AND action = 'purchase.cancel'`,
		shop.ID, created.Id).Scan(&auditCount); err != nil {
		t.Fatalf("count audit_log: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("audit_log rows for the cancel = %d, want 1", auditCount)
	}

	result, err := stock.Rebuild(ctx, pool, shop.Slug)
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if result.Movements != 2 {
		t.Fatalf("Rebuild Movements = %d, want 2 (the receive + the cancel reversal)", result.Movements)
	}
	rebuiltLevel, exists := readLevel(ctx, t, pool, shop.ID, variant.ID, loc.ID)
	if !exists || !rebuiltLevel.Equal(d(t, "0.000")) {
		t.Fatalf("level after rebuild = (%s, exists=%v), want (0.000, true), matching the pre-rebuild value", rebuiltLevel, exists)
	}
}

func TestCancelPurchase_receivedWithStockAlreadyMovedOut(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := stock.NewHandler(stock.NewService(pool, q))
	svc := stock.NewService(pool, q)

	// allow_negative_stock off (default): the reversal must fail with
	// STOCK_INSUFFICIENT and leave everything unchanged.
	shopOff := seedShop(ctx, t, q, "purchase-cancel-insufficient")
	managerOff := seedUser(ctx, t, q, shopOff.ID, "manager1", db.UserRoleManager)
	supplierOff := seedSupplier(ctx, t, q, shopOff.ID, "Insufficient Supplier")
	unitOff := seedUnit(ctx, t, q, shopOff.ID, "pcs")
	productOff := seedProduct(ctx, t, q, shopOff.ID, unitOff.ID, "pinsufficient")
	variantOff := seedVariant(ctx, t, q, shopOff.ID, productOff.ID, "{}")
	locOff := seedLocation(ctx, t, q, shopOff.ID, "Main")
	ctxOff := ctxAs(shopOff.ID, managerOff)

	createdOff := mustCreatePurchase(t, h, ctxOff, supplierOff.ID, locOff.ID, variantOff.ID, "10.000", "1.00")
	if _, err := receivePurchase(ctxOff, t, h, pool, q, createdOff.Id); err != nil {
		t.Fatalf("receivePurchase: %v", err)
	}
	// Half the stock leaves through a sale before the purchase is
	// cancelled — the reversal (-10) would need the level to reach -5.
	if _, err := svc.MoveInTx(ctx, stock.MoveParams{
		ShopID: shopOff.ID, VariantID: variantOff.ID, LocationID: locOff.ID,
		Kind: db.StockMovementKindSaleOut, Qty: d(t, "-5.000"),
	}); err != nil {
		t.Fatalf("sale out: %v", err)
	}

	_, err := h.CancelPurchase(ctxOff, gen.CancelPurchaseRequestObject{Id: createdOff.Id})
	if err == nil {
		t.Fatal("want 409 STOCK_INSUFFICIENT, got no error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 409 || apiErr.Code != gen.STOCKINSUFFICIENT {
		t.Fatalf("error = %v, want 409 STOCK_INSUFFICIENT", err)
	}

	// Nothing changed: the purchase is still received, the level is still
	// 5, no reversal movement was written.
	stillReceived, err := q.GetPurchase(ctx, db.GetPurchaseParams{ShopID: shopOff.ID, ID: createdOff.Id})
	if err != nil {
		t.Fatalf("GetPurchase: %v", err)
	}
	if stillReceived.Status != db.PurchaseStatusReceived {
		t.Fatalf("Status after failed cancel = %q, want received (unchanged)", stillReceived.Status)
	}
	level, exists := readLevel(ctx, t, pool, shopOff.ID, variantOff.ID, locOff.ID)
	if !exists || !level.Equal(d(t, "5.000")) {
		t.Fatalf("level after failed cancel = (%s, exists=%v), want (5.000, true), unchanged", level, exists)
	}
	if count := countMovements(ctx, t, pool, shopOff.ID, variantOff.ID, locOff.ID, ""); count != 2 {
		t.Fatalf("movements after failed cancel = %d, want 2 (the receive and the sale only)", count)
	}

	// allow_negative_stock on: the same reversal succeeds and the level
	// goes negative.
	shopOn := seedShopAllowNegative(ctx, t, q, "purchase-cancel-negative-ok")
	managerOn := seedUser(ctx, t, q, shopOn.ID, "manager1", db.UserRoleManager)
	supplierOn := seedSupplier(ctx, t, q, shopOn.ID, "Negative Ok Supplier")
	unitOn := seedUnit(ctx, t, q, shopOn.ID, "pcs")
	productOn := seedProduct(ctx, t, q, shopOn.ID, unitOn.ID, "pnegok")
	variantOn := seedVariant(ctx, t, q, shopOn.ID, productOn.ID, "{}")
	locOn := seedLocation(ctx, t, q, shopOn.ID, "Main")
	ctxOn := ctxAs(shopOn.ID, managerOn)

	createdOn := mustCreatePurchase(t, h, ctxOn, supplierOn.ID, locOn.ID, variantOn.ID, "10.000", "1.00")
	if _, err := receivePurchase(ctxOn, t, h, pool, q, createdOn.Id); err != nil {
		t.Fatalf("receivePurchase (allow negative): %v", err)
	}
	if _, err := svc.MoveInTx(ctx, stock.MoveParams{
		ShopID: shopOn.ID, VariantID: variantOn.ID, LocationID: locOn.ID,
		Kind: db.StockMovementKindSaleOut, Qty: d(t, "-5.000"),
	}); err != nil {
		t.Fatalf("sale out (allow negative): %v", err)
	}

	if _, err := h.CancelPurchase(ctxOn, gen.CancelPurchaseRequestObject{Id: createdOn.Id}); err != nil {
		t.Fatalf("CancelPurchase (allow negative): %v", err)
	}
	levelOn, exists := readLevel(ctx, t, pool, shopOn.ID, variantOn.ID, locOn.ID)
	if !exists || !levelOn.Equal(d(t, "-5.000")) {
		t.Fatalf("level after cancel with allow_negative_stock on = (%s, exists=%v), want (-5.000, true)", levelOn, exists)
	}
}

func TestPurchasesAndSuppliers_cashierForbiddenOnEveryRoute(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := stock.NewHandler(stock.NewService(pool, q))

	shop := seedShop(ctx, t, q, "purchase-cashier")
	owner := seedUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	supplier := seedSupplier(ctx, t, q, shop.ID, "Cashier Blocked Supplier")
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "pcashier")
	variant := seedVariant(ctx, t, q, shop.ID, product.ID, "{}")
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	ownerCtx := ctxAs(shop.ID, owner)
	cashierCtx := ctxAs(shop.ID, cashier)

	created := mustCreatePurchase(t, h, ownerCtx, supplier.ID, loc.ID, variant.ID, "1.000", "1.00")

	assertForbidden(t, "ListPurchases", func() error {
		_, err := h.ListPurchases(cashierCtx, gen.ListPurchasesRequestObject{})
		return err
	})
	assertForbidden(t, "GetPurchase", func() error {
		_, err := h.GetPurchase(cashierCtx, gen.GetPurchaseRequestObject{Id: created.Id})
		return err
	})
	assertForbidden(t, "CreatePurchase", func() error {
		_, err := h.CreatePurchase(cashierCtx, purchaseCreateReq(supplier.ID, loc.ID, variant.ID, "1.000", "1.00"))
		return err
	})
	assertForbidden(t, "UpdatePurchase", func() error {
		_, err := h.UpdatePurchase(cashierCtx, gen.UpdatePurchaseRequestObject{Id: created.Id, Body: &gen.PurchasePatch{}})
		return err
	})
	assertForbidden(t, "ReceivePurchaseTx", func() error {
		_, err := h.ReceivePurchaseTx(cashierCtx, nil, created.Id)
		return err
	})
	assertForbidden(t, "CancelPurchase", func() error {
		_, err := h.CancelPurchase(cashierCtx, gen.CancelPurchaseRequestObject{Id: created.Id})
		return err
	})
}

func TestListPurchases_isolatedPerShop(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := stock.NewHandler(stock.NewService(pool, q))

	shopA := seedShop(ctx, t, q, "purchase-list-a")
	shopB := seedShop(ctx, t, q, "purchase-list-b")
	managerA := seedUser(ctx, t, q, shopA.ID, "manager-a", db.UserRoleManager)
	managerB := seedUser(ctx, t, q, shopB.ID, "manager-b", db.UserRoleManager)
	supplierA := seedSupplier(ctx, t, q, shopA.ID, "Supplier A")
	supplierB := seedSupplier(ctx, t, q, shopB.ID, "Supplier B")
	unitA := seedUnit(ctx, t, q, shopA.ID, "pcs")
	unitB := seedUnit(ctx, t, q, shopB.ID, "pcs")
	productA := seedProduct(ctx, t, q, shopA.ID, unitA.ID, "plist-a")
	productB := seedProduct(ctx, t, q, shopB.ID, unitB.ID, "plist-b")
	variantA := seedVariant(ctx, t, q, shopA.ID, productA.ID, "{}")
	variantB := seedVariant(ctx, t, q, shopB.ID, productB.ID, "{}")
	locA := seedLocation(ctx, t, q, shopA.ID, "Main A")
	locB := seedLocation(ctx, t, q, shopB.ID, "Main B")

	mustCreatePurchase(t, h, ctxAs(shopA.ID, managerA), supplierA.ID, locA.ID, variantA.ID, "1.000", "1.00")
	mustCreatePurchase(t, h, ctxAs(shopB.ID, managerB), supplierB.ID, locB.ID, variantB.ID, "1.000", "1.00")

	resp, err := h.ListPurchases(ctxAs(shopA.ID, managerA), gen.ListPurchasesRequestObject{})
	if err != nil {
		t.Fatalf("ListPurchases(shop A): %v", err)
	}
	list, ok := resp.(gen.ListPurchases200JSONResponse)
	if !ok {
		t.Fatalf("ListPurchases response type = %T", resp)
	}
	if len(list.Items) != 1 || list.Items[0].SupplierId != supplierA.ID {
		t.Fatalf("shop A purchases = %+v, want exactly one, supplier A's", list.Items)
	}
}

// seedAttributeDefinition creates an attribute definition (no
// translation — purchases_test.go only needs its Code to build a
// variant's attributes JSON, never its display name).
func seedAttributeDefinition(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, code string, sortOrder int32) db.AttributeDefinition {
	t.Helper()
	a, err := q.CreateAttributeDefinition(ctx, db.CreateAttributeDefinitionParams{ID: uuid.New(), ShopID: shopID, Code: code, SortOrder: sortOrder})
	if err != nil {
		t.Fatalf("seedAttributeDefinition(%q): %v", code, err)
	}
	return a
}
