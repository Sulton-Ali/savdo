package sales

// This file: shared helpers for the Phase 5 draft-sales endpoints
// (docs/00-DECISIONS.md D-87..D-89; docs/04-DATA-MODEL.md § 4
// "sale_drafts"/"sale_draft_items"; docs/05-API.md's six `/sales/drafts`
// rows; ADR-014's 2026-09-06 amendment). drafts_read.go (GetSaleDraft,
// ListSaleDrafts) and drafts_write.go (CreateSaleDraftTx,
// UpdateSaleDraftTx, DeleteSaleDraftTx, CompleteSaleDraftTx) both build on
// buildSaleDraftResponse/priceDraftItems below. A draft never stores a
// price (D-87): every read recomputes each line's current unit price by
// the same D-67 precedence create.go's own effectiveUnitPrice applies —
// resolveSaleItems (create.go) is reused directly for a brand new or
// replaced line set (its unit_cost is simply unused: a draft never
// carries cost, for any role, hard rule 5), and priceDraftItems below
// mirrors the same precedence for a draft's already-stored lines, off the
// cost-free GetVariantForCashier/GetProductForCashier queries — never
// GetVariantForSale's cost-carrying row, since there is no cost value
// this response-shaping read ever needs to touch (§ 04-DATA-MODEL.md
// rule 8's "keep separate queries" instruction).

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oapi-codegen/nullable"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/catalog"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/money"
)

// shopClock loads shopID's own row once and returns the locale/timezone
// pair every draft handler needs together: locale for
// productName/variantLabel resolution (requestLocaleFor's own reasoning,
// convert.go) and loc for D-67's promo-activity check (promoActive,
// create.go) — merges CreateSaleTx's own two separate reads of the shop
// row into one, since every draft operation needs both at once.
func shopClock(ctx context.Context, q *db.Queries, shopID uuid.UUID) (locale string, loc *time.Location, err error) {
	shopRow, err := q.GetShop(ctx, shopID)
	if err != nil {
		return "", nil, fmt.Errorf("sales: get shop (draft): %w", err)
	}
	loc, err = time.LoadLocation(shopRow.Timezone)
	if err != nil {
		return "", nil, fmt.Errorf("sales: load shop timezone (draft): %w", err)
	}
	return catalog.ResolveLocale(ctx, shopRow.DefaultLocale), loc, nil
}

// canManageDraft reports whether the authenticated caller in ctx may
// PATCH/DELETE a draft whose creator is createdBy (D-89): manager+
// always may (auth.PermSalesVoid — owner and manager only, the same
// permission VoidSale/CreateSaleReturn gate on); otherwise only the
// draft's own creator. A nil createdBy (the DB column's defensive
// nullability, docs/04-DATA-MODEL.md § 4's own doc comment — this
// service always sets it on create) can never equal any real caller's
// id, so the plain comparison below already enforces "a draft with null
// created_by is manager+ only" without a separate branch for it.
func canManageDraft(ctx context.Context, createdBy *uuid.UUID) bool {
	if auth.Require(ctx, auth.PermSalesVoid) == nil {
		return true
	}
	authCtx, ok := auth.FromContext(ctx)
	return ok && createdBy != nil && *createdBy == authCtx.UserID
}

// draftDisplayItem is one draft line priced and labelled for the wire —
// priceDraftItems' own return shape, converted to gen.SaleDraftItem by
// buildSaleDraftResponse below.
type draftDisplayItem struct {
	variantID    uuid.UUID
	productID    uuid.UUID
	productName  string
	variantLabel string
	qty          decimal.Decimal
	unitPrice    decimal.Decimal
	lineTotal    decimal.Decimal
}

