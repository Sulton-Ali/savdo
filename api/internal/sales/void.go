package sales

// This file: POST /sales/{id}/void (docs/04-DATA-MODEL.md § 4 Rules
// D-56..D-69; ADR-006/ADR-013/ADR-014). VoidSaleTx runs entirely on the
// transaction httpx.VoidSale opens and hands it as qtx — sales.Handler
// holds no pool of its own (service.go's own doc comment: this task
// "adds whatever dependency it needs" only if it turns out to need one;
// it does not — the contract has no Idempotency-Key for this operation,
// so there is no advisory lock or replay bookkeeping to share a
// transaction with, and httpx already holds the pool CreateSale's own
// split needs anyway), so httpx.VoidSale begins, commits and rolls back
// the one transaction this method runs on, the same "single connection"
// shape CreateSaleTx's own doc comment describes, applied here for a
// simpler reason: there is just one write path, not a replay-or-run-once
// helper wrapping it.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/audit"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/catalog"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/money"
	"github.com/Sulton-Ali/savdo/api/internal/stock"
)

// saleVoidRefType is the stock_movements.ref_type VoidSaleTx writes on
// every sale_void_in movement — mirrors saleRefType (create.go)/
// purchaseCancelRefType (stock/errors.go): "kind carries the story,
// ref_type carries what to blame it on".
const saleVoidRefType = "sale_void"

// voidAuditItem/voidAuditBefore/voidAuditAfter are the sale.void
// audit_log row's before/after JSON shapes (D-47) — mirror stock's
// purchaseAuditItem/purchaseAuditBefore/purchaseAuditAfter
// (stock/purchases.go).
type voidAuditItem struct {
	VariantID uuid.UUID `json:"variantId"`
	Qty       string    `json:"qty"`
}

type voidAuditBefore struct {
	Status string `json:"status"`
}

type voidAuditAfter struct {
	Status string          `json:"status"`
	Reason *string         `json:"reason,omitempty"`
	Items  []voidAuditItem `json:"items"`
}

// sortedSaleItemsByVariant returns rows sorted by variant_id ascending —
// mirrors sortedSaleLines (create.go): a sale's items all share the sale's
// own single location_id, so sorting by (variant_id, location_id)
// (MoveParams' own deadlock-avoidance rule) reduces to sorting by
// variant_id alone.
func sortedSaleItemsByVariant(rows []db.SaleItem) []db.SaleItem {
	sorted := make([]db.SaleItem, len(rows))
	copy(sorted, rows)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].VariantID.String() < sorted[j].VariantID.String() })
	return sorted
}

// sameCalendarDay reports whether a and b fall on the same calendar date
// in loc — mirrors promoActive's own day-string comparison (create.go),
// specialized to two instants instead of a range.
func sameCalendarDay(a, b time.Time, loc *time.Location) bool {
	const dayFormat = "2006-01-02"
	return a.In(loc).Format(dayFormat) == b.In(loc).Format(dayFormat)
}

