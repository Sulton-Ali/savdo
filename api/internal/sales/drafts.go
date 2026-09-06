package sales

// This file: shared helpers for the Phase 5 draft-sales endpoints
// (docs/00-DECISIONS.md D-87..D-89, D-96; docs/04-DATA-MODEL.md § 4
// "sale_drafts"/"sale_draft_items"; docs/05-API.md's six `/sales/drafts`
// rows; ADR-014's 2026-09-06 amendment). drafts_read.go (GetSaleDraft,
// ListSaleDrafts) and drafts_write.go (CreateSaleDraftTx,
// UpdateSaleDraftTx, DeleteSaleDraftTx, CompleteSaleDraftTx) both build on
// buildSaleDraftResponse/priceDraftItemsBatch below. A draft never stores
// a price (D-87): every read recomputes each line's current unit price by
// the same D-67 precedence create.go's own effectiveUnitPrice applies —
// resolveSaleItems (create.go) is reused directly for a brand new or
// replaced line set (its unit_cost is simply unused: a draft never
// carries cost, for any role, hard rule 5), and priceDraftItemsBatch below
// mirrors the same precedence for a draft's already-stored lines, off
// ListSaleDraftItemsForPricing — a single, cost-free, batched query
// (review MAJOR: the previous per-line GetVariantForCashier +
// GetProductForCashier pair was an N+1, one extra round trip per line per
// draft on every GET/List) that also surfaces an inactive or
// soft-deleted variant/product as `available: false` instead of a
// missing row (review CRITICAL: a plain read must never 500 or 404 over
// that state — only completion, CompleteSaleDraftTx, actually rejects
// it, 422 naming the line, D-88).
//
// Prices are never stored on a draft — every read recomputes from the
// catalogue, so priceDraftItemsBatch's `GetVariantForCashier`/
// `GetProductForCashier`-equivalent columns are the SaleDraftItem's own
// (never `GetVariantForSale`'s cost-carrying row: this is a
// response-shaping read, § 04-DATA-MODEL.md rule 8's "keep separate
// queries" instruction, not the internal pricing read inside a sale's
// own write transaction D-69 carves out).

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
// PATCH/DELETE a draft whose creator is createdBy (D-89 — unaffected by
// D-96, which only widens who may *complete* a draft). manager+ always
// may (auth.PermSalesVoid — owner and manager only, the same permission
// VoidSale/CreateSaleReturn gate on); otherwise only the draft's own
// creator. A nil createdBy (the DB column's defensive nullability,
// docs/04-DATA-MODEL.md § 4's own doc comment — this service always sets
// it on create) can never equal any real caller's id, so the plain
// comparison below already enforces "a draft with null created_by is
// manager+ only" without a separate branch for it.
func canManageDraft(ctx context.Context, createdBy *uuid.UUID) bool {
	if auth.Require(ctx, auth.PermSalesVoid) == nil {
		return true
	}
	authCtx, ok := auth.FromContext(ctx)
	return ok && createdBy != nil && *createdBy == authCtx.UserID
}

// draftDisplayItem is one draft line priced, labelled and availability-
// checked for the wire — priceDraftItemsBatch's own return shape,
// converted to gen.SaleDraftItem by buildSaleDraftResponse below.
// unitPrice/lineTotal are "0.00" (decimal.Zero) whenever available is
// false: the variant or product behind this line has gone inactive or
// was soft-deleted since the line was added (review CRITICAL) — the line
// still renders so the draft stays editable, it just prices at zero and
// is excluded from subtotal (the zero contributes nothing to the sum
// regardless).
type draftDisplayItem struct {
	variantID    uuid.UUID
	productID    uuid.UUID
	productName  string
	variantLabel string
	qty          decimal.Decimal
	unitPrice    decimal.Decimal
	lineTotal    decimal.Decimal
	available    bool
}