// effectiveDraftUnitPrice is effectiveUnitPrice (create.go) specialized
// to the cost-free cashier-shaped product/variant rows priceDraftItems
// reads: the variant's own price_override when set, else the product's
// base_price, then the product's promo_price when D-67's promoActive
// says today is within it.
func effectiveDraftUnitPrice(product db.GetProductForCashierRow, variant db.GetVariantForCashierRow, now time.Time, loc *time.Location) (decimal.Decimal, error) {
	base := product.BasePrice
	if variant.PriceOverride.Valid {
		base = variant.PriceOverride
	}
	basePrice, err := money.FromNumeric(base)
	if err != nil {
		return decimal.Decimal{}, fmt.Errorf("sales: draft variant base price: %w", err)
	}
	if product.PromoPrice.Valid && promoActive(product.PromoFrom, product.PromoTo, now, loc) {
		promoPrice, err := money.FromNumeric(product.PromoPrice)
		if err != nil {
			return decimal.Decimal{}, fmt.Errorf("sales: draft variant promo price: %w", err)
		}
		return promoPrice, nil
	}
	return basePrice, nil
}

// priceDraftItems resolves rows (a draft's already-stored lines) to
// their current display shape, product lookups memoized by product id
// within one call (two variants of the same product cost one
// GetProductForCashier, not two). Unlike resolveSaleItems, this never
// rejects an inactive or soft-deleted-turned-unreachable variant/product
// with a 404: a draft that outlived a catalogue change must still
// render so the client can edit or drop the offending line — completion
// (CompleteSaleDraftTx, via the real CreateSaleTx path) is what actually
// re-validates and rejects it (D-88's "the client shows which line and
// lets the user edit" describes that rejection, not this read).
func priceDraftItems(ctx context.Context, q *db.Queries, shopID uuid.UUID, rows []db.SaleDraftItem, defs []db.ListAttributeDefinitionsRow, locale string, now time.Time, loc *time.Location) ([]draftDisplayItem, error) {
	products := make(map[uuid.UUID]db.GetProductForCashierRow, len(rows))
	items := make([]draftDisplayItem, len(rows))
	for i, r := range rows {
		variant, err := q.GetVariantForCashier(ctx, db.GetVariantForCashierParams{ShopID: shopID, ID: r.VariantID})
		if err != nil {
			return nil, fmt.Errorf("sales: get variant for cashier (draft item): %w", err)
		}
		product, ok := products[variant.ProductID]
		if !ok {
			product, err = q.GetProductForCashier(ctx, db.GetProductForCashierParams{Locale: locale, ShopID: shopID, ID: variant.ProductID})
			if err != nil {
				return nil, fmt.Errorf("sales: get product for cashier (draft item): %w", err)
			}
			products[variant.ProductID] = product
		}

		label, err := variantLabel(variant.Sku, variant.ID, variant.Attributes, defs)
		if err != nil {
			return nil, err
		}
		unitPrice, err := effectiveDraftUnitPrice(product, variant, now, loc)
		if err != nil {
			return nil, err
		}
		qty, err := money.FromNumeric(r.Qty)
		if err != nil {
			return nil, fmt.Errorf("sales: draft item qty: %w", err)
		}

		items[i] = draftDisplayItem{
			variantID: r.VariantID, productID: variant.ProductID, productName: product.Name,
			variantLabel: label, qty: qty, unitPrice: unitPrice, lineTotal: qty.Mul(unitPrice).Round(2),
		}
	}
	return items, nil
}

// draftDiscountFromRow builds the *gen.SaleDiscount a draft's own stored
// discount_type/discount_value renders as (nil when the draft has none)
// — used both by buildSaleDraftResponse's `discount` field and by
// CompleteSaleDraftTx to carry the same discount into the SaleCreate it
// hands CreateSaleTx.
func draftDiscountFromRow(discountType *db.DiscountType, discountValue pgtype.Numeric) (*gen.SaleDiscount, error) {
	if discountType == nil {
		return nil, nil
	}
	value, err := money.FromNumeric(discountValue)
	if err != nil {
		return nil, fmt.Errorf("sales: draft discount value: %w", err)
	}
	return &gen.SaleDiscount{Type: gen.DiscountType(*discountType), Value: money.String(value)}, nil
}

