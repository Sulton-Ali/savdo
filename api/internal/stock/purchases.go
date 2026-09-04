package stock

// This file: purchases, draft -> receive -> cancel
// (docs/03-ARCHITECTURE.md § "Receive a purchase"; docs/04-DATA-MODEL.md
// § 3; D-42/D-45/D-47/D-48). Requires stock.write (manager+,
// docs/05-API.md's endpoint table) on every route — the same permission
// every other write in this package gates on, and every role with it also
// has cost.read (auth.rolePermissions), so unitCost/totalCost are
// effectively manager+-only by the route gate alone; there is no
// cashier-reachable purchases code path to separately filter cost out of
// (mirrors ListStockMovements' own note on this).
//
// productName is resolved by the caller's locale (contracts/openapi.yaml's
// PurchaseItem, requested -> uz -> any, same as Product.name, ADR-012):
// catalog.AcceptLanguageFromContext/catalog.ResolveLocale (exported for
// exactly this, internal/catalog/locale.go) read the Accept-Language
// httpx.NewRouter's existing catalog.AcceptLanguageMiddleware already
// stashes on every request's context — catalog does not import stock, so
// this is not a cycle.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/audit"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/catalog"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/money"
	"github.com/Sulton-Ali/savdo/api/internal/pagination"
)

// purchaseItemInput is one PurchaseItemCreate, already validated and with
// its line_total computed server-side (hard rule 8), ready to insert.
type purchaseItemInput struct {
	variantID uuid.UUID
	qty       decimal.Decimal
	unitCost  decimal.Decimal
	lineTotal decimal.Decimal
}

// computeLineTotal is qty * unitCost rounded to money's 2 decimal places
// (ADR-007) — the same rounding money.String's callers already assume.
func computeLineTotal(qty, unitCost decimal.Decimal) decimal.Decimal {
	return qty.Mul(unitCost).Round(2)
}

// maxPurchaseItems bounds PurchaseCreate.items/PurchasePatch.items
// (contracts/openapi.yaml `maxItems: 200`, MINOR 8, T4 review) — enforced
// here too since the strict server does not check JSON Schema array
// bounds at runtime (the same reasoning as httpx.ValidateIdempotencyKey's
// own doc comment on maxLength).
const maxPurchaseItems = 200

// validatePurchaseItems checks every item of a PurchaseCreate/PurchasePatch
// against the shop: each variantId must belong to the shop (404 "variant",
// returned immediately — a different error class from the field-level 400s
// below, checked first per item so a client seeing 404 knows exactly which
// kind of problem it has), qty must be a positive decimal string (parseQty)
// and unitCost a non-negative decimal string (money.ParseAmount already
// rejects negative amounts) — field errors are collected across every item
// and reported together as one 400 VALIDATION_FAILED, `items[i].qty` /
// `items[i].unitCost` naming, matching catalog.prepareVariants' own
// `variants[i].<field>` convention. A variantId repeated across two items
// of the same purchase is also rejected (`items`: invalid, MINOR 3, T4
// review) — a purchase with two lines for the same variant is not a
// documented case any docs/04-DATA-MODEL.md § 3 read describes, and
// silently accepting it would let ReceivePurchaseTx write two separate
// purchase_in movements for what a caller almost certainly meant as one.
func (h *Handler) validatePurchaseItems(ctx context.Context, shopID uuid.UUID, in []gen.PurchaseItemCreate) ([]purchaseItemInput, error) {
	items := make([]purchaseItemInput, len(in))
	fields := map[string]string{}
	seenVariants := make(map[uuid.UUID]bool, len(in))
	for i, it := range in {
		if _, err := h.svc.q.GetVariantForStaff(ctx, db.GetVariantForStaffParams{ShopID: shopID, ID: it.VariantId}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, apierr.NotFound("variant")
			}
			return nil, fmt.Errorf("stock: get variant: %w", err)
		}

		if seenVariants[it.VariantId] {
			fields["items"] = "invalid"
		}
		seenVariants[it.VariantId] = true

		qty, ok := parseQty(it.Qty)
		if !ok || !qty.IsPositive() {
			fields[fmt.Sprintf("items[%d].qty", i)] = "invalid"
		}
		unitCost, apiErr := money.ParseAmount(it.UnitCost)
		if apiErr != nil {
			fields[fmt.Sprintf("items[%d].unitCost", i)] = "invalid"
		}

		items[i] = purchaseItemInput{variantID: it.VariantId, qty: qty, unitCost: unitCost, lineTotal: computeLineTotal(qty, unitCost)}
	}
	if len(fields) > 0 {
		return nil, apierr.Validation(fields)
	}
	return items, nil
}

