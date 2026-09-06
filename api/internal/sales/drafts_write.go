package sales

// This file: POST /sales/drafts, PATCH /sales/drafts/{id},
// DELETE /sales/drafts/{id} and POST /sales/drafts/{id}/complete
// (docs/00-DECISIONS.md D-87..D-89, D-96; docs/04-DATA-MODEL.md § 4;
// ADR-006/007/010/013/014). Every method here is a plain
// (result, error) method taking the caller's own transaction (qtx) —
// sales.Handler has no pool of its own (service.go's own doc comment),
// so httpx/sales.go opens the transaction each one runs on, the same
// split CreateSale/VoidSale/CreateSaleReturn already use and for the same
// reason (internal/httpx already imports internal/sales to wire the
// router, so the reverse import would cycle). CreateSaleDraftTx/
// UpdateSaleDraftTx/DeleteSaleDraftTx need no Idempotency-Key
// (contracts/openapi.yaml has none for them: a duplicate draft, edit or
// delete is not a ledger event); CompleteSaleDraftTx does, the same as
// CreateSaleTx, since it creates a real Sale.

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oapi-codegen/nullable"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/money"
)

// resolveNewDiscount validates a brand new SaleDraftCreate.discount (or
// none at all) against subtotal, the same way CreateSaleTx's own
// discountAmount check does (409 DISCOUNT_EXCEEDS_SUBTOTAL, D-57), and
// returns the discount_type/discount_value pair CreateSaleDraft's own
// INSERT stores. A nil discount returns (nil, zero-value, nil) — no
// discount at all. Unlike SaleDraftPatch's discountType/discountValue
// (resolveDraftDiscountPatch below), SaleDraftCreate.discount is always
// an atomic `{type, value}` object or absent entirely (contracts/
// openapi.yaml's SaleDiscount schema), so there is no partial-pair case
// to validate here.
func resolveNewDiscount(discount *gen.SaleDiscount, subtotal decimal.Decimal) (*db.DiscountType, pgtype.Numeric, error) {
	if discount == nil {
		return nil, pgtype.Numeric{}, nil
	}
	amount, err := computeDiscountAmount(discount, subtotal)
	if err != nil {
		return nil, pgtype.Numeric{}, err
	}
	if amount.GreaterThan(subtotal) {
		return nil, pgtype.Numeric{}, errDiscountExceedsSubtotal
	}
	dt := db.DiscountType(discount.Type)
	value, apiErr := money.ParseAmount(discount.Value)
	if apiErr != nil {
		return nil, pgtype.Numeric{}, apiErr
	}
	return &dt, money.ToNumeric(value), nil
}

