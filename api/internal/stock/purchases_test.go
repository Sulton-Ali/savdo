package stock_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oapi-codegen/nullable"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/catalog"
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

func mustCreatePurchase(ctx context.Context, t *testing.T, h *stock.Handler, supplierID, locationID, variantID uuid.UUID, qty, unitCost string) gen.Purchase {
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

	first := mustCreatePurchase(managerCtx, t, h, supplier.ID, loc.ID, variant.ID, "10.000", "1500.00")
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

	second := mustCreatePurchase(managerCtx, t, h, supplier.ID, loc.ID, variant.ID, "2.000", "1000.00")
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

	created := mustCreatePurchase(managerCtx, t, h, supplier.ID, loc.ID, variant.ID, "3.000", "500.00")
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

	created := mustCreatePurchase(managerCtx, t, h, supplier.ID, loc.ID, variantA.ID, "10.000", "100.00")
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

	created := mustCreatePurchase(managerCtx, t, h, supplier.ID, loc.ID, variant.ID, "10.000", "100.00")
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

	created := mustCreatePurchase(managerCtx, t, h, supplier.ID, loc.ID, variant.ID, "10.000", "250.00")

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

	createdOn := mustCreatePurchase(ctxOn, t, h, supplierOn.ID, locOn.ID, variantOn.ID, "5.000", "777.00")
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

	createdOff := mustCreatePurchase(ctxOff, t, h, supplierOff.ID, locOff.ID, variantOff.ID, "5.000", "999.00")
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

	created := mustCreatePurchase(managerCtx, t, h, supplier.ID, loc.ID, variant.ID, "10.000", "100.00")
	if _, err := receivePurchase(managerCtx, t, h, pool, q, created.Id); err != nil {
		t.Fatalf("first receive: %v", err)
	}
	_, err := receivePurchase(managerCtx, t, h, pool, q, created.Id)
	assertConflict(t, "second receive", err, gen.PURCHASEALREADYRECEIVED)

	createdB := mustCreatePurchase(managerCtx, t, h, supplier.ID, loc.ID, variant.ID, "1.000", "1.00")
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

	created := mustCreatePurchase(managerCtx, t, h, supplier.ID, loc.ID, variant.ID, "4.000", "10.00")
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

	created := mustCreatePurchase(managerCtx, t, h, supplier.ID, loc.ID, variant.ID, "6.000", "50.00")

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

	created := mustCreatePurchase(managerCtx, t, h, supplier.ID, loc.ID, variant.ID, "8.000", "20.00")
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

	createdOff := mustCreatePurchase(ctxOff, t, h, supplierOff.ID, locOff.ID, variantOff.ID, "10.000", "1.00")
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

	createdOn := mustCreatePurchase(ctxOn, t, h, supplierOn.ID, locOn.ID, variantOn.ID, "10.000", "1.00")
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

	created := mustCreatePurchase(ownerCtx, t, h, supplier.ID, loc.ID, variant.ID, "1.000", "1.00")

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

	mustCreatePurchase(ctxAs(shopA.ID, managerA), t, h, supplierA.ID, locA.ID, variantA.ID, "1.000", "1.00")
	mustCreatePurchase(ctxAs(shopB.ID, managerB), t, h, supplierB.ID, locB.ID, variantB.ID, "1.000", "1.00")

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

// withAcceptLanguage runs fn with ctx carrying header as the request's
// `Accept-Language` (catalog.AcceptLanguageMiddleware's own stashing,
// exercised directly here rather than via a full router) — the only way
// to exercise stock's own catalog.ResolveLocale call sites without
// standing up httpx.NewRouter's whole middleware chain (MAJOR 1, T4
// review).
func withAcceptLanguage(ctx context.Context, t *testing.T, header string, fn func(context.Context)) {
	t.Helper()
	mw := catalog.AcceptLanguageMiddleware(func(ctx context.Context, _ http.ResponseWriter, _ *http.Request, _ any) (any, error) {
		fn(ctx)
		return nil, nil
	}, "test")
	req, err := http.NewRequest(http.MethodGet, "/", nil)
	if err != nil {
		t.Fatalf("withAcceptLanguage: NewRequest: %v", err)
	}
	if header != "" {
		req.Header.Set("Accept-Language", header)
	}
	if _, err := mw(ctx, nil, req, nil); err != nil {
		t.Fatalf("withAcceptLanguage: middleware: %v", err)
	}
}

// TestGetPurchase_productNameHonoursAcceptLanguage is MAJOR 1's own test
// (T4 review): a product with both uz and ru names, requested with
// `Accept-Language: ru` resolves productName in ru; requested with no
// header at all falls back to the shop's default_locale (uz) — exactly
// Product.name's own requested -> uz -> any rule (ADR-012).
func TestGetPurchase_productNameHonoursAcceptLanguage(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := stock.NewHandler(stock.NewService(pool, q))

	shop := seedShop(ctx, t, q, "purchase-accept-language")
	manager := seedUser(ctx, t, q, shop.ID, "manager1", db.UserRoleManager)
	supplier := seedSupplier(ctx, t, q, shop.ID, "Locale Supplier")
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "accept-language-product")
	if err := q.UpsertProductTranslation(ctx, db.UpsertProductTranslationParams{ProductID: product.ID, Locale: "uz", Name: "Koylak"}); err != nil {
		t.Fatalf("UpsertProductTranslation(uz): %v", err)
	}
	if err := q.UpsertProductTranslation(ctx, db.UpsertProductTranslationParams{ProductID: product.ID, Locale: "ru", Name: "Rubashka"}); err != nil {
		t.Fatalf("UpsertProductTranslation(ru): %v", err)
	}
	variant := seedVariant(ctx, t, q, shop.ID, product.ID, "{}")
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	managerCtx := ctxAs(shop.ID, manager)

	created := mustCreatePurchase(managerCtx, t, h, supplier.ID, loc.ID, variant.ID, "1.000", "10.00")

	withAcceptLanguage(managerCtx, t, "ru", func(ctx context.Context) {
		resp, err := h.GetPurchase(ctx, gen.GetPurchaseRequestObject{Id: created.Id})
		if err != nil {
			t.Fatalf("GetPurchase (Accept-Language: ru): %v", err)
		}
		got, ok := resp.(gen.GetPurchase200JSONResponse)
		if !ok {
			t.Fatalf("GetPurchase response type = %T", resp)
		}
		if len(got.Items) != 1 || got.Items[0].ProductName != "Rubashka" {
			t.Fatalf("Items = %+v, want productName Rubashka (Accept-Language: ru)", got.Items)
		}
	})

	withAcceptLanguage(managerCtx, t, "", func(ctx context.Context) {
		resp, err := h.GetPurchase(ctx, gen.GetPurchaseRequestObject{Id: created.Id})
		if err != nil {
			t.Fatalf("GetPurchase (no Accept-Language): %v", err)
		}
		got, ok := resp.(gen.GetPurchase200JSONResponse)
		if !ok {
			t.Fatalf("GetPurchase response type = %T", resp)
		}
		if len(got.Items) != 1 || got.Items[0].ProductName != "Koylak" {
			t.Fatalf("Items = %+v, want productName Koylak (shop default_locale, no header)", got.Items)
		}
	})
}