// requestLocaleFor resolves the locale a purchase response's productName
// fields render in: the caller's `Accept-Language` (catalog.ResolveLocale,
// requested -> uz -> any) falling back to shopID's own default_locale,
// exactly like catalog's own resolveLocale — see this file's own doc
// comment.
func requestLocaleFor(ctx context.Context, q *db.Queries, shopID uuid.UUID) (string, error) {
	shopRow, err := q.GetShop(ctx, shopID)
	if err != nil {
		return "", fmt.Errorf("stock: get shop: %w", err)
	}
	return catalog.ResolveLocale(ctx, shopRow.DefaultLocale), nil
}

// purchaseItemsAndTotals loads purchaseID's items (resolved to
// productName/variantLabel/sku via ListPurchaseItemsWithLabels) and each
// one's line_total, given defs already fetched by the caller — split out
// of loadPurchaseWithItems (NIT 10, T4 review) so ListPurchases can fetch
// the shop's attribute definitions once for the whole page instead of once
// per purchase.
func purchaseItemsAndTotals(ctx context.Context, q *db.Queries, shopID uuid.UUID, locale string, purchaseID uuid.UUID, defs []db.ListAttributeDefinitionsRow) ([]gen.PurchaseItem, []decimal.Decimal, error) {
	rows, err := q.ListPurchaseItemsWithLabels(ctx, db.ListPurchaseItemsWithLabelsParams{Locale: locale, ShopID: shopID, PurchaseID: purchaseID})
	if err != nil {
		return nil, nil, fmt.Errorf("stock: list purchase items with labels: %w", err)
	}

	items := make([]gen.PurchaseItem, len(rows))
	totals := make([]decimal.Decimal, len(rows))
	for i, r := range rows {
		label, err := variantLabel(r.VariantSku, r.VariantID, r.VariantAttributes, defs)
		if err != nil {
			return nil, nil, err
		}
		g, err := toGenPurchaseItem(r, label)
		if err != nil {
			return nil, nil, err
		}
		items[i] = g

		lineTotal, err := money.FromNumeric(r.LineTotal)
		if err != nil {
			return nil, nil, fmt.Errorf("stock: purchase item line total: %w", err)
		}
		totals[i] = lineTotal
	}
	return items, totals, nil
}

// loadPurchaseWithItems builds the full wire Purchase for p: its items and
// totalCost (summed from them — toGenPurchase's own doc comment on why the
// stored column is never trusted here), fetching the shop's attribute
// definitions itself — the single-purchase case (Get/Create/Update/
// Receive/Cancel all return exactly one Purchase, so one extra query per
// call is the right trade-off; ListPurchases fetches defs once for the
// whole page and calls purchaseItemsAndTotals directly instead, see its
// own doc comment). q is the caller's *db.Queries — h.svc.q for a plain
// read (after its own transaction already committed) or a qtx still
// inside the caller's transaction (ReceivePurchaseTx).
func (h *Handler) loadPurchaseWithItems(ctx context.Context, q *db.Queries, shopID uuid.UUID, locale string, p db.Purchase) (gen.Purchase, error) {
	defs, err := q.ListAttributeDefinitions(ctx, db.ListAttributeDefinitionsParams{Locale: locale, ShopID: shopID})
	if err != nil {
		return gen.Purchase{}, fmt.Errorf("stock: list attribute definitions: %w", err)
	}
	items, totals, err := purchaseItemsAndTotals(ctx, q, shopID, locale, p.ID, defs)
	if err != nil {
		return gen.Purchase{}, err
	}
	return toGenPurchase(p, items, totals), nil
}