// CreateSaleDraftTx saves a new draft (POST /sales/drafts, D-87):
// locationId/customerId must belong to the shop (404 otherwise, same as
// CreateSaleTx); items are validated and priced exactly like a real sale
// (resolveSaleItems, create.go) but only variant_id/qty are persisted —
// no stock movement, no availability change (D-88). created_by is always
// the authenticated caller. Requires sales.create (owner/manager/cashier,
// D-87).
func (h *Handler) CreateSaleDraftTx(ctx context.Context, qtx *db.Queries, body *gen.SaleDraftCreate) (gen.SaleDraft, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return gen.SaleDraft{}, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermSalesCreate); err != nil {
		return gen.SaleDraft{}, err
	}

	if len(body.Items) == 0 {
		return gen.SaleDraft{}, apierr.Validation(map[string]string{"items": "required"})
	}
	if len(body.Items) > maxSaleItems {
		return gen.SaleDraft{}, apierr.Validation(map[string]string{"items": "too_long"})
	}

	if _, err := qtx.GetLocation(ctx, db.GetLocationParams{ShopID: authCtx.ShopID, ID: body.LocationId}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.SaleDraft{}, apierr.NotFound("location")
		}
		return gen.SaleDraft{}, fmt.Errorf("sales: get location: %w", err)
	}
	var customerID *uuid.UUID
	if body.CustomerId != nil {
		if _, err := qtx.GetCustomer(ctx, db.GetCustomerParams{ShopID: authCtx.ShopID, ID: *body.CustomerId}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return gen.SaleDraft{}, apierr.NotFound("customer")
			}
			return gen.SaleDraft{}, fmt.Errorf("sales: get customer: %w", err)
		}
		customerID = body.CustomerId
	}

	locale, loc, err := shopClock(ctx, qtx, authCtx.ShopID)
	if err != nil {
		return gen.SaleDraft{}, err
	}
	now := h.svc.now()

	lines, err := resolveSaleItems(ctx, qtx, authCtx.ShopID, body.Items, now, loc)
	if err != nil {
		return gen.SaleDraft{}, err
	}
	subtotal := decimal.Zero
	for _, l := range lines {
		subtotal = subtotal.Add(l.lineTotal)
	}

	discountType, discountValue, err := resolveNewDiscount(body.Discount, subtotal)
	if err != nil {
		return gen.SaleDraft{}, err
	}

	draft, err := qtx.CreateSaleDraft(ctx, db.CreateSaleDraftParams{
		ID: newID(), ShopID: authCtx.ShopID, LocationID: body.LocationId, CustomerID: customerID,
		DiscountType: discountType, DiscountValue: discountValue, DiscountReason: body.DiscountReason,
		Note: body.Note, CreatedBy: &authCtx.UserID,
	})
	if err != nil {
		return gen.SaleDraft{}, fmt.Errorf("sales: create sale draft: %w", err)
	}

	for i, l := range lines {
		if _, err := qtx.InsertSaleDraftItem(ctx, db.InsertSaleDraftItemParams{
			ID: newID(), ShopID: authCtx.ShopID, SaleDraftID: draft.ID, VariantID: l.variantID,
			Qty: money.ToNumeric(l.qty), Position: int32(i),
		}); err != nil {
			return gen.SaleDraft{}, fmt.Errorf("sales: insert sale draft item: %w", err)
		}
	}

	defs, err := qtx.ListAttributeDefinitions(ctx, db.ListAttributeDefinitionsParams{ShopID: authCtx.ShopID, Locale: locale})
	if err != nil {
		return gen.SaleDraft{}, fmt.Errorf("sales: list attribute definitions: %w", err)
	}
	createdByName, err := resolveCreatedByName(ctx, qtx, authCtx.ShopID, draft.CreatedBy)
	if err != nil {
		return gen.SaleDraft{}, err
	}
	customerName, err := resolveCustomerName(ctx, qtx, authCtx.ShopID, draft.CustomerID)
	if err != nil {
		return gen.SaleDraft{}, err
	}
	return buildSaleDraftResponse(ctx, qtx, authCtx.ShopID, draft, createdByName, customerName, now, loc, locale, defs)
}

// draftDiscountPatch is resolveDraftDiscountPatch's own result: the
// fully-merged discount_type/discount_value pair a PATCH should leave
// stored, already validated against the D-52 "one concept" invariant
// (sale_drafts' own CHECK: both null or both set) — so the caller passes
// it to UpdateSaleDraft's ClearDiscount/DiscountType/DiscountValue
// params directly, never relying on that query's own COALESCE-with-
// current fallback to resolve an inconsistent pair (review MAJOR: that
// fallback alone let a lone discountType or discountValue reach the
// UPDATE and surface as an opaque 500 SQLSTATE 23514 instead of a 400).
// touched is false when the request named neither discountType nor
// discountValue at all — the caller must then leave the discount
// entirely alone, including skipping the post-write
// DISCOUNT_EXCEEDS_SUBTOTAL re-check (review MINOR: a note-only PATCH
// must succeed even when the already-stored discount has since gone
// stale against the current subtotal).
type draftDiscountPatch struct {
	touched bool
	clear   bool
	typ     *db.DiscountType
	value   pgtype.Numeric
}