// effectiveDraftUnitPrice is effectiveUnitPrice (create.go) specialized
// to one row of the batched ListSaleDraftItemsForPricing query: the
// variant's own price_override when set, else the product's base_price,
// then the product's promo_price when D-67's promoActive says today is
// within it. Callers only invoke this once a row's own
// variant_available/product_available are both true — an unavailable
// line is never priced at all (draftDisplayItem's own doc comment).
func effectiveDraftUnitPrice(row db.ListSaleDraftItemsForPricingRow, now time.Time, loc *time.Location) (decimal.Decimal, error) {
	base := row.BasePrice
	if row.PriceOverride.Valid {
		base = row.PriceOverride
	}
	basePrice, err := money.FromNumeric(base)
	if err != nil {
		return decimal.Decimal{}, fmt.Errorf("sales: draft variant base price: %w", err)
	}
	if row.PromoPrice.Valid && promoActive(row.PromoFrom, row.PromoTo, now, loc) {
		promoPrice, err := money.FromNumeric(row.PromoPrice)
		if err != nil {
			return decimal.Decimal{}, fmt.Errorf("sales: draft variant promo price: %w", err)
		}
		return promoPrice, nil
	}
	return basePrice, nil
}

// rowAvailable reports whether row's variant and product are both still
// live for sale: neither soft-deleted nor deactivated since the line was
// added. ListSaleDraftItemsForPricing's own VariantAvailable/
// ProductAvailable are sqlc-inferred as nullable (`*bool`, a conservative
// static read on a computed boolean expression) even though the query's
// plain JOINs guarantee a row and the expression itself
// (`is_active AND deleted_at IS NULL`) is never actually NULL in
// practice — nil is treated as unavailable defensively rather than
// trusted true, so a genuinely unexpected NULL fails safe (an item
// wrongly hidden from the subtotal) rather than unsafe (a phantom line
// silently priced and sold).
func rowAvailable(row db.ListSaleDraftItemsForPricingRow) bool {
	return row.VariantAvailable != nil && *row.VariantAvailable &&
		row.ProductAvailable != nil && *row.ProductAvailable
}