// CreatePurchase creates a draft purchase. Requires stock.write
// (manager+). supplierId/locationId must belong to the shop (404
// otherwise); items must be non-empty and each pass validatePurchaseItems.
// number is server-generated (shops.next_purchase_number under row lock,
// D-45); line_total/totalCost are computed server-side (hard rule 8), in
// one transaction with the item inserts.
func (h *Handler) CreatePurchase(ctx context.Context, req gen.CreatePurchaseRequestObject) (gen.CreatePurchaseResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermStockWrite); err != nil {
		return nil, err
	}
	body := req.Body

	if _, err := h.svc.q.GetSupplier(ctx, db.GetSupplierParams{ShopID: authCtx.ShopID, ID: body.SupplierId}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("supplier")
		}
		return nil, fmt.Errorf("stock: get supplier: %w", err)
	}
	if _, err := h.svc.q.GetLocation(ctx, db.GetLocationParams{ShopID: authCtx.ShopID, ID: body.LocationId}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("location")
		}
		return nil, fmt.Errorf("stock: get location: %w", err)
	}
	if len(body.Items) == 0 {
		return nil, apierr.Validation(map[string]string{"items": "required"})
	}
	if len(body.Items) > maxPurchaseItems {
		return nil, apierr.Validation(map[string]string{"items": "too_long"})
	}
	items, err := h.validatePurchaseItems(ctx, authCtx.ShopID, body.Items)
	if err != nil {
		return nil, err
	}

	tx, err := h.svc.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("stock: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := h.svc.q.WithTx(tx)

	seq, err := qtx.NextPurchaseNumber(ctx, authCtx.ShopID)
	if err != nil {
		return nil, fmt.Errorf("stock: next purchase number: %w", err)
	}
	number := fmt.Sprintf("P-%06d", seq)

	created, err := qtx.CreatePurchase(ctx, db.CreatePurchaseParams{
		ID: newID(), ShopID: authCtx.ShopID, SupplierID: body.SupplierId, LocationID: body.LocationId,
		Number: number, SupplierInvoiceNo: body.SupplierInvoiceNo, Note: body.Note, CreatedBy: &authCtx.UserID,
	})
	if err != nil {
		return nil, fmt.Errorf("stock: create purchase: %w", err)
	}

	for _, it := range items {
		if _, err := qtx.CreatePurchaseItem(ctx, db.CreatePurchaseItemParams{
			ID: newID(), ShopID: authCtx.ShopID, PurchaseID: created.ID, VariantID: it.variantID,
			Qty: money.ToNumeric(it.qty), UnitCost: money.ToNumeric(it.unitCost), LineTotal: money.ToNumeric(it.lineTotal),
		}); err != nil {
			if apiErr, ok := mapOutOfRange(err); ok {
				return nil, apiErr
			}
			return nil, fmt.Errorf("stock: create purchase item: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("stock: commit create purchase: %w", err)
	}

	locale, err := requestLocaleFor(ctx, h.svc.q, authCtx.ShopID)
	if err != nil {
		return nil, err
	}
	resp, err := h.loadPurchaseWithItems(ctx, h.svc.q, authCtx.ShopID, locale, created)
	if err != nil {
		return nil, err
	}
	return gen.CreatePurchase201JSONResponse(resp), nil
}

// GetPurchase gets a purchase by id. Requires stock.write (manager+). 404
// for another shop's id.
func (h *Handler) GetPurchase(ctx context.Context, req gen.GetPurchaseRequestObject) (gen.GetPurchaseResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermStockWrite); err != nil {
		return nil, err
	}

	p, err := h.svc.q.GetPurchase(ctx, db.GetPurchaseParams{ShopID: authCtx.ShopID, ID: req.Id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("purchase")
		}
		return nil, fmt.Errorf("stock: get purchase: %w", err)
	}
	locale, err := requestLocaleFor(ctx, h.svc.q, authCtx.ShopID)
	if err != nil {
		return nil, err
	}
	resp, err := h.loadPurchaseWithItems(ctx, h.svc.q, authCtx.ShopID, locale, p)
	if err != nil {
		return nil, err
	}
	return gen.GetPurchase200JSONResponse(resp), nil
}

// ListPurchases lists the shop's purchases. Requires stock.write
// (manager+). Cursor-paginated (created_at, id keyset, internal/pagination);
// status/supplierId are optional exact-match filters.
func (h *Handler) ListPurchases(ctx context.Context, req gen.ListPurchasesRequestObject) (gen.ListPurchasesResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermStockWrite); err != nil {
		return nil, err
	}

	limit := clampLimit(req.Params.Limit)
	cCreatedAt, cID, err := decodeMovementCursor(req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	cursorCreatedAt, cursorID := movementCursorPtr(cCreatedAt, cID)

	var status *db.PurchaseStatus
	if req.Params.Status != nil {
		s := db.PurchaseStatus(*req.Params.Status)
		status = &s
	}

	rows, err := h.svc.q.ListPurchases(ctx, db.ListPurchasesParams{
		ShopID: authCtx.ShopID, Status: status, SupplierID: req.Params.SupplierId,
		CursorCreatedAt: cursorCreatedAt, CursorID: cursorID, Limit: limit + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("stock: list purchases: %w", err)
	}

	items, nextCursor := paginatePurchases(rows, limit)
	locale, err := requestLocaleFor(ctx, h.svc.q, authCtx.ShopID)
	if err != nil {
		return nil, err
	}
	// Fetched once for the whole page, not once per purchase (NIT 10, T4
	// review) — every item on every purchase resolves its variantLabel
	// against the same shop-wide attribute-definition order regardless of
	// which purchase it belongs to.
	defs, err := h.svc.q.ListAttributeDefinitions(ctx, db.ListAttributeDefinitionsParams{Locale: locale, ShopID: authCtx.ShopID})
	if err != nil {
		return nil, fmt.Errorf("stock: list attribute definitions: %w", err)
	}
	genItems := make([]gen.Purchase, len(items))
	for i, r := range items {
		purchaseItems, totals, err := purchaseItemsAndTotals(ctx, h.svc.q, authCtx.ShopID, locale, r.ID, defs)
		if err != nil {
			return nil, err
		}
		genItems[i] = toGenPurchase(r, purchaseItems, totals)
	}
	return gen.ListPurchases200JSONResponse(gen.PurchaseList{Items: genItems, NextCursor: nullableString(nextCursor)}), nil
}

// UpdatePurchase updates a draft purchase. Requires stock.write
// (manager+). Only while status: draft (409 PURCHASE_NOT_DRAFT
// otherwise); supplierInvoiceNo/note are D-35 nullable; items, when
// present, replaces the full item list (delete + insert, recomputing
// totals) in the same transaction that locks and re-checks the purchase's
// status.
func (h *Handler) UpdatePurchase(ctx context.Context, req gen.UpdatePurchaseRequestObject) (gen.UpdatePurchaseResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermStockWrite); err != nil {
		return nil, err
	}
	body := req.Body

	if body.SupplierId != nil {
		if _, err := h.svc.q.GetSupplier(ctx, db.GetSupplierParams{ShopID: authCtx.ShopID, ID: *body.SupplierId}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, apierr.NotFound("supplier")
			}
			return nil, fmt.Errorf("stock: get supplier: %w", err)
		}
	}
	if body.LocationId != nil {
		if _, err := h.svc.q.GetLocation(ctx, db.GetLocationParams{ShopID: authCtx.ShopID, ID: *body.LocationId}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, apierr.NotFound("location")
			}
			return nil, fmt.Errorf("stock: get location: %w", err)
		}
	}

	replaceItems := body.Items != nil
	var newItems []purchaseItemInput
	if replaceItems {
		if len(*body.Items) == 0 {
			return nil, apierr.Validation(map[string]string{"items": "required"})
		}
		if len(*body.Items) > maxPurchaseItems {
			return nil, apierr.Validation(map[string]string{"items": "too_long"})
		}
		var err error
		newItems, err = h.validatePurchaseItems(ctx, authCtx.ShopID, *body.Items)
		if err != nil {
			return nil, err
		}
	}

	supplierInvoiceNo := optionalString(body.SupplierInvoiceNo)
	note := optionalString(body.Note)

	tx, err := h.svc.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("stock: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := h.svc.q.WithTx(tx)

	current, err := qtx.GetPurchaseForUpdate(ctx, db.GetPurchaseForUpdateParams{ShopID: authCtx.ShopID, ID: req.Id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("purchase")
		}
		return nil, fmt.Errorf("stock: get purchase for update: %w", err)
	}
	if current.Status != db.PurchaseStatusDraft {
		return nil, errPurchaseNotDraft
	}

	params := db.UpdatePurchaseHeaderParams{SupplierID: body.SupplierId, LocationID: body.LocationId, ShopID: authCtx.ShopID, ID: req.Id}
	if supplierInvoiceNo != nil {
		params.ClearSupplierInvoiceNo = *supplierInvoiceNo == nil
		params.SupplierInvoiceNo = *supplierInvoiceNo
	}
	if note != nil {
		params.ClearNote = *note == nil
		params.Note = *note
	}
	updated, err := qtx.UpdatePurchaseHeader(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("stock: update purchase header: %w", err)
	}

	if replaceItems {
		if _, err := qtx.DeletePurchaseItems(ctx, db.DeletePurchaseItemsParams{ShopID: authCtx.ShopID, PurchaseID: req.Id}); err != nil {
			return nil, fmt.Errorf("stock: delete purchase items: %w", err)
		}
		for _, it := range newItems {
			if _, err := qtx.CreatePurchaseItem(ctx, db.CreatePurchaseItemParams{
				ID: newID(), ShopID: authCtx.ShopID, PurchaseID: req.Id, VariantID: it.variantID,
				Qty: money.ToNumeric(it.qty), UnitCost: money.ToNumeric(it.unitCost), LineTotal: money.ToNumeric(it.lineTotal),
			}); err != nil {
				if apiErr, ok := mapOutOfRange(err); ok {
					return nil, apiErr
				}
				return nil, fmt.Errorf("stock: create purchase item: %w", err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("stock: commit update purchase: %w", err)
	}

	locale, err := requestLocaleFor(ctx, h.svc.q, authCtx.ShopID)
	if err != nil {
		return nil, err
	}
	resp, err := h.loadPurchaseWithItems(ctx, h.svc.q, authCtx.ShopID, locale, updated)
	if err != nil {
		return nil, err
	}
	return gen.UpdatePurchase200JSONResponse(resp), nil
}

// purchaseAuditItem is one line audit.Write's after payload carries for a
// receive or a received-purchase cancel (D-47).
type purchaseAuditItem struct {
	VariantID uuid.UUID `json:"variantId"`
	Qty       string    `json:"qty"`
	UnitCost  string    `json:"unitCost"`
}

// purchaseAuditBefore/purchaseAuditAfter are audit_log.before/after's
// shape for purchase.receive and purchase.cancel.
type purchaseAuditBefore struct {
	Status string `json:"status"`
}
type purchaseAuditAfter struct {
	Status    string              `json:"status"`
	TotalCost string              `json:"totalCost,omitempty"`
	Items     []purchaseAuditItem `json:"items,omitempty"`
}

// sortedPurchaseItems returns items sorted by variant_id ascending — the
// MoveParams doc comment's multi-line-caller rule (move.go): a purchase's
// items all share one location_id, so sorting by (variant_id, location_id)
// reduces to sorting by variant_id alone.
func sortedPurchaseItems(items []db.PurchaseItem) []db.PurchaseItem {
	sorted := make([]db.PurchaseItem, len(items))
	copy(sorted, items)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].VariantID.String() < sorted[j].VariantID.String() })
	return sorted
}