// resolveDraftDiscountPatch merges SaleDraftPatch's discountType/
// discountValue against current's already-stored pair, following the
// exact D-35 pairing convention docs/04-DATA-MODEL.md § 4 and the
// contract's own SaleDraftPatch/updateSaleDraft descriptions state (the
// same rule ProductPatch's promo fields already use, ClearPromo,
// catalog.UpdateProduct): an explicit `null` on *either* field clears
// the discount entirely — even when the other field carries a real
// value in the very same request, e.g. `{"discountType":null,
// "discountValue":"100.00"}` clears, it does not 400 — because a clear
// signal on one half of an atomic pair is unambiguous regardless of
// what the other half says. Short of an explicit null, naming only one
// half with a non-null value merges it with current's already-stored
// other half; if that other half was never stored, the pair cannot be
// completed and this is a 400 VALIDATION_FAILED naming the missing half
// (the same invariant sale_drafts' own CHECK constraint enforces,
// checked here before any UPDATE runs, so a lone half never surfaces as
// a raw constraint-violation 500). touched is false only when the
// request named neither field at all — the caller must then leave the
// discount entirely alone, including skipping the post-write
// DISCOUNT_EXCEEDS_SUBTOTAL re-check (a note-only PATCH must succeed
// even when the already-stored discount has since gone stale against
// the current subtotal).
func resolveDraftDiscountPatch(typeField, valueField nullable.Nullable[string], current db.SaleDraft) (draftDiscountPatch, error) {
	typeSet := optionalString(typeField)
	valueSet := optionalString(valueField)
	if typeSet == nil && valueSet == nil {
		return draftDiscountPatch{touched: false}, nil
	}

	// An explicit null on either half clears the whole pair outright,
	// regardless of what the other half names — checked first, before
	// any parsing of a same-request value the clear makes moot.
	if (typeSet != nil && *typeSet == nil) || (valueSet != nil && *valueSet == nil) {
		return draftDiscountPatch{touched: true, clear: true}, nil
	}

	finalType := current.DiscountType
	if typeSet != nil {
		t := db.DiscountType(**typeSet)
		if t != db.DiscountTypePercent && t != db.DiscountTypeFixed {
			return draftDiscountPatch{}, apierr.Validation(map[string]string{"discountType": "invalid"})
		}
		finalType = &t
	}

	finalValue := current.DiscountValue
	finalValueSet := current.DiscountValue.Valid
	if valueSet != nil {
		v, apiErr := money.ParseAmount(**valueSet)
		if apiErr != nil {
			return draftDiscountPatch{}, apierr.Validation(map[string]string{"discountValue": "invalid"})
		}
		finalValue = money.ToNumeric(v)
		finalValueSet = true
	}

	if (finalType == nil) != !finalValueSet {
		if finalType == nil {
			return draftDiscountPatch{}, apierr.Validation(map[string]string{"discountType": "required"})
		}
		return draftDiscountPatch{}, apierr.Validation(map[string]string{"discountValue": "required"})
	}

	return draftDiscountPatch{touched: true, clear: false, typ: finalType, value: finalValue}, nil
}