// repeatPurchaseItem builds n items all naming the same variantID — used
// by TestCreatePurchase_itemsFieldValidation's "too many items" row, where
// the count itself is what must be rejected before any per-item field
// (including the very duplication this same variantID would otherwise
// also trigger) is ever examined.
func repeatPurchaseItem(variantID uuid.UUID, n int) []gen.PurchaseItemCreate {
	items := make([]gen.PurchaseItemCreate, n)
	for i := range items {
		items[i] = gen.PurchaseItemCreate{VariantId: variantID, Qty: "1.000", UnitCost: "1.00"}
	}
	return items
}

// TestCreatePurchase_itemsFieldValidation is MINOR 3's and MINOR 8's tests
// (T4 review, the latter added as a review-residual table row rather than
// its own function): two items of the same PurchaseCreate naming the same
// variantId is 400 VALIDATION_FAILED fields.items=invalid; more than
// maxPurchaseItems (200) items is 400 fields.items=too_long, checked
// before any per-item validation (so 201 copies of the same variantId —
// which would also be a duplicate — still reports too_long, not invalid).
func TestCreatePurchase_itemsFieldValidation(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := stock.NewHandler(stock.NewService(pool, q))

	shop := seedShop(ctx, t, q, "purchase-items-field-validation")
	manager := seedUser(ctx, t, q, shop.ID, "manager1", db.UserRoleManager)
	supplier := seedSupplier(ctx, t, q, shop.ID, "Items Field Validation Supplier")
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "items-field-validation-product")
	variant := seedVariant(ctx, t, q, shop.ID, product.ID, "{}")
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	managerCtx := ctxAs(shop.ID, manager)

	tests := []struct {
		name       string
		items      []gen.PurchaseItemCreate
		wantReason string
	}{
		{
			name: "duplicate variantId",
			items: []gen.PurchaseItemCreate{
				{VariantId: variant.ID, Qty: "1.000", UnitCost: "1.00"},
				{VariantId: variant.ID, Qty: "2.000", UnitCost: "2.00"},
			},
			wantReason: "invalid",
		},
		{
			name:       "more than 200 items",
			items:      repeatPurchaseItem(variant.ID, 201),
			wantReason: "too_long",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.CreatePurchase(managerCtx, gen.CreatePurchaseRequestObject{Body: &gen.PurchaseCreate{
				SupplierId: supplier.ID, LocationId: loc.ID, Items: tt.items,
			}})
			if err == nil {
				t.Fatalf("want 400 VALIDATION_FAILED, got no error")
			}
			var apiErr *apierr.Error
			if !errors.As(err, &apiErr) || apiErr.Status != 400 {
				t.Fatalf("error = %v, want 400 VALIDATION_FAILED", err)
			}
			if got := apiErr.Details["fields"].(map[string]string)["items"]; got != tt.wantReason {
				t.Fatalf("details.fields.items = %q, want %q", got, tt.wantReason)
			}
		})
	}
}

