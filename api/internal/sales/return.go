package sales

// This file: POST /sales/{id}/return (docs/04-DATA-MODEL.md § 4 Rules
// D-56..D-69, especially D-58/D-61/D-64/D-66; ADR-006/ADR-007/ADR-013/
// ADR-014). CreateSaleReturnTx runs entirely on the transaction
// httpx.Idempotent opens and hands it as qtx — httpx/sales.go's own doc
// comment on CreateSaleReturn explains why (the same cycle-avoidance
// reasoning as CreateSaleTx's).

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
	"github.com/Sulton-Ali/savdo/api/internal/stock"
)

// saleReturnRefType is the stock_movements.ref_type CreateSaleReturnTx
// writes on every return_in movement — mirrors saleRefType/
// saleVoidRefType.
const saleReturnRefType = "sale_return"

// returnAuditItem/returnAuditAfter are the sale.return audit_log row's
// after JSON shape (D-47); a return is a brand-new sale, so there is no
// "before" to record (mirrors purchase.receive's own audit shape,
// stock/purchases.go).
type returnAuditItem struct {
	OriginalSaleItemID uuid.UUID `json:"originalSaleItemId"`
	VariantID          uuid.UUID `json:"variantId"`
	Qty                string    `json:"qty"`
	Refund             string    `json:"refund"`
}

type returnAuditAfter struct {
	OriginalSaleID uuid.UUID         `json:"originalSaleId"`
	Total          string            `json:"total"`
	Items          []returnAuditItem `json:"items"`
}

// lineDiscountShares splits the original sale's discount_amount across
// items proportionally (D-64): share = round(line_total * discount_amount
// / subtotal, 2) for every line except the one with the greatest
// sale_items.id, which takes discount_amount minus the sum of the
// others' shares — so shares always sum exactly to discount_amount
// regardless of rounding, and the same allocation feeds D-61's return
// refund. items need not be pre-sorted; the "last" line is found by
// comparing ids directly (uuid.UUID.String() comparison, the same
// ordering sortedSaleLines already relies on for a v7 id's timestamp
// prefix). subtotal zero never happens for a real sale (every completed
// sale has at least one line with a positive line_total, checked by
// CreateSaleTx), but is handled defensively by giving every line a zero
// share rather than dividing by zero.
func lineDiscountShares(items []db.SaleItem, subtotal, discountAmount decimal.Decimal) (map[uuid.UUID]decimal.Decimal, error) {
	shares := make(map[uuid.UUID]decimal.Decimal, len(items))
	if len(items) == 0 {
		return shares, nil
	}
	if subtotal.IsZero() {
		for _, it := range items {
			shares[it.ID] = decimal.Zero
		}
		return shares, nil
	}

	lastID := items[0].ID
	for _, it := range items {
		if it.ID.String() > lastID.String() {
			lastID = it.ID
		}
	}

	sum := decimal.Zero
	for _, it := range items {
		if it.ID == lastID {
			continue
		}
		lineTotal, err := money.FromNumeric(it.LineTotal)
		if err != nil {
			return nil, fmt.Errorf("sales: original line total: %w", err)
		}
		share := lineTotal.Mul(discountAmount).Div(subtotal).Round(2)
		shares[it.ID] = share
		sum = sum.Add(share)
	}
	shares[lastID] = discountAmount.Sub(sum)
	return shares, nil
}

// returnLine is one validated, priced SaleReturnItemCreate: the original
// sale_items row it refunds (already locked by GetSaleItemsForUpdate),
// the quantity being returned now, and the refund amount computeReturnLines
// resolved for it (D-61/D-64).
type returnLine struct {
	original db.SaleItem
	qty      decimal.Decimal
	refund   decimal.Decimal
}