// UpdateSaleDraftTx edits a draft (PATCH /sales/drafts/{id}, D-87).
// Requires sales.create and either the draft's own creator or manager+
// (canManageDraft, D-89), checked only after the row is locked
// (GetSaleDraftForUpdate) so the ownership check and a concurrent write
// race nothing. `items`, when present, replaces the whole line set
// (delete + insert, the same pattern stock.UpdatePurchase's own item
// replace uses). `customerId`/`discountReason`/`note` are D-35 nullable
// (optionalUUID/optionalString, drafts.go); `discountType`/
// `discountValue` are merged and validated as one pair by
// resolveDraftDiscountPatch before the UPDATE ever runs (review MAJOR).
// The resulting discount (only when `items` or the discount itself was
// touched — review MINOR, a PATCH touching neither leaves a stale
// discount alone rather than re-validating it) is checked against the
// (possibly just-replaced) items' subtotal the same way
// CreateSaleDraftTx validates a brand new one (409
// DISCOUNT_EXCEEDS_SUBTOTAL, D-57); a violation still rolls the whole
// transaction back, since httpx/sales.go only commits when this method
// returns no error.
func (h *Handler) UpdateSaleDraftTx(ctx context.Context, qtx *db.Queries, id uuid.UUID, body *gen.SaleDraftPatch) (gen.SaleDraft, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return gen.SaleDraft{}, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermSalesCreate); err != nil {
		return gen.SaleDraft{}, err
	}

	current, err := qtx.GetSaleDraftForUpdate(ctx, db.GetSaleDraftForUpdateParams{ShopID: authCtx.ShopID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.SaleDraft{}, apierr.NotFound("draft")
		}
		return gen.SaleDraft{}, fmt.Errorf("sales: get sale draft for update: %w", err)
	}
	if !canManageDraft(ctx, current.CreatedBy) {
		return gen.SaleDraft{}, apierr.Forbidden()
	}

	locale, loc, err := shopClock(ctx, qtx, authCtx.ShopID)
	if err != nil {
		return gen.SaleDraft{}, err
	}
	now := h.svc.now()
	defs, err := qtx.ListAttributeDefinitions(ctx, db.ListAttributeDefinitionsParams{ShopID: authCtx.ShopID, Locale: locale})
	if err != nil {
		return gen.SaleDraft{}, fmt.Errorf("sales: list attribute definitions: %w", err)
	}

	if body.LocationId != nil {
		if _, err := qtx.GetLocation(ctx, db.GetLocationParams{ShopID: authCtx.ShopID, ID: *body.LocationId}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return gen.SaleDraft{}, apierr.NotFound("location")
			}
			return gen.SaleDraft{}, fmt.Errorf("sales: get location: %w", err)
		}
	}

	customerField := optionalUUID(body.CustomerId)
	if customerField != nil && *customerField != nil {
		if _, err := qtx.GetCustomer(ctx, db.GetCustomerParams{ShopID: authCtx.ShopID, ID: **customerField}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return gen.SaleDraft{}, apierr.NotFound("customer")
			}
			return gen.SaleDraft{}, fmt.Errorf("sales: get customer: %w", err)
		}
	}

	replaceItems := body.Items != nil
	var newItems []saleLineInput
	if replaceItems {
		if len(*body.Items) == 0 {
			return gen.SaleDraft{}, apierr.Validation(map[string]string{"items": "required"})
		}
		if len(*body.Items) > maxSaleItems {
			return gen.SaleDraft{}, apierr.Validation(map[string]string{"items": "too_long"})
		}
		newItems, err = resolveSaleItems(ctx, qtx, authCtx.ShopID, *body.Items, now, loc)
		if err != nil {
			return gen.SaleDraft{}, err
		}
	}

	discountPatch, err := resolveDraftDiscountPatch(body.DiscountType, body.DiscountValue, current)
	if err != nil {
		return gen.SaleDraft{}, err
	}

	// subtotal to validate the resulting discount against — only
	// computed when there is something to validate (replaceItems or the
	// discount itself was touched, review MINOR): the newly-submitted
	// items when items is being replaced, else the currently-stored ones
	// repriced at this instant (priceDraftItemsBatch), same D-67 rule
	// either way. A PATCH touching neither skips this entirely — no
	// pricing lookups, no discount re-check — so it can never fail over
	// an already-stale discount it did not itself introduce.
	needsDiscountCheck := replaceItems || discountPatch.touched
	subtotal := decimal.Zero
	if needsDiscountCheck {
		if replaceItems {
			for _, l := range newItems {
				subtotal = subtotal.Add(l.lineTotal)
			}
		} else {
			byDraft, err := priceDraftItemsBatch(ctx, qtx, authCtx.ShopID, []uuid.UUID{id}, locale, now, loc, defs)
			if err != nil {
				return gen.SaleDraft{}, err
			}
			for _, l := range byDraft[id] {
				subtotal = subtotal.Add(l.lineTotal)
			}
		}
	}

	params := db.UpdateSaleDraftParams{ShopID: authCtx.ShopID, ID: id, LocationID: body.LocationId}
	if customerField != nil {
		params.ClearCustomer = *customerField == nil
		params.CustomerID = *customerField
	}
	if discountPatch.touched {
		params.ClearDiscount = discountPatch.clear
		params.DiscountType = discountPatch.typ
		params.DiscountValue = discountPatch.value
	}

	discountReasonField := optionalString(body.DiscountReason)
	if discountReasonField != nil {
		params.ClearDiscountReason = *discountReasonField == nil
		params.DiscountReason = *discountReasonField
	}
	noteField := optionalString(body.Note)
	if noteField != nil {
		params.ClearNote = *noteField == nil
		params.Note = *noteField
	}

	updated, err := qtx.UpdateSaleDraft(ctx, params)
	if err != nil {
		return gen.SaleDraft{}, fmt.Errorf("sales: update sale draft: %w", err)
	}

	if replaceItems {
		if _, err := qtx.DeleteSaleDraftItems(ctx, db.DeleteSaleDraftItemsParams{ShopID: authCtx.ShopID, SaleDraftID: id}); err != nil {
			return gen.SaleDraft{}, fmt.Errorf("sales: delete sale draft items: %w", err)
		}
		for i, l := range newItems {
			if _, err := qtx.InsertSaleDraftItem(ctx, db.InsertSaleDraftItemParams{
				ID: newID(), ShopID: authCtx.ShopID, SaleDraftID: id, VariantID: l.variantID,
				Qty: money.ToNumeric(l.qty), Position: int32(i),
			}); err != nil {
				return gen.SaleDraft{}, fmt.Errorf("sales: insert sale draft item: %w", err)
			}
		}
	}

	if needsDiscountCheck && updated.DiscountType != nil {
		disc, err := draftDiscountFromRow(updated.DiscountType, updated.DiscountValue)
		if err != nil {
			return gen.SaleDraft{}, err
		}
		amount, err := computeDiscountAmount(disc, subtotal)
		if err != nil {
			return gen.SaleDraft{}, err
		}
		if amount.GreaterThan(subtotal) {
			return gen.SaleDraft{}, errDiscountExceedsSubtotal
		}
	}

	createdByName, err := resolveCreatedByName(ctx, qtx, authCtx.ShopID, updated.CreatedBy)
	if err != nil {
		return gen.SaleDraft{}, err
	}
	customerName, err := resolveCustomerName(ctx, qtx, authCtx.ShopID, updated.CustomerID)
	if err != nil {
		return gen.SaleDraft{}, err
	}
	return buildSaleDraftResponse(ctx, qtx, authCtx.ShopID, updated, createdByName, customerName, now, loc, locale, defs)
}

