package sales

// This file: POST /sales — the quick sale (docs/03-ARCHITECTURE.md § Key
// flows › Quick sale; docs/04-DATA-MODEL.md § 4 Rules D-56..D-64;
// ADR-006/007/010/013/014). CreateSaleTx runs entirely on the transaction
// httpx.Idempotent opens and hands it as qtx (httpx/sales.go's own doc
// comment) — every read and write below uses qtx, never h.svc.pool/
// h.svc.q, so this operation never opens a second connection while
// Idempotent's own transaction is still open (the "nested pool acquire"
// trap CreateAdjustmentTx's history already found once, stock/service.go's
// createdByName doc comment).

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/catalog"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/money"
	"github.com/Sulton-Ali/savdo/api/internal/stock"
)

// saleLineInput is one SaleItemCreate, already resolved to its
// server-computed unit_price/unit_cost/line_total (hard rule 8) — never
// the client's, since SaleItemCreate carries only variantId/qty (D-56).
type saleLineInput struct {
	variantID uuid.UUID
	qty       decimal.Decimal
	unitPrice decimal.Decimal
	unitCost  decimal.Decimal
	lineTotal decimal.Decimal
}

// promoActive reports whether today, in loc (the shop's timezone,
// shops.timezone), falls within [from, to] inclusive by calendar date —
// not by comparing now against the stored instants directly, which would
// end a promo whose promo_to is midnight of its last day before that day
// even starts (the D-52/05-API "promo pricing" bullet's own "when active"
// wording, read together with docs/04-DATA-MODEL.md rule 9's "shop
// timezone applied in the service layer"): a promo ending "today" in the
// shop's own calendar must still apply for the rest of that day. from/to
// nil (no promo configured) is never active.
func promoActive(from, to *time.Time, now time.Time, loc *time.Location) bool {
	if from == nil || to == nil {
		return false
	}
	const dayFormat = "2006-01-02"
	today := now.In(loc).Format(dayFormat)
	fromDay := from.In(loc).Format(dayFormat)
	toDay := to.In(loc).Format(dayFormat)
	return today >= fromDay && today <= toDay
}

// effectiveUnitPrice resolves row's price the way the catalogue already
// prices a variant: price_override when set, else the product's
// base_price — then, when a promo is configured and promoActive says
// today is within it, the product's promo_price replaces that entirely
// (D-56: "promo price when active"; promo pricing is product-level only,
// docs/05-API.md's own promo bullet — it is never combined with
// price_override).
func effectiveUnitPrice(row db.GetVariantForSaleRow, now time.Time, loc *time.Location) (decimal.Decimal, error) {
	base := row.BasePrice
	if row.PriceOverride.Valid {
		base = row.PriceOverride
	}
	basePrice, err := money.FromNumeric(base)
	if err != nil {
		return decimal.Decimal{}, fmt.Errorf("sales: variant base price: %w", err)
	}
	if row.PromoPrice.Valid && promoActive(row.PromoFrom, row.PromoTo, now, loc) {
		promoPrice, err := money.FromNumeric(row.PromoPrice)
		if err != nil {
			return decimal.Decimal{}, fmt.Errorf("sales: variant promo price: %w", err)
		}
		return promoPrice, nil
	}
	return basePrice, nil
}

// effectiveUnitCost resolves row's frozen unit_cost the same way a
// purchase receive resolves cost_override (D-42, stock/purchases.go):
// the variant's own cost_override when set, else the product's
// cost_price, else zero — sale_items.unit_cost is NOT NULL (never
// returned to a cashier or public/bot path regardless, hard rule 5), so a
// variant with neither set freezes at "0.00", never a NULL.
func effectiveUnitCost(row db.GetVariantForSaleRow) (decimal.Decimal, error) {
	if row.CostOverride.Valid {
		d, err := money.FromNumeric(row.CostOverride)
		if err != nil {
			return decimal.Decimal{}, fmt.Errorf("sales: variant cost override: %w", err)
		}
		return d, nil
	}
	if row.CostPrice.Valid {
		d, err := money.FromNumeric(row.CostPrice)
		if err != nil {
			return decimal.Decimal{}, fmt.Errorf("sales: product cost price: %w", err)
		}
		return d, nil
	}
	return decimal.Zero, nil
}