// TestCreatePurchase_lineTotalOutOfRangeMapsTo400 is MINOR 4's test (T4
// review): a qty and unitCost that each individually pass their own
// bound (parseQty's numeric(12,3), money.ParseAmount's numeric(14,2)) can
// still multiply into a line_total Postgres' numeric(14,2) column cannot
// hold — CreatePurchaseItem's own insert must map that the same way
// Move already does (mapOutOfRange), not surface an opaque 500.
func TestCreatePurchase_lineTotalOutOfRangeMapsTo400(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := stock.NewHandler(stock.NewService(pool, q))

	shop := seedShop(ctx, t, q, "purchase-out-of-range")
	manager := seedUser(ctx, t, q, shop.ID, "manager1", db.UserRoleManager)
	supplier := seedSupplier(ctx, t, q, shop.ID, "Out Of Range Supplier")
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "out-of-range-product")
	variant := seedVariant(ctx, t, q, shop.ID, product.ID, "{}")
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	managerCtx := ctxAs(shop.ID, manager)

	_, err := h.CreatePurchase(managerCtx, purchaseCreateReq(supplier.ID, loc.ID, variant.ID, "999999999.999", "999999999999.99"))
	if err == nil {
		t.Fatal("want 400 VALIDATION_FAILED for a line_total out of numeric(14,2) range, got no error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 400 {
		t.Fatalf("error = %v, want 400 VALIDATION_FAILED", err)
	}
	if apiErr.Details["fields"].(map[string]string)["qty"] != "invalid" {
		t.Fatalf("details = %v, want fields.qty=invalid (mapOutOfRange's own shape)", apiErr.Details)
	}
}