// DeleteSaleDraftTx deletes a draft (DELETE /sales/drafts/{id}, D-89):
// hard delete, cascading its items (ON DELETE CASCADE,
// 0017_sale_drafts.sql) — no ledger effect, since a draft never moved
// stock (D-88). Requires sales.create and either the draft's own creator
// or manager+ (canManageDraft), checked after the row is locked
// (GetSaleDraftForUpdate) the same way UpdateSaleDraftTx does.
func (h *Handler) DeleteSaleDraftTx(ctx context.Context, qtx *db.Queries, id uuid.UUID) error {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermSalesCreate); err != nil {
		return err
	}

	current, err := qtx.GetSaleDraftForUpdate(ctx, db.GetSaleDraftForUpdateParams{ShopID: authCtx.ShopID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.NotFound("draft")
		}
		return fmt.Errorf("sales: get sale draft for update: %w", err)
	}
	if !canManageDraft(ctx, current.CreatedBy) {
		return apierr.Forbidden()
	}

	if _, err := qtx.DeleteSaleDraft(ctx, db.DeleteSaleDraftParams{ShopID: authCtx.ShopID, ID: id}); err != nil {
		return fmt.Errorf("sales: delete sale draft: %w", err)
	}
	return nil
}

// CompleteSaleDraftTx completes draftID (POST
// /sales/drafts/{id}/complete, D-87, D-96): locks the draft
// (GetSaleDraftForUpdate) against a concurrent second completion or
// edit — a second, concurrent CompleteSaleDraftTx call for the same id
// blocks on that lock until the first commits (deleting the draft) or
// rolls back, so at most one of two racing completions ever succeeds;
// the loser's own GetSaleDraftForUpdate then simply finds no row and
// answers 404 "draft", the same as completing an already-completed one
// — never a double sale, never a double movement. Every line is priced
// and availability-checked in one batch (priceDraftItemsBatch,
// drafts.go); an unavailable line (its variant or product has gone
// inactive or was soft-deleted since the draft was created or last
// read) fails the whole completion with 422 VALIDATION_FAILED naming it
// (errDraftLineUnavailable, review CRITICAL) before CreateSaleTx (create.go)
// ever runs — CreateSaleTx's own resolveSaleItems would otherwise 404 it
// generically, losing the "which line" detail D-88 promises. Once every
// line is available, this builds an equivalent SaleCreate from the
// draft's stored location/customer/discount/note/lines and the request's
// paymentMethod and hands it to CreateSaleTx completely unchanged — the
// same server-side price/stock/discount recomputation a real
// `POST /sales` performs (D-56/D-67), including its own 409
// STOCK_INSUFFICIENT/DISCOUNT_EXCEEDS_SUBTOTAL checks — then deletes the
// draft (cascading its items) in the same transaction, so a completion
// and its draft's disappearance are atomic: a failed CreateSaleTx call
// returns before the delete ever runs, and any error from either step
// rolls the whole transaction back (httpx/sales.go only commits — and
// only stores the Idempotency-Key row — once this method returns no
// error), so the draft is left intact and no sale exists on any failure
// path. Requires sales.create, the same as CreateSaleTx — D-96: any
// staff who may create a sale may complete any draft, regardless of its
// own creator (the narrower creator-or-manager+ rule, canManageDraft,
// governs PATCH/DELETE only, D-89); the sale is booked under the
// completing cashier (CreateSaleTx's own CashierID: authCtx.UserID).
func (h *Handler) CompleteSaleDraftTx(ctx context.Context, qtx *db.Queries, draftID uuid.UUID, body *gen.SaleDraftComplete) (gen.Sale, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return gen.Sale{}, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermSalesCreate); err != nil {
		return gen.Sale{}, err
	}
	if !validPaymentMethods[body.PaymentMethod] {
		return gen.Sale{}, apierr.Validation(map[string]string{"paymentMethod": "invalid"})
	}

	draft, err := qtx.GetSaleDraftForUpdate(ctx, db.GetSaleDraftForUpdateParams{ShopID: authCtx.ShopID, ID: draftID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.Sale{}, apierr.NotFound("draft")
		}
		return gen.Sale{}, fmt.Errorf("sales: get sale draft for update: %w", err)
	}

	locale, loc, err := shopClock(ctx, qtx, authCtx.ShopID)
	if err != nil {
		return gen.Sale{}, err
	}
	defs, err := qtx.ListAttributeDefinitions(ctx, db.ListAttributeDefinitionsParams{ShopID: authCtx.ShopID, Locale: locale})
	if err != nil {
		return gen.Sale{}, fmt.Errorf("sales: list attribute definitions: %w", err)
	}
	byDraft, err := priceDraftItemsBatch(ctx, qtx, authCtx.ShopID, []uuid.UUID{draftID}, locale, h.svc.now(), loc, defs)
	if err != nil {
		return gen.Sale{}, err
	}
	priced := byDraft[draftID]

	// Defensive line-count check (hard rule 8: never trust a computed
	// quantity without confirming it against the source of truth): the
	// draft's own stored item rows are the source of truth for how many
	// lines a sale must carry; priceDraftItemsBatch's JOIN-based query
	// should always return exactly that many rows (every FK it joins
	// through is NOT NULL and blocks a hard delete, drafts.go's own doc
	// comment), but a mismatch — fewer or more — must never silently
	// reach CreateSaleTx and sell the wrong line set. This is an internal
	// error (500), not a client-facing one: nothing in the request caused
	// it.
	rawItems, err := qtx.ListSaleDraftItems(ctx, db.ListSaleDraftItemsParams{ShopID: authCtx.ShopID, SaleDraftID: draftID})
	if err != nil {
		return gen.Sale{}, fmt.Errorf("sales: list sale draft items: %w", err)
	}
	if len(rawItems) != len(priced) {
		return gen.Sale{}, fmt.Errorf("sales: draft %s has %d stored item(s) but priceDraftItemsBatch priced %d — refusing to complete with a mismatched line count", draftID, len(rawItems), len(priced))
	}

	if len(priced) == 0 {
		return gen.Sale{}, apierr.Validation(map[string]string{"items": "required"})
	}

	fields := map[string]string{}
	for i, it := range priced {
		if !it.available {
			fields[fmt.Sprintf("items[%d].variantId", i)] = "invalid"
		}
	}
	if len(fields) > 0 {
		return gen.Sale{}, errDraftLineUnavailable(fields)
	}

	items := make([]gen.SaleItemCreate, len(priced))
	for i, it := range priced {
		items[i] = gen.SaleItemCreate{VariantId: it.variantID, Qty: qtyString(it.qty)}
	}

	discount, err := draftDiscountFromRow(draft.DiscountType, draft.DiscountValue)
	if err != nil {
		return gen.Sale{}, err
	}

	saleBody := &gen.SaleCreate{
		LocationId: draft.LocationID, CustomerId: draft.CustomerID, Items: items,
		Discount: discount, DiscountReason: draft.DiscountReason, Note: draft.Note,
		Payment: gen.SalePaymentCreate{Method: body.PaymentMethod},
	}

	sale, err := h.CreateSaleTx(ctx, qtx, saleBody)
	if err != nil {
		return gen.Sale{}, err
	}

	if _, err := qtx.DeleteSaleDraft(ctx, db.DeleteSaleDraftParams{ShopID: authCtx.ShopID, ID: draftID}); err != nil {
		return gen.Sale{}, fmt.Errorf("sales: delete sale draft: %w", err)
	}
	return sale, nil
}