// resolveSaleItems validates and prices every line of a SaleCreate:
// variantId must resolve to a variant that belongs to the shop, is not
// soft-deleted, is active, and whose product is also active and not
// soft-deleted (404 "variant" on any miss, checked per item before any
// other item's field errors — same "404 beats batched 400s" order
// validatePurchaseItems uses, stock/purchases.go); a variantId repeated
// across two items is rejected (`items`: invalid — merging quantities
// instead would silently change what the client asked for); qty must be
// a positive decimal string. Field errors are collected across every item
// and reported together as one 400 VALIDATION_FAILED, `items[i].qty`
// naming (mirrors validatePurchaseItems).
func resolveSaleItems(ctx context.Context, qtx *db.Queries, shopID uuid.UUID, in []gen.SaleItemCreate, now time.Time, loc *time.Location) ([]saleLineInput, error) {
	items := make([]saleLineInput, len(in))
	fields := map[string]string{}
	seenVariants := make(map[uuid.UUID]bool, len(in))
	for i, it := range in {
		row, err := qtx.GetVariantForSale(ctx, db.GetVariantForSaleParams{ShopID: shopID, ID: it.VariantId})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, apierr.NotFound("variant")
			}
			return nil, fmt.Errorf("sales: get variant for sale: %w", err)
		}
		if !row.VariantIsActive || !row.ProductIsActive {
			return nil, apierr.NotFound("variant")
		}

		if seenVariants[it.VariantId] {
			fields["items"] = "invalid"
		}
		seenVariants[it.VariantId] = true

		qty, ok := parseQty(it.Qty)
		if !ok || !qty.IsPositive() {
			fields[fmt.Sprintf("items[%d].qty", i)] = "invalid"
			continue
		}

		unitPrice, err := effectiveUnitPrice(row, now, loc)
		if err != nil {
			return nil, err
		}
		unitCost, err := effectiveUnitCost(row)
		if err != nil {
			return nil, err
		}

		items[i] = saleLineInput{
			variantID: it.VariantId, qty: qty, unitPrice: unitPrice, unitCost: unitCost,
			lineTotal: qty.Mul(unitPrice).Round(2),
		}
	}
	if len(fields) > 0 {
		return nil, apierr.Validation(fields)
	}
	return items, nil
}

// computeDiscountAmount resolves SaleCreate.discount against subtotal
// (D-52/D-57): a percent discount (0..100) is
// round(subtotal * value / 100, 2); a fixed discount is value itself,
// capped later by the caller against subtotal (409
// DISCOUNT_EXCEEDS_SUBTOTAL, not here — this function only computes the
// requested amount). No discount at all is zero. discountReason is a
// free-text field on the request, independent of whether discount is
// set, and is not this function's concern.
func computeDiscountAmount(discount *gen.SaleDiscount, subtotal decimal.Decimal) (decimal.Decimal, error) {
	if discount == nil {
		return decimal.Zero, nil
	}
	switch discount.Type {
	case gen.Percent:
		value, apiErr := money.ParseAmount(discount.Value)
		if apiErr != nil {
			return decimal.Decimal{}, apierr.Validation(map[string]string{"discount.value": "invalid"})
		}
		if value.GreaterThan(decimal.NewFromInt(100)) {
			return decimal.Decimal{}, apierr.Validation(map[string]string{"discount.value": "invalid"})
		}
		return subtotal.Mul(value).Div(decimal.NewFromInt(100)).Round(2), nil
	case gen.Fixed:
		value, apiErr := money.ParseAmount(discount.Value)
		if apiErr != nil {
			return decimal.Decimal{}, apierr.Validation(map[string]string{"discount.value": "invalid"})
		}
		return value, nil
	default:
		return decimal.Decimal{}, apierr.Validation(map[string]string{"discount.type": "invalid"})
	}
}