// TestReceiveAndCancelPurchase_multiItem is MAJOR 2's test (T4 review),
// both halves:
//
// (a) receiving a three-item purchase (variants inserted out of order in
// the request) writes one purchase_in per line, in ascending variant_id
// order (Move's own deadlock-avoidance rule), totalCost is the sum of the
// three line totals, and the stored purchases.total_cost column equals
// the response totalCost (Sonnet MINOR 2 / Opus MINOR 5).
//
// (b) cancelling that received purchase after only one variant's stock
// left (a sale, not a purchase-cancel) — deliberately the variant whose
// id sorts LAST, so at least one reversing Move has already succeeded
// before the failing one, making the all-or-nothing assertion meaningful
// — is 409 STOCK_INSUFFICIENT, writes zero purchase_cancel movements (the
// whole transaction rolls back, not just the failing line), leaves the
// first-sorting variant's level unchanged, leaves the purchase status
// received, and stock.Rebuild still matches the (unchanged) levels.
func TestReceiveAndCancelPurchase_multiItem(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := stock.NewHandler(stock.NewService(pool, q))
	svc := stock.NewService(pool, q)

	shop := seedShop(ctx, t, q, "purchase-multi-item")
	manager := seedUser(ctx, t, q, shop.ID, "manager1", db.UserRoleManager)
	supplier := seedSupplier(ctx, t, q, shop.ID, "Multi Item Supplier")
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "multi-item-product")
	v1 := seedVariant(ctx, t, q, shop.ID, product.ID, `{"k":"1"}`)
	v2 := seedVariant(ctx, t, q, shop.ID, product.ID, `{"k":"2"}`)
	v3 := seedVariant(ctx, t, q, shop.ID, product.ID, `{"k":"3"}`)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	managerCtx := ctxAs(shop.ID, manager)

	// seedVariant's ids are random (uuid.New(), v4) — sort them so part (b)
	// below can deliberately sell out whichever one sorts LAST. Cancel's
	// own Move calls run in ascending variant_id order (sortedPurchaseItems),
	// so selling out the last-sorting variant guarantees the first two
	// reversing Moves succeed before the third one fails — without this,
	// roughly one run in three would have picked a variant that happens to
	// sort first, making the "the whole transaction rolled back a
	// successful reversal" assertion below vacuous (nothing would yet have
	// succeeded when the failure hit).
	sortedByID := []db.ProductVariant{v1, v2, v3}
	sort.Slice(sortedByID, func(i, j int) bool { return sortedByID[i].ID.String() < sortedByID[j].ID.String() })
	firstSorted, sellOutVariant := sortedByID[0], sortedByID[2]

	// Inserted out of order (v3, v1, v2) — CreatePurchase itself does not
	// sort; ReceivePurchaseTx's own sortedPurchaseItems is what must.
	resp, err := h.CreatePurchase(managerCtx, gen.CreatePurchaseRequestObject{Body: &gen.PurchaseCreate{
		SupplierId: supplier.ID, LocationId: loc.ID,
		Items: []gen.PurchaseItemCreate{
			{VariantId: v3.ID, Qty: "5.000", UnitCost: "10.00"},
			{VariantId: v1.ID, Qty: "5.000", UnitCost: "10.00"},
			{VariantId: v2.ID, Qty: "5.000", UnitCost: "10.00"},
		},
	}})
	if err != nil {
		t.Fatalf("CreatePurchase: %v", err)
	}
	created, ok := resp.(gen.CreatePurchase201JSONResponse)
	if !ok {
		t.Fatalf("CreatePurchase response type = %T", resp)
	}

	received, err := receivePurchase(managerCtx, t, h, pool, q, created.Id)
	if err != nil {
		t.Fatalf("receivePurchase: %v", err)
	}
	if received.TotalCost != "150.00" {
		t.Fatalf("TotalCost = %q, want 150.00 (3 lines * 5 * 10.00)", received.TotalCost)
	}

	for _, v := range []struct {
		name string
		id   uuid.UUID
	}{{"v1", v1.ID}, {"v2", v2.ID}, {"v3", v3.ID}} {
		if count := countMovements(ctx, t, pool, shop.ID, v.id, loc.ID, db.StockMovementKindPurchaseIn); count != 1 {
			t.Fatalf("%s purchase_in movements = %d, want 1", v.name, count)
		}
	}

	// Movements were written in ascending variant_id order regardless of
	// the request's own item order — ids are UUID v7 (time-ordered), so
	// the insertion order Move actually ran in is recoverable by sorting
	// the written rows by id.
	rows, err := pool.Query(ctx, `SELECT variant_id FROM stock_movements WHERE shop_id = $1 AND ref_id = $2 ORDER BY id`, shop.ID, created.Id)
	if err != nil {
		t.Fatalf("query movement order: %v", err)
	}
	var gotOrder []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan movement variant_id: %v", err)
		}
		gotOrder = append(gotOrder, id)
	}
	rows.Close()
	wantOrder := []uuid.UUID{v1.ID, v2.ID, v3.ID}
	sort.Slice(wantOrder, func(i, j int) bool { return wantOrder[i].String() < wantOrder[j].String() })
	if len(gotOrder) != 3 || gotOrder[0] != wantOrder[0] || gotOrder[1] != wantOrder[1] || gotOrder[2] != wantOrder[2] {
		t.Fatalf("movement write order = %v, want ascending variant_id order %v", gotOrder, wantOrder)
	}

	dbPurchase, err := q.GetPurchase(ctx, db.GetPurchaseParams{ShopID: shop.ID, ID: created.Id})
	if err != nil {
		t.Fatalf("GetPurchase: %v", err)
	}
	storedTotal, err := dbPurchase.TotalCost.Value()
	if err != nil {
		t.Fatalf("TotalCost.Value: %v", err)
	}
	if storedTotal != "150.00" {
		t.Fatalf("stored purchases.total_cost = %v, want 150.00 (equal to the response totalCost)", storedTotal)
	}

	// (b) Only the last-sorting variant's stock leaves, via a sale — not a
	// purchase cancel.
	if _, err := svc.MoveInTx(ctx, stock.MoveParams{
		ShopID: shop.ID, VariantID: sellOutVariant.ID, LocationID: loc.ID,
		Kind: db.StockMovementKindSaleOut, Qty: d(t, "-5.000"),
	}); err != nil {
		t.Fatalf("sale out sellOutVariant: %v", err)
	}

	otherVariant := sortedByID[1]
	beforeFirst, _ := readLevel(ctx, t, pool, shop.ID, firstSorted.ID, loc.ID)
	beforeOther, _ := readLevel(ctx, t, pool, shop.ID, otherVariant.ID, loc.ID)

	_, err = h.CancelPurchase(managerCtx, gen.CancelPurchaseRequestObject{Id: created.Id})
	if err == nil {
		t.Fatal("want 409 STOCK_INSUFFICIENT, got no error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 409 || apiErr.Code != gen.STOCKINSUFFICIENT {
		t.Fatalf("error = %v, want 409 STOCK_INSUFFICIENT", err)
	}

	var cancelCount int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM stock_movements WHERE shop_id = $1 AND ref_type = 'purchase_cancel'`, shop.ID).Scan(&cancelCount); err != nil {
		t.Fatalf("count purchase_cancel movements: %v", err)
	}
	if cancelCount != 0 {
		t.Fatalf("purchase_cancel movements after a failed cancel = %d, want 0 (the whole transaction must roll back, including the first-sorting variant's already-successful reversal)", cancelCount)
	}

	afterFirst, exists := readLevel(ctx, t, pool, shop.ID, firstSorted.ID, loc.ID)
	if !exists || !afterFirst.Equal(beforeFirst) {
		t.Fatalf("first-sorting variant's level after the failed cancel = (%s, exists=%v), want unchanged at %s (its reversal ran and succeeded, then rolled back with everything else)", afterFirst, exists, beforeFirst)
	}

	stillReceived, err := q.GetPurchase(ctx, db.GetPurchaseParams{ShopID: shop.ID, ID: created.Id})
	if err != nil {
		t.Fatalf("GetPurchase after failed cancel: %v", err)
	}
	if stillReceived.Status != db.PurchaseStatusReceived {
		t.Fatalf("Status after failed cancel = %q, want received (unchanged)", stillReceived.Status)
	}

	beforeSellOut, _ := readLevel(ctx, t, pool, shop.ID, sellOutVariant.ID, loc.ID)
	result, err := stock.Rebuild(ctx, pool, shop.Slug)
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if result.Movements != 4 {
		t.Fatalf("Rebuild Movements = %d, want 4 (3 receives + 1 sale; nothing from the failed cancel)", result.Movements)
	}
	afterRebuildFirst, _ := readLevel(ctx, t, pool, shop.ID, firstSorted.ID, loc.ID)
	afterRebuildOther, _ := readLevel(ctx, t, pool, shop.ID, otherVariant.ID, loc.ID)
	afterRebuildSellOut, _ := readLevel(ctx, t, pool, shop.ID, sellOutVariant.ID, loc.ID)
	if !afterRebuildFirst.Equal(beforeFirst) || !afterRebuildOther.Equal(beforeOther) || !afterRebuildSellOut.Equal(beforeSellOut) {
		t.Fatalf("levels after rebuild = (%s, %s, %s), want unchanged (%s, %s, %s)",
			afterRebuildFirst, afterRebuildOther, afterRebuildSellOut, beforeFirst, beforeOther, beforeSellOut)
	}
}

// receiveStep/startReceiveHoldingLock/assertReceiveStillBlocked/
// waitReceiveLocked/finishReceive are ReceivePurchaseTx's own copy of
// move_test.go's moveStep/startMoveHoldingLock/assertStillBlocked/
// waitLocked/finish (MINOR 6, T4 review): begin a transaction, run
// ReceivePurchaseTx inside it, and block holding whatever row lock
// GetPurchaseForUpdate took until the test says commit — the same
// deterministic "genuinely blocked, not just not-yet-scheduled" shape,
// reused as separate functions (not generics) since the two step types
// carry a different result field and Go's stdlib channel plumbing is
// cheap to duplicate compared to a shared generic type.
type receiveStep struct {
	result gen.Purchase
	err    error
	locked chan struct{}
	commit chan struct{}
	done   chan error
}

func startReceiveHoldingLock(ctx context.Context, t *testing.T, pool *pgxpool.Pool, q *db.Queries, h *stock.Handler, id uuid.UUID) *receiveStep {
	t.Helper()
	s := &receiveStep{locked: make(chan struct{}), commit: make(chan struct{}), done: make(chan error, 1)}
	go func() {
		tx, err := pool.Begin(ctx)
		if err != nil {
			s.err = err
			close(s.locked)
			s.done <- err
			return
		}
		s.result, s.err = h.ReceivePurchaseTx(ctx, q.WithTx(tx), id)
		close(s.locked)

		<-s.commit
		if s.err != nil {
			s.done <- tx.Rollback(ctx)
			return
		}
		s.done <- tx.Commit(ctx)
	}()
	return s
}

func assertReceiveStillBlocked(t *testing.T, label string, s *receiveStep, timeout time.Duration) {
	t.Helper()
	select {
	case <-s.locked:
		t.Fatalf("%s: ReceivePurchaseTx returned before the blocking transaction committed, want it still blocked", label)
	case <-time.After(timeout):
	}
}

func waitReceiveLocked(t *testing.T, label string, s *receiveStep, timeout time.Duration) error {
	t.Helper()
	select {
	case <-s.locked:
		return s.err
	case <-time.After(timeout):
		t.Fatalf("%s: ReceivePurchaseTx did not return within %s", label, timeout)
		return nil
	}
}

func finishReceive(t *testing.T, label string, s *receiveStep, timeout time.Duration) error {
	t.Helper()
	close(s.commit)
	select {
	case err := <-s.done:
		return err
	case <-time.After(timeout):
		t.Fatalf("%s: commit/rollback did not complete within %s", label, timeout)
		return nil
	}
}

// TestReceivePurchaseTx_concurrentReceiveExactlyOneSucceeds is MINOR 6's
// test (T4 review): two goroutines racing ReceivePurchaseTx on the same
// draft purchase — deterministically interleaved the same way
// move_test.go's own TestMove_concurrentLastUnit_exactlyOneSucceeds is,
// via GetPurchaseForUpdate's row lock rather than stock_levels' — exactly
// one succeeds, the other (once unblocked) gets
// PURCHASE_ALREADY_RECEIVED, and exactly one purchase_in movement per
// line is ever written.
func TestReceivePurchaseTx_concurrentReceiveExactlyOneSucceeds(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := stock.NewHandler(stock.NewService(pool, q))

	shop := seedShop(ctx, t, q, "purchase-receive-race")
	manager := seedUser(ctx, t, q, shop.ID, "manager1", db.UserRoleManager)
	supplier := seedSupplier(ctx, t, q, shop.ID, "Race Supplier")
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "race-product")
	v1 := seedVariant(ctx, t, q, shop.ID, product.ID, `{"k":"1"}`)
	v2 := seedVariant(ctx, t, q, shop.ID, product.ID, `{"k":"2"}`)
	loc := seedLocation(ctx, t, q, shop.ID, "Main")
	managerCtx := ctxAs(shop.ID, manager)

	resp, err := h.CreatePurchase(managerCtx, gen.CreatePurchaseRequestObject{Body: &gen.PurchaseCreate{
		SupplierId: supplier.ID, LocationId: loc.ID,
		Items: []gen.PurchaseItemCreate{
			{VariantId: v1.ID, Qty: "3.000", UnitCost: "10.00"},
			{VariantId: v2.ID, Qty: "4.000", UnitCost: "10.00"},
		},
	}})
	if err != nil {
		t.Fatalf("CreatePurchase: %v", err)
	}
	created, ok := resp.(gen.CreatePurchase201JSONResponse)
	if !ok {
		t.Fatalf("CreatePurchase response type = %T", resp)
	}

	stepA := startReceiveHoldingLock(managerCtx, t, pool, q, h, created.Id)
	if err := waitReceiveLocked(t, "A", stepA, resultWait); err != nil {
		t.Fatalf("A: want ReceivePurchaseTx to succeed (first to the lock), got: %v", err)
	}

	stepB := startReceiveHoldingLock(managerCtx, t, pool, q, h, created.Id)
	assertReceiveStillBlocked(t, "B", stepB, blockedWait)

	if err := finishReceive(t, "A", stepA, resultWait); err != nil {
		t.Fatalf("A: commit: %v", err)
	}

	bErr := waitReceiveLocked(t, "B", stepB, resultWait)
	assertConflict(t, "B: receive once unblocked", bErr, gen.PURCHASEALREADYRECEIVED)
	if err := finishReceive(t, "B", stepB, resultWait); err != nil {
		t.Fatalf("B: rollback: %v", err)
	}

	if count := countMovements(ctx, t, pool, shop.ID, v1.ID, loc.ID, db.StockMovementKindPurchaseIn); count != 1 {
		t.Fatalf("v1 purchase_in movements = %d, want 1 (B's must not have written a second one)", count)
	}
	if count := countMovements(ctx, t, pool, shop.ID, v2.ID, loc.ID, db.StockMovementKindPurchaseIn); count != 1 {
		t.Fatalf("v2 purchase_in movements = %d, want 1 (B's must not have written a second one)", count)
	}
}