// priceDraftItemsBatch prices every line of every draft named in
// draftIDs in one round trip (ListSaleDraftItemsForPricing) instead of a
// GetVariantForCashier/GetProductForCashier pair per line (review MAJOR:
// N+1 in ListSaleDrafts) — the same ANY($ids) batching
// ListCoverImagesForProducts already uses for D-83, reused here for
// GetSaleDraft/buildSaleDraftResponse's own single-draft case too (a
// one-element draftIDs, not a special case) so there is exactly one
// pricing code path for every caller. A line whose variant or product
// has gone inactive or was soft-deleted renders (available: false,
// unitPrice/lineTotal zero) rather than erroring (review CRITICAL) —
// only CompleteSaleDraftTx treats that state as fatal. Each draft's
// items are returned in the same (position, id) order
// ListSaleDraftItemsForPricing's own ORDER BY guarantees; a draft with
// no rows at all (should not happen — every write path requires at
// least one item) is simply absent from the returned map, so callers
// index it with the zero-value (nil slice, len 0) that a missing map key
// already gives.
func priceDraftItemsBatch(ctx context.Context, q *db.Queries, shopID uuid.UUID, draftIDs []uuid.UUID, locale string, now time.Time, loc *time.Location, defs []db.ListAttributeDefinitionsRow) (map[uuid.UUID][]draftDisplayItem, error) {
	byDraft := make(map[uuid.UUID][]draftDisplayItem, len(draftIDs))
	if len(draftIDs) == 0 {
		return byDraft, nil
	}

	rows, err := q.ListSaleDraftItemsForPricing(ctx, db.ListSaleDraftItemsForPricingParams{
		Locale: locale, ShopID: shopID, SaleDraftIds: draftIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("sales: list sale draft items for pricing: %w", err)
	}

	for _, r := range rows {
		qty, err := money.FromNumeric(r.Qty)
		if err != nil {
			return nil, fmt.Errorf("sales: draft item qty: %w", err)
		}
		label, err := variantLabel(r.VariantSku, r.VariantID, r.VariantAttributes, defs)
		if err != nil {
			return nil, err
		}

		item := draftDisplayItem{
			variantID: r.VariantID, productID: r.ProductID, productName: r.ProductName,
			variantLabel: label, qty: qty, available: rowAvailable(r),
		}
		if item.available {
			unitPrice, err := effectiveDraftUnitPrice(r, now, loc)
			if err != nil {
				return nil, err
			}
			item.unitPrice = unitPrice
			item.lineTotal = qty.Mul(unitPrice).Round(2)
		}
		byDraft[r.SaleDraftID] = append(byDraft[r.SaleDraftID], item)
	}
	return byDraft, nil
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

// assembleSaleDraft converts draft plus its already-priced lines
// (priceDraftItemsBatch's own per-draft slice) into the wire SaleDraft —
// pure, no I/O, so ListSaleDrafts can call priceDraftItemsBatch exactly
// once for a whole page and then assemble each row from the shared
// result (review MAJOR fix) instead of buildSaleDraftResponse's own
// one-query-per-draft loop. discountAmount is computeDiscountAmount's
// (create.go) own result capped at subtotal rather than rejected — see
// SaleDraft.discountAmount's own doc comment in contracts/openapi.yaml: a
// draft is mutable and its stored discount can legitimately outlive the
// subtotal it was set against (an edited item, an expired promo, or a
// line that went unavailable and now contributes zero);
// CreateSaleDraftTx/UpdateSaleDraftTx reject that case outright when
// `items` or the discount itself is touched (409
// DISCOUNT_EXCEEDS_SUBTOTAL, D-57), but a plain read never fails over
// it, and a PATCH touching neither leaves a stale discount alone
// (review MINOR).
func assembleSaleDraft(draft db.SaleDraft, priced []draftDisplayItem) (gen.SaleDraft, error) {
	items := make([]gen.SaleDraftItem, len(priced))
	subtotal := decimal.Zero
	for i, it := range priced {
		items[i] = gen.SaleDraftItem{
			VariantId: it.variantID, ProductId: it.productID, ProductName: it.productName,
			VariantLabel: it.variantLabel, Qty: qtyString(it.qty),
			UnitPrice: money.String(it.unitPrice), LineTotal: money.String(it.lineTotal),
			Available: it.available,
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
		Id: draft.ID, LocationId: draft.LocationID, CustomerId: nullableUUID(draft.CustomerID),
		Discount: nullableSaleDiscount(discount), DiscountReason: nullableString(draft.DiscountReason), Note: nullableString(draft.Note),
		Items: items, Subtotal: money.String(subtotal), DiscountAmount: money.String(discountAmount),
		EstimatedTotal: money.String(estimatedTotal), CreatedBy: nullableUUID(draft.CreatedBy),
		CreatedAt: draft.CreatedAt, UpdatedAt: draft.UpdatedAt,
	}, nil
}

// buildSaleDraftResponse is assembleSaleDraft for a single draft,
// pricing its lines itself (priceDraftItemsBatch with a one-element
// draftIDs) — the shape every single-draft caller (GetSaleDraft,
// CreateSaleDraftTx, UpdateSaleDraftTx) needs. q is the caller's own
// *db.Queries — h.svc.q for a plain read (GetSaleDraft) or a qtx still
// inside the caller's transaction (every write's own response).
func buildSaleDraftResponse(ctx context.Context, q *db.Queries, shopID uuid.UUID, draft db.SaleDraft, now time.Time, loc *time.Location, locale string, defs []db.ListAttributeDefinitionsRow) (gen.SaleDraft, error) {
	byDraft, err := priceDraftItemsBatch(ctx, q, shopID, []uuid.UUID{draft.ID}, locale, now, loc, defs)
	if err != nil {
		return gen.SaleDraft{}, err
	}
	return assembleSaleDraft(draft, byDraft[draft.ID])
}

// nullableSaleDiscount converts a *gen.SaleDiscount (nil = SQL NULL) to
// the tri-state nullable.Nullable SaleDraft.discount needs (review:
// switched to Sale's own required-and-nullable convention for
// customerId/discount/discountReason/note/createdBy) — mirrors
// nullableUUID/nullableString/nullableTime (convert.go), specialized to
// an object rather than a scalar.
func nullableSaleDiscount(v *gen.SaleDiscount) nullable.Nullable[gen.SaleDiscount] {
	if v == nil {
		return nullable.NewNullNullable[gen.SaleDiscount]()
	}
	return nullable.NewNullableWithValue(*v)
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