// sortedSaleLines returns lines sorted by variant_id ascending — every
// line of one sale shares the same location_id (SaleCreate.locationId is
// a single field, not per-item), so sorting by (variant_id, location_id)
// (MoveParams' own deadlock-avoidance rule, stock/move.go) reduces to
// sorting by variant_id alone — mirrors stock.sortedPurchaseItems.
func sortedSaleLines(lines []saleLineInput) []saleLineInput {
	sorted := make([]saleLineInput, len(lines))
	copy(sorted, lines)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].variantID.String() < sorted[j].variantID.String() })
	return sorted
}

// mapOutOfRange reports whether err is Postgres' numeric_value_out_of_range
// (a qty*unitPrice or a discount past what numeric(14,2)/numeric(12,3)
// can hold), returning the 400 VALIDATION_FAILED "qty" error in place of
// it — mirrors stock.mapOutOfRange (NIT 13 there).
func mapOutOfRange(err error) (*apierr.Error, bool) {
	if money.IsOutOfRange(err) {
		return apierr.Validation(map[string]string{"qty": "invalid"}), true
	}
	return nil, false
}

// CreateSaleTx completes a quick sale: validates locationId/customerId
// belong to the shop, prices every line server-side (resolveSaleItems),
// computes and caps the manual discount (D-57, 409
// DISCOUNT_EXCEEDS_SUBTOTAL), takes the next sale number under the shop's
// row lock (NextSaleNumber — always before touching any stock row, so
// STOCK_INSUFFICIENT rolls the whole transaction back, including the
// number: the next attempt gets the same one), writes one sale_out
// movement per line via stock.Move (sorted per sortedSaleLines, ref_type
// "sale", ref_id the sale id, unit_cost per line), then inserts the sale,
// its items and its one payment (status completed, completed_at
// defaulted by the migration). qtx must already be bound to the caller's
// transaction — httpx.Idempotent runs this the same way
// stock.Handler.ReceivePurchaseTx runs inside it (internal/httpx cannot be
// imported here without a cycle, per sales.Handler's own doc comment).
// Requires sales.create (owner/manager/cashier, D-56).
func (h *Handler) CreateSaleTx(ctx context.Context, qtx *db.Queries, body *gen.SaleCreate) (gen.Sale, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return gen.Sale{}, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermSalesCreate); err != nil {
		return gen.Sale{}, err
	}

	if len(body.Items) == 0 {
		return gen.Sale{}, apierr.Validation(map[string]string{"items": "required"})
	}
	if len(body.Items) > maxSaleItems {
		return gen.Sale{}, apierr.Validation(map[string]string{"items": "too_long"})
	}
	if !validPaymentMethods[body.Payment.Method] {
		return gen.Sale{}, apierr.Validation(map[string]string{"payment.method": "invalid"})
	}

	if _, err := qtx.GetLocation(ctx, db.GetLocationParams{ShopID: authCtx.ShopID, ID: body.LocationId}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.Sale{}, apierr.NotFound("location")
		}
		return gen.Sale{}, fmt.Errorf("sales: get location: %w", err)
	}
	var customerID *uuid.UUID
	if body.CustomerId != nil {
		if _, err := qtx.GetCustomer(ctx, db.GetCustomerParams{ShopID: authCtx.ShopID, ID: *body.CustomerId}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return gen.Sale{}, apierr.NotFound("customer")
			}
			return gen.Sale{}, fmt.Errorf("sales: get customer: %w", err)
		}
		customerID = body.CustomerId
	}

	shopRow, err := qtx.GetShop(ctx, authCtx.ShopID)
	if err != nil {
		return gen.Sale{}, fmt.Errorf("sales: get shop: %w", err)
	}
	shopLoc, err := time.LoadLocation(shopRow.Timezone)
	if err != nil {
		return gen.Sale{}, fmt.Errorf("sales: load shop timezone: %w", err)
	}

	lines, err := resolveSaleItems(ctx, qtx, authCtx.ShopID, body.Items, h.svc.now(), shopLoc)
	if err != nil {
		return gen.Sale{}, err
	}

	subtotal := decimal.Zero
	for _, l := range lines {
		subtotal = subtotal.Add(l.lineTotal)
	}
	discountAmount, err := computeDiscountAmount(body.Discount, subtotal)
	if err != nil {
		return gen.Sale{}, err
	}
	if discountAmount.GreaterThan(subtotal) {
		return gen.Sale{}, errDiscountExceedsSubtotal
	}
	total := subtotal.Sub(discountAmount)

	number, err := qtx.NextSaleNumber(ctx, authCtx.ShopID)
	if err != nil {
		return gen.Sale{}, fmt.Errorf("sales: next sale number: %w", err)
	}

	saleID := newID()
	refType := saleRefType
	for _, l := range sortedSaleLines(lines) {
		// UnitCost is left nil here, same as every other non-purchase
		// mover (adjustments, transfers) — D-64's margin math reads
		// sale_items.unit_cost (frozen below by InsertSaleItem), not the
		// movement row; stock_movements.unit_cost exists only to record
		// what stock cost coming in, on a purchase_in line.
		if _, err := stock.Move(ctx, qtx, stock.MoveParams{
			ShopID: authCtx.ShopID, VariantID: l.variantID, LocationID: body.LocationId,
			Kind: db.StockMovementKindSaleOut, Qty: l.qty.Neg(),
			RefType: &refType, RefID: &saleID, ActorID: &authCtx.UserID,
		}); err != nil {
			return gen.Sale{}, mapMoveError(err)
		}
	}

	created, err := qtx.InsertSale(ctx, db.InsertSaleParams{
		ID: saleID, ShopID: authCtx.ShopID, Number: number, Kind: db.SaleKindSale,
		LocationID: body.LocationId, CustomerID: customerID, CashierID: authCtx.UserID,
		Subtotal: money.ToNumeric(subtotal), DiscountAmount: money.ToNumeric(discountAmount),
		DiscountReason: body.DiscountReason, Total: money.ToNumeric(total), Note: body.Note,
	})
	if err != nil {
		if apiErr, ok := mapOutOfRange(err); ok {
			return gen.Sale{}, apiErr
		}
		return gen.Sale{}, fmt.Errorf("sales: insert sale: %w", err)
	}

	for _, l := range lines {
		if _, err := qtx.InsertSaleItem(ctx, db.InsertSaleItemParams{
			ID: newID(), ShopID: authCtx.ShopID, SaleID: saleID, VariantID: l.variantID,
			Qty: money.ToNumeric(l.qty), UnitPrice: money.ToNumeric(l.unitPrice),
			UnitCost: money.ToNumeric(l.unitCost), LineTotal: money.ToNumeric(l.lineTotal),
		}); err != nil {
			if apiErr, ok := mapOutOfRange(err); ok {
				return gen.Sale{}, apiErr
			}
			return gen.Sale{}, fmt.Errorf("sales: insert sale item: %w", err)
		}
	}

	if _, err := qtx.InsertSalePayment(ctx, db.InsertSalePaymentParams{
		ID: newID(), ShopID: authCtx.ShopID, SaleID: saleID,
		Method: db.PaymentMethod(body.Payment.Method), Amount: money.ToNumeric(total),
	}); err != nil {
		return gen.Sale{}, fmt.Errorf("sales: insert sale payment: %w", err)
	}

	locale := catalog.ResolveLocale(ctx, shopRow.DefaultLocale)
	includeCost := auth.Require(ctx, auth.PermCostRead) == nil
	resp, err := h.buildSaleResponse(ctx, qtx, authCtx.ShopID, created.ID, locale, includeCost)
	if err != nil {
		return gen.Sale{}, err
	}
	return resp, nil
}