// ReceivePurchaseTx receives a draft purchase (draft -> received): locks
// the purchase row (GetPurchaseForUpdate), requires status: draft (409
// PURCHASE_ALREADY_RECEIVED/PURCHASE_ALREADY_CANCELLED otherwise), writes
// one purchase_in movement per item (sorted per Move's deadlock-avoidance
// rule) with ref_type "purchase", sets the received variant's
// cost_override to the line unit_cost when shops.update_cost_on_purchase
// is on (D-42/D-48), computes total_cost from the items and writes it via
// SetPurchaseReceived, and writes a purchase.receive audit_log row (D-47).
// qtx must already be bound to the caller's transaction — httpx.Idempotent
// runs this the same way stock.Handler.CreateAdjustmentTx runs inside it
// (internal/httpx cannot be imported here without a cycle, per stock's own
// package doc comment). Requires stock.write (manager+).
func (h *Handler) ReceivePurchaseTx(ctx context.Context, qtx *db.Queries, id uuid.UUID) (gen.Purchase, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return gen.Purchase{}, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermStockWrite); err != nil {
		return gen.Purchase{}, err
	}

	current, err := qtx.GetPurchaseForUpdate(ctx, db.GetPurchaseForUpdateParams{ShopID: authCtx.ShopID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.Purchase{}, apierr.NotFound("purchase")
		}
		return gen.Purchase{}, fmt.Errorf("stock: get purchase for update: %w", err)
	}
	switch current.Status {
	case db.PurchaseStatusReceived:
		return gen.Purchase{}, errPurchaseAlreadyReceived
	case db.PurchaseStatusCancelled:
		return gen.Purchase{}, errPurchaseAlreadyCancelled
	}

	rawItems, err := qtx.ListPurchaseItems(ctx, db.ListPurchaseItemsParams{ShopID: authCtx.ShopID, PurchaseID: id})
	if err != nil {
		return gen.Purchase{}, fmt.Errorf("stock: list purchase items: %w", err)
	}
	shopRow, err := qtx.GetShop(ctx, authCtx.ShopID)
	if err != nil {
		return gen.Purchase{}, fmt.Errorf("stock: get shop: %w", err)
	}

	total := decimal.Zero
	auditItems := make([]purchaseAuditItem, 0, len(rawItems))
	refType := purchaseRefType
	for _, it := range sortedPurchaseItems(rawItems) {
		qty, err := money.FromNumeric(it.Qty)
		if err != nil {
			return gen.Purchase{}, fmt.Errorf("stock: purchase item qty: %w", err)
		}
		unitCost, err := money.FromNumeric(it.UnitCost)
		if err != nil {
			return gen.Purchase{}, fmt.Errorf("stock: purchase item unit cost: %w", err)
		}
		lineTotal, err := money.FromNumeric(it.LineTotal)
		if err != nil {
			return gen.Purchase{}, fmt.Errorf("stock: purchase item line total: %w", err)
		}
		total = total.Add(lineTotal)

		if _, err := Move(ctx, qtx, MoveParams{
			ShopID: authCtx.ShopID, VariantID: it.VariantID, LocationID: current.LocationID,
			Kind: db.StockMovementKindPurchaseIn, Qty: qty, UnitCost: &unitCost,
			RefType: &refType, RefID: &current.ID, ActorID: &authCtx.UserID,
		}); err != nil {
			return gen.Purchase{}, mapMoveError(err)
		}

		if shopRow.UpdateCostOnPurchase {
			if _, err := qtx.UpdateVariant(ctx, db.UpdateVariantParams{
				CostOverride: optionalNumeric(&unitCost), ShopID: authCtx.ShopID, ID: it.VariantID,
			}); err != nil {
				return gen.Purchase{}, fmt.Errorf("stock: update variant cost_override: %w", err)
			}
		}

		auditItems = append(auditItems, purchaseAuditItem{VariantID: it.VariantID, Qty: qtyString(qty), UnitCost: money.String(unitCost)})
	}

	received, err := qtx.SetPurchaseReceived(ctx, db.SetPurchaseReceivedParams{ShopID: authCtx.ShopID, ID: id, TotalCost: money.ToNumeric(total)})
	if err != nil {
		if apiErr, ok := mapOutOfRange(err); ok {
			return gen.Purchase{}, apiErr
		}
		return gen.Purchase{}, fmt.Errorf("stock: set purchase received: %w", err)
	}

	before, err := json.Marshal(purchaseAuditBefore{Status: string(current.Status)})
	if err != nil {
		return gen.Purchase{}, fmt.Errorf("stock: marshal audit before: %w", err)
	}
	after, err := json.Marshal(purchaseAuditAfter{Status: string(received.Status), TotalCost: money.String(total), Items: auditItems})
	if err != nil {
		return gen.Purchase{}, fmt.Errorf("stock: marshal audit after: %w", err)
	}
	if err := audit.Write(ctx, qtx, audit.Entry{
		ShopID: authCtx.ShopID, ActorID: authCtx.UserID, Action: "purchase.receive",
		EntityType: "purchase", EntityID: id, Before: before, After: after,
	}); err != nil {
		return gen.Purchase{}, fmt.Errorf("stock: write audit: %w", err)
	}

	resp, err := h.loadPurchaseWithItems(ctx, qtx, authCtx.ShopID, catalog.ResolveLocale(ctx, shopRow.DefaultLocale), received)
	if err != nil {
		return gen.Purchase{}, err
	}
	return resp, nil
}