// resolveReturnLines validates every SaleReturnItemCreate against the
// original sale's already-locked items (byID) — same order as
// resolveSaleItems (create.go): 400 field errors are collected across
// every item and reported together (`items[i].saleItemId`/
// `items[i].qty`), a saleItemId repeated across two items is rejected the
// same way a repeated variantId is on a sale — then, once every line
// shapes out, computes each one's refund per D-61/D-64 against shares
// (this original sale's per-line discount share) and whatever that line
// has already had refunded (SumReturnedForSaleItem, run only after
// GetSaleItemsForUpdate's own lock makes a concurrent second return
// against the same line block until this transaction ends — its own doc
// comment). A line whose requested qty plus already-returned qty would
// exceed what was sold answers 409 RETURN_EXCEEDS_SOLD before any refund
// is computed for it.
func resolveReturnLines(ctx context.Context, qtx *db.Queries, shopID uuid.UUID, in []gen.SaleReturnItemCreate, byID map[uuid.UUID]db.SaleItem, shares map[uuid.UUID]decimal.Decimal) ([]returnLine, error) {
	lines := make([]returnLine, len(in))
	fields := map[string]string{}
	seen := make(map[uuid.UUID]bool, len(in))
	for i, it := range in {
		orig, ok := byID[it.SaleItemId]
		if !ok {
			fields[fmt.Sprintf("items[%d].saleItemId", i)] = "invalid"
			continue
		}
		if seen[it.SaleItemId] {
			fields[fmt.Sprintf("items[%d].saleItemId", i)] = "invalid"
			continue
		}
		seen[it.SaleItemId] = true

		qty, ok := parseQty(it.Qty)
		if !ok || !qty.IsPositive() {
			fields[fmt.Sprintf("items[%d].qty", i)] = "invalid"
			continue
		}
		lines[i] = returnLine{original: orig, qty: qty}
	}
	if len(fields) > 0 {
		return nil, apierr.Validation(fields)
	}

	for i := range lines {
		orig := lines[i].original
		soldQty, err := money.FromNumeric(orig.Qty)
		if err != nil {
			return nil, fmt.Errorf("sales: original item qty: %w", err)
		}
		lineTotal, err := money.FromNumeric(orig.LineTotal)
		if err != nil {
			return nil, fmt.Errorf("sales: original item line total: %w", err)
		}
		sums, err := qtx.SumReturnedForSaleItem(ctx, db.SumReturnedForSaleItemParams{ShopID: shopID, OriginalSaleItemID: &orig.ID})
		if err != nil {
			return nil, fmt.Errorf("sales: sum returned for sale item: %w", err)
		}
		alreadyQty, err := money.FromNumeric(sums.ReturnedQty)
		if err != nil {
			return nil, fmt.Errorf("sales: already returned qty: %w", err)
		}
		alreadyAmount, err := money.FromNumeric(sums.ReturnedAmount)
		if err != nil {
			return nil, fmt.Errorf("sales: already returned amount: %w", err)
		}

		newQty := alreadyQty.Add(lines[i].qty)
		if newQty.GreaterThan(soldQty) {
			return nil, errReturnExceedsSold(i, orig.ID)
		}

		share, ok := shares[orig.ID]
		if !ok {
			return nil, fmt.Errorf("sales: no discount share resolved for original item %s", orig.ID)
		}
		net := lineTotal.Sub(share)

		var refund decimal.Decimal
		if newQty.Equal(soldQty) {
			// Fully returned across this and every earlier completed
			// return of this line: the exact remainder, never a rounded
			// approximation, so the sum of every return's refund for this
			// line lands on net exactly (D-61).
			refund = net.Sub(alreadyAmount)
		} else {
			refund = net.Mul(lines[i].qty).Div(soldQty).Round(2)
			if alreadyAmount.Add(refund).GreaterThan(net) {
				refund = net.Sub(alreadyAmount)
			}
		}
		lines[i].refund = refund
	}
	return lines, nil
}

// sortedReturnLines returns lines sorted by original variant_id ascending
// — mirrors sortedSaleLines/sortedSaleItemsByVariant: a return shares the
// original sale's single location_id, so sorting by (variant_id,
// location_id) reduces to sorting by variant_id alone.
func sortedReturnLines(lines []returnLine) []returnLine {
	sorted := make([]returnLine, len(lines))
	copy(sorted, lines)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].original.VariantID.String() < sorted[j].original.VariantID.String()
	})
	return sorted
}