// VoidSaleTx voids id: 404 "sale" when it does not resolve in the actor's
// shop; 409 SALE_NOT_VOIDABLE when it is a return, not a sale (D-66); 409
// SALE_ALREADY_VOIDED when it is already voided; 409
// SALE_VOID_WINDOW_CLOSED once its completed_at's calendar date, in the
// shop's own timezone, is no longer today (D-59); 409 SALE_HAS_RETURNS
// when a completed return already references it (D-62). Otherwise: locks
// the sale row (GetSaleForUpdate, so a concurrent void/return against the
// same sale serializes instead of racing) and its item rows
// (GetSaleItemsForUpdate), writes one sale_void_in movement per line
// (sorted per sortedSaleItemsByVariant, ref_type "sale_void", ref_id the
// sale id, nil unit_cost — same reasoning as CreateSaleTx's own sale_out
// movements: sale_items.unit_cost, not stock_movements.unit_cost, is
// where D-64's margin math reads from), sets status voided via VoidSale
// (voided_at/voided_by/void_reason — the only UPDATE ADR-014 allows), and
// writes a sale.void audit_log row (D-47). Requires sales.void
// (owner/manager, D-58's "manager+"). qtx must already be bound to the
// caller's own transaction (httpx.VoidSale's own doc comment).
func (h *Handler) VoidSaleTx(ctx context.Context, qtx *db.Queries, id uuid.UUID, body *gen.SaleVoid) (gen.Sale, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return gen.Sale{}, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermSalesVoid); err != nil {
		return gen.Sale{}, err
	}

	current, err := qtx.GetSaleForUpdate(ctx, db.GetSaleForUpdateParams{ShopID: authCtx.ShopID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.Sale{}, apierr.NotFound("sale")
		}
		return gen.Sale{}, fmt.Errorf("sales: get sale for update: %w", err)
	}
	if current.Kind != db.SaleKindSale {
		return gen.Sale{}, errSaleNotVoidable
	}
	if current.Status == db.SaleStatusVoided {
		return gen.Sale{}, errSaleAlreadyVoided
	}

	shopRow, err := qtx.GetShop(ctx, authCtx.ShopID)
	if err != nil {
		return gen.Sale{}, fmt.Errorf("sales: get shop: %w", err)
	}
	shopLoc, err := time.LoadLocation(shopRow.Timezone)
	if err != nil {
		return gen.Sale{}, fmt.Errorf("sales: load shop timezone: %w", err)
	}
	if !sameCalendarDay(current.CompletedAt, h.svc.now(), shopLoc) {
		return gen.Sale{}, errSaleVoidWindowClosed
	}

	returnsCount, err := qtx.CountCompletedReturnsForSale(ctx, db.CountCompletedReturnsForSaleParams{ShopID: authCtx.ShopID, OriginalSaleID: &id})
	if err != nil {
		return gen.Sale{}, fmt.Errorf("sales: count completed returns: %w", err)
	}
	if returnsCount > 0 {
		return gen.Sale{}, errSaleHasReturns
	}

	items, err := qtx.GetSaleItemsForUpdate(ctx, db.GetSaleItemsForUpdateParams{ShopID: authCtx.ShopID, SaleID: id})
	if err != nil {
		return gen.Sale{}, fmt.Errorf("sales: get sale items for update: %w", err)
	}

	refType := saleVoidRefType
	auditItems := make([]voidAuditItem, 0, len(items))
	for _, it := range sortedSaleItemsByVariant(items) {
		qty, err := money.FromNumeric(it.Qty)
		if err != nil {
			return gen.Sale{}, fmt.Errorf("sales: sale item qty: %w", err)
		}
		if _, err := stock.Move(ctx, qtx, stock.MoveParams{
			ShopID: authCtx.ShopID, VariantID: it.VariantID, LocationID: current.LocationID,
			Kind: db.StockMovementKindSaleVoidIn, Qty: qty,
			RefType: &refType, RefID: &current.ID, ActorID: &authCtx.UserID,
		}); err != nil {
			return gen.Sale{}, mapMoveError(err)
		}
		auditItems = append(auditItems, voidAuditItem{VariantID: it.VariantID, Qty: qtyString(qty)})
	}

	var reason *string
	if body != nil && body.Reason != nil && *body.Reason != "" {
		reason = body.Reason
	}

	voided, err := qtx.VoidSale(ctx, db.VoidSaleParams{VoidedBy: &authCtx.UserID, VoidReason: reason, ShopID: authCtx.ShopID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Unreachable in practice: current.Kind/Status were just
			// checked above under GetSaleForUpdate's own row lock, and
			// nothing between there and here can have changed them.
			return gen.Sale{}, errSaleNotVoidable
		}
		return gen.Sale{}, fmt.Errorf("sales: void sale: %w", err)
	}

	before, err := json.Marshal(voidAuditBefore{Status: string(current.Status)})
	if err != nil {
		return gen.Sale{}, fmt.Errorf("sales: marshal audit before: %w", err)
	}
	after, err := json.Marshal(voidAuditAfter{Status: string(voided.Status), Reason: reason, Items: auditItems})
	if err != nil {
		return gen.Sale{}, fmt.Errorf("sales: marshal audit after: %w", err)
	}
	if err := audit.Write(ctx, qtx, audit.Entry{
		ShopID: authCtx.ShopID, ActorID: authCtx.UserID, Action: "sale.void",
		EntityType: "sale", EntityID: id, Before: before, After: after,
	}); err != nil {
		return gen.Sale{}, fmt.Errorf("sales: write audit: %w", err)
	}

	locale := catalog.ResolveLocale(ctx, shopRow.DefaultLocale)
	includeCost := auth.Require(ctx, auth.PermCostRead) == nil
	return h.buildSaleResponse(ctx, qtx, authCtx.ShopID, id, locale, includeCost)
}