// CancelPurchase cancels a purchase. Requires stock.write (manager+).
// From draft: a plain status change, no movements. From received: one
// reversing movement per item — kind purchase_in, negative qty
// (-line qty), ref_type "purchase_cancel" (there is no separate enum
// value for a cancellation, ADR-006; see errors.go's own doc comment) —
// which fails with 409 STOCK_INSUFFICIENT if the received stock was
// already sold/moved below what the reversal needs (D-41) and rolls back
// the whole transaction, including the status change. From cancelled:
// 409 PURCHASE_ALREADY_CANCELLED. cost_override is never reverted on
// cancel (per the pre-emptive ruling on this task's cancel-of-received
// ambiguity). Writes a purchase.cancel audit_log row (D-47). Not
// idempotency-wrapped (no Idempotency-Key in the contract, unlike
// ReceivePurchase — docs/05-API.md § Conventions).
func (h *Handler) CancelPurchase(ctx context.Context, req gen.CancelPurchaseRequestObject) (gen.CancelPurchaseResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermStockWrite); err != nil {
		return nil, err
	}

	tx, err := h.svc.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("stock: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := h.svc.q.WithTx(tx)

	current, err := qtx.GetPurchaseForUpdate(ctx, db.GetPurchaseForUpdateParams{ShopID: authCtx.ShopID, ID: req.Id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("purchase")
		}
		return nil, fmt.Errorf("stock: get purchase for update: %w", err)
	}
	if current.Status == db.PurchaseStatusCancelled {
		return nil, errPurchaseAlreadyCancelled
	}

	auditItems := []purchaseAuditItem{}
	if current.Status == db.PurchaseStatusReceived {
		rawItems, err := qtx.ListPurchaseItems(ctx, db.ListPurchaseItemsParams{ShopID: authCtx.ShopID, PurchaseID: req.Id})
		if err != nil {
			return nil, fmt.Errorf("stock: list purchase items: %w", err)
		}
		refType := purchaseCancelRefType
		for _, it := range sortedPurchaseItems(rawItems) {
			qty, err := money.FromNumeric(it.Qty)
			if err != nil {
				return nil, fmt.Errorf("stock: purchase item qty: %w", err)
			}
			unitCost, err := money.FromNumeric(it.UnitCost)
			if err != nil {
				return nil, fmt.Errorf("stock: purchase item unit cost: %w", err)
			}
			if _, err := Move(ctx, qtx, MoveParams{
				ShopID: authCtx.ShopID, VariantID: it.VariantID, LocationID: current.LocationID,
				Kind: db.StockMovementKindPurchaseIn, Qty: qty.Neg(), UnitCost: &unitCost,
				RefType: &refType, RefID: &current.ID, ActorID: &authCtx.UserID,
			}); err != nil {
				return nil, mapMoveError(err)
			}
			auditItems = append(auditItems, purchaseAuditItem{VariantID: it.VariantID, Qty: qtyString(qty.Neg()), UnitCost: money.String(unitCost)})
		}
	}

	cancelled, err := qtx.SetPurchaseCancelled(ctx, db.SetPurchaseCancelledParams{ShopID: authCtx.ShopID, ID: req.Id})
	if err != nil {
		return nil, fmt.Errorf("stock: set purchase cancelled: %w", err)
	}

	before, err := json.Marshal(purchaseAuditBefore{Status: string(current.Status)})
	if err != nil {
		return nil, fmt.Errorf("stock: marshal audit before: %w", err)
	}
	after, err := json.Marshal(purchaseAuditAfter{Status: string(cancelled.Status), Items: auditItems})
	if err != nil {
		return nil, fmt.Errorf("stock: marshal audit after: %w", err)
	}
	if err := audit.Write(ctx, qtx, audit.Entry{
		ShopID: authCtx.ShopID, ActorID: authCtx.UserID, Action: "purchase.cancel",
		EntityType: "purchase", EntityID: req.Id, Before: before, After: after,
	}); err != nil {
		return nil, fmt.Errorf("stock: write audit: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		if isDeadlock(err) {
			return nil, errDeadlock
		}
		return nil, fmt.Errorf("stock: commit cancel purchase: %w", err)
	}

	locale, err := requestLocaleFor(ctx, h.svc.q, authCtx.ShopID)
	if err != nil {
		return nil, err
	}
	resp, err := h.loadPurchaseWithItems(ctx, h.svc.q, authCtx.ShopID, locale, cancelled)
	if err != nil {
		return nil, err
	}
	return gen.CancelPurchase200JSONResponse(resp), nil
}

// paginatePurchases trims rows (fetched with limit+1) down to at most
// limit items and reports the opaque cursor for the next page — mirrors
// paginateMovements, specialized to db.Purchase's own (created_at, id).
func paginatePurchases(rows []db.Purchase, limit int32) ([]db.Purchase, *string) {
	if len(rows) <= int(limit) {
		return rows, nil
	}
	items := rows[:limit]
	last := items[len(items)-1]
	cursor := pagination.Encode(last.CreatedAt, last.ID)
	return items, &cursor
}