// CreateSaleReturnTx completes a partial or full return against
// originalSaleID: locks the original sale (GetSaleForUpdate) and its
// items (GetSaleItemsForUpdate) before any check (what makes
// RETURN_EXCEEDS_SOLD race-free under a concurrent second return of the
// same line — resolveReturnLines' own doc comment); 404 "sale" when the
// original does not resolve in the actor's shop; 409 SALE_NOT_RETURNABLE
// when it is itself a return (D-66: a return of a return); 409
// SALE_ALREADY_VOIDED when it is voided. Every requested
// line is priced per D-61/D-64 (resolveReturnLines) and written sorted by
// variant_id (sortedReturnLines) as one return_in movement each (ref_type
// "sale_return", ref_id the new return sale's id, unit_cost copied from
// the original line's own frozen unit_cost — D-64's margin math reads
// sale_items.unit_cost, same as every other mover). The next sale number
// is taken (NextSaleNumber, locking the shop row) after every check has
// passed and before any stock row is touched, the same ordering
// CreateSaleTx's own doc comment explains. Writes a sale.return audit_log
// row (D-47) and responds with the new kind:"return" sale (cost-gated
// like GetSale/CreateSaleTx). Requires sales.void (owner/manager, D-58's
// "manager+" — returns share CreateSale/VoidSale's own permission,
// docs/04-DATA-MODEL.md § 7). qtx must already be bound to the caller's
// own transaction (httpx.Idempotent's own doc comment; httpx/sales.go's
// CreateSaleReturn wraps this the same way it wraps CreateSaleTx).
func (h *Handler) CreateSaleReturnTx(ctx context.Context, qtx *db.Queries, originalSaleID uuid.UUID, body *gen.SaleReturnCreate) (gen.Sale, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return gen.Sale{}, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermSalesVoid); err != nil {
		return gen.Sale{}, err
	}

	if len(body.Items) == 0 {
		return gen.Sale{}, apierr.Validation(map[string]string{"items": "required"})
	}
	if len(body.Items) > maxSaleItems {
		return gen.Sale{}, apierr.Validation(map[string]string{"items": "too_long"})
	}

	original, err := qtx.GetSaleForUpdate(ctx, db.GetSaleForUpdateParams{ShopID: authCtx.ShopID, ID: originalSaleID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.Sale{}, apierr.NotFound("sale")
		}
		return gen.Sale{}, fmt.Errorf("sales: get sale for update: %w", err)
	}
	originalItems, err := qtx.GetSaleItemsForUpdate(ctx, db.GetSaleItemsForUpdateParams{ShopID: authCtx.ShopID, SaleID: originalSaleID})
	if err != nil {
		return gen.Sale{}, fmt.Errorf("sales: get sale items for update: %w", err)
	}

	if original.Kind != db.SaleKindSale {
		return gen.Sale{}, errSaleNotReturnable
	}
	if original.Status == db.SaleStatusVoided {
		return gen.Sale{}, errSaleAlreadyVoided
	}

	subtotal, err := money.FromNumeric(original.Subtotal)
	if err != nil {
		return gen.Sale{}, fmt.Errorf("sales: original subtotal: %w", err)
	}
	discountAmount, err := money.FromNumeric(original.DiscountAmount)
	if err != nil {
		return gen.Sale{}, fmt.Errorf("sales: original discount amount: %w", err)
	}
	shares, err := lineDiscountShares(originalItems, subtotal, discountAmount)
	if err != nil {
		return gen.Sale{}, err
	}
	byID := make(map[uuid.UUID]db.SaleItem, len(originalItems))
	for _, it := range originalItems {
		byID[it.ID] = it
	}

	lines, err := resolveReturnLines(ctx, qtx, authCtx.ShopID, body.Items, byID, shares)
	if err != nil {
		return gen.Sale{}, err
	}

	total := decimal.Zero
	for _, l := range lines {
		total = total.Add(l.refund)
	}

	// The original sale's payment method — GetSaleForUpdate's plain
	// `SELECT * FROM sales` has no payment column (that join lives only in
	// the read-shaped GetSaleForStaff/GetSaleForCashier, D-63's own doc
	// comment); the row is already locked above, so this extra read is
	// merely fetching a column that lock already made stable.
	paymentRow, err := qtx.GetSaleForStaff(ctx, db.GetSaleForStaffParams{ShopID: authCtx.ShopID, ID: originalSaleID})
	if err != nil {
		return gen.Sale{}, fmt.Errorf("sales: get sale for staff (payment method): %w", err)
	}
	if paymentRow.PaymentMethod == nil {
		return gen.Sale{}, fmt.Errorf("sales: original sale %s has no payment row", originalSaleID)
	}

	number, err := qtx.NextSaleNumber(ctx, authCtx.ShopID)
	if err != nil {
		return gen.Sale{}, fmt.Errorf("sales: next sale number: %w", err)
	}

	returnID := newID()
	refType := saleReturnRefType
	auditItems := make([]returnAuditItem, 0, len(lines))
	for _, l := range sortedReturnLines(lines) {
		if _, err := stock.Move(ctx, qtx, stock.MoveParams{
			ShopID: authCtx.ShopID, VariantID: l.original.VariantID, LocationID: original.LocationID,
			Kind: db.StockMovementKindReturnIn, Qty: l.qty,
			RefType: &refType, RefID: &returnID, ActorID: &authCtx.UserID,
		}); err != nil {
			return gen.Sale{}, mapMoveError(err)
		}
	}

	created, err := qtx.InsertSale(ctx, db.InsertSaleParams{
		ID: returnID, ShopID: authCtx.ShopID, Number: number, Kind: db.SaleKindReturn,
		LocationID: original.LocationID, CustomerID: original.CustomerID, CashierID: authCtx.UserID,
		OriginalSaleID: &originalSaleID, Subtotal: money.ToNumeric(total), DiscountAmount: money.ToNumeric(decimal.Zero),
		Total: money.ToNumeric(total), Note: body.Note,
	})
	if err != nil {
		if apiErr, ok := mapOutOfRange(err); ok {
			return gen.Sale{}, apiErr
		}
		return gen.Sale{}, fmt.Errorf("sales: insert return sale: %w", err)
	}

	for _, l := range lines {
		unitPrice := decimal.Zero
		if l.qty.IsPositive() {
			unitPrice = l.refund.Div(l.qty).Round(2)
		}
		if _, err := qtx.InsertSaleItem(ctx, db.InsertSaleItemParams{
			ID: newID(), ShopID: authCtx.ShopID, SaleID: returnID, VariantID: l.original.VariantID,
			Qty: money.ToNumeric(l.qty), UnitPrice: money.ToNumeric(unitPrice),
			UnitCost: l.original.UnitCost, LineTotal: money.ToNumeric(l.refund),
			OriginalSaleItemID: &l.original.ID,
		}); err != nil {
			if apiErr, ok := mapOutOfRange(err); ok {
				return gen.Sale{}, apiErr
			}
			return gen.Sale{}, fmt.Errorf("sales: insert return sale item: %w", err)
		}
		auditItems = append(auditItems, returnAuditItem{
			OriginalSaleItemID: l.original.ID, VariantID: l.original.VariantID,
			Qty: qtyString(l.qty), Refund: money.String(l.refund),
		})
	}

	if _, err := qtx.InsertSalePayment(ctx, db.InsertSalePaymentParams{
		ID: newID(), ShopID: authCtx.ShopID, SaleID: returnID,
		Method: *paymentRow.PaymentMethod, Amount: money.ToNumeric(total),
	}); err != nil {
		return gen.Sale{}, fmt.Errorf("sales: insert return payment: %w", err)
	}

	after, err := json.Marshal(returnAuditAfter{OriginalSaleID: originalSaleID, Total: money.String(total), Items: auditItems})
	if err != nil {
		return gen.Sale{}, fmt.Errorf("sales: marshal audit after: %w", err)
	}
	if err := audit.Write(ctx, qtx, audit.Entry{
		ShopID: authCtx.ShopID, ActorID: authCtx.UserID, Action: "sale.return",
		EntityType: "sale", EntityID: returnID, After: after,
	}); err != nil {
		return gen.Sale{}, fmt.Errorf("sales: write audit: %w", err)
	}

	shopRow, err := qtx.GetShop(ctx, authCtx.ShopID)
	if err != nil {
		return gen.Sale{}, fmt.Errorf("sales: get shop: %w", err)
	}
	locale := catalog.ResolveLocale(ctx, shopRow.DefaultLocale)
	includeCost := auth.Require(ctx, auth.PermCostRead) == nil
	return h.buildSaleResponse(ctx, qtx, authCtx.ShopID, created.ID, locale, includeCost)
}