// buildSaleDraftResponse renders draft's full wire SaleDraft: its
// already-stored discount (draftDiscountFromRow) and lines priced at
// this instant (priceDraftItems), with subtotal/discountAmount/
// estimatedTotal computed from them. discountAmount is
// computeDiscountAmount's (create.go) own result capped at subtotal
// rather than rejected — see SaleDraft.discountAmount's own doc comment
// in contracts/openapi.yaml: a draft is mutable and its stored discount
// can legitimately outlive the subtotal it was set against (an edited
// item, an expired promo); CreateSaleDraftTx/UpdateSaleDraftTx reject
// that case outright (409 DISCOUNT_EXCEEDS_SUBTOTAL, D-57) at write
// time, but a plain read never fails over it. q is the caller's own
// *db.Queries — h.svc.q for a plain read (GetSaleDraft/ListSaleDrafts)
// or a qtx still inside the caller's transaction (every write's own
// response).
func (h *Handler) buildSaleDraftResponse(ctx context.Context, q *db.Queries, shopID uuid.UUID, draft db.SaleDraft, now time.Time, loc *time.Location, locale string, defs []db.ListAttributeDefinitionsRow) (gen.SaleDraft, error) {
	rows, err := q.ListSaleDraftItems(ctx, db.ListSaleDraftItemsParams{ShopID: shopID, DraftID: draft.ID})
	if err != nil {
		return gen.SaleDraft{}, fmt.Errorf("sales: list sale draft items: %w", err)
	}
	priced, err := priceDraftItems(ctx, q, shopID, rows, defs, locale, now, loc)
	if err != nil {
		return gen.SaleDraft{}, err
	}

	items := make([]gen.SaleDraftItem, len(priced))
	subtotal := decimal.Zero
	for i, it := range priced {
		items[i] = gen.SaleDraftItem{
			VariantId: it.variantID, ProductId: it.productID, ProductName: it.productName,
			VariantLabel: it.variantLabel, Qty: qtyString(it.qty),
			UnitPrice: money.String(it.unitPrice), LineTotal: money.String(it.lineTotal),
		}
		subtotal = subtotal.Add(it.lineTotal)
	}

	discount, err := draftDiscountFromRow(draft.DiscountType, draft.DiscountValue)
	if err != nil {
		return gen.SaleDraft{}, err
	}
	discountAmount := decimal.Zero
	if discount != nil {
		discountAmount, err = computeDiscountAmount(discount, subtotal)
		if err != nil {
			return gen.SaleDraft{}, err
		}
		if discountAmount.GreaterThan(subtotal) {
			discountAmount = subtotal
		}
	}
	estimatedTotal := subtotal.Sub(discountAmount)

	return gen.SaleDraft{
		Id: draft.ID, LocationId: draft.LocationID, CustomerId: draft.CustomerID,
		Discount: discount, DiscountReason: draft.DiscountReason, Note: draft.Note,
		Items: items, Subtotal: money.String(subtotal), DiscountAmount: money.String(discountAmount),
		EstimatedTotal: money.String(estimatedTotal), CreatedBy: draft.CreatedBy,
		CreatedAt: draft.CreatedAt, UpdatedAt: draft.UpdatedAt,
	}, nil
}

// optionalUUID reads a nullable.Nullable[uuid.UUID] PATCH field into the
// three states a request can mean: nil (not specified — leave
// unchanged), a pointer to nil (explicit `null` — clear), or a pointer
// to a value (set). Mirrors stock.optionalString/catalog.optionalString/
// crm.optionalString, specialized to uuid.UUID for SaleDraftPatch.customerId.
func optionalUUID(n nullable.Nullable[uuid.UUID]) **uuid.UUID {
	if !n.IsSpecified() {
		return nil
	}
	if n.IsNull() {
		var nilPtr *uuid.UUID
		return &nilPtr
	}
	v := n.MustGet()
	return &[]*uuid.UUID{&v}[0]
}

// optionalString is optionalUUID specialized to string, for
// SaleDraftPatch's discountType/discountValue/discountReason/note —
// mirrors stock.optionalString/catalog.optionalString/crm.optionalString
// (each package keeps its own copy rather than exporting one, the same
// trade-off those packages' own doc comments already make).
func optionalString(n nullable.Nullable[string]) **string {
	if !n.IsSpecified() {
		return nil
	}
	if n.IsNull() {
		var nilPtr *string
		return &nilPtr
	}
	v := n.MustGet()
	return &[]*string{&v}[0]
}
