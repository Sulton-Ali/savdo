package sales

// This file: POST /sales/drafts, PATCH /sales/drafts/{id},
// DELETE /sales/drafts/{id} and POST /sales/drafts/{id}/complete
// (docs/00-DECISIONS.md D-87..D-89; docs/04-DATA-MODEL.md § 4;
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
// discount at all.
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
			ID: newID(), ShopID: authCtx.ShopID, DraftID: draft.ID, VariantID: l.variantID,
			Qty: money.ToNumeric(l.qty), Position: int32(i),
		}); err != nil {
			return gen.SaleDraft{}, fmt.Errorf("sales: insert sale draft item: %w", err)
		}
	}

	defs, err := qtx.ListAttributeDefinitions(ctx, db.ListAttributeDefinitionsParams{ShopID: authCtx.ShopID, Locale: locale})
	if err != nil {
		return gen.SaleDraft{}, fmt.Errorf("sales: list attribute definitions: %w", err)
	}
	return h.buildSaleDraftResponse(ctx, qtx, authCtx.ShopID, draft, now, loc, locale, defs)
}

// UpdateSaleDraftTx edits a draft (PATCH /sales/drafts/{id}, D-87).
// Requires sales.create and either the draft's own creator or manager+
// (canManageDraft, D-89), checked only after the row is locked
// (GetSaleDraftForUpdate) so the ownership check and a concurrent write
// race nothing. `items`, when present, replaces the whole line set
// (delete + insert, the same pattern stock.UpdatePurchase's own item
// replace uses). `customerId`/`discountReason`/`note` are D-35 nullable
// (optionalUUID/optionalString, drafts.go); `discountType`/
// `discountValue` clear together as a pair on an explicit `null` on
// either one (UpdateSaleDraftParams' own ClearDiscount flag) — the same
// rule ProductPatch's promo fields already use, docs/05-API.md's promo
// bullet. Whatever the UPDATE actually leaves stored (via its own
// COALESCE-with-current logic, sale_drafts.sql's own doc comment) is
// re-validated against the (possibly just-replaced) items' subtotal the
// same way CreateSaleDraftTx validates a brand new one (409
// DISCOUNT_EXCEEDS_SUBTOTAL, D-57) — checked after the write so the
// discount actually being validated is the merged one, not a
// hand-replicated guess at COALESCE's own result; a violation still rolls
// the whole transaction back, since httpx/sales.go only commits when this
// method returns no error.
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

	// subtotal to validate the resulting discount against: the
	// newly-submitted items when items is being replaced, else the
	// currently-stored ones repriced at this instant (priceDraftItems) —
	// same D-67 rule either way.
	subtotal := decimal.Zero
	if replaceItems {
		for _, l := range newItems {
			subtotal = subtotal.Add(l.lineTotal)
		}
	} else {
		existingRows, err := qtx.ListSaleDraftItems(ctx, db.ListSaleDraftItemsParams{ShopID: authCtx.ShopID, DraftID: id})
		if err != nil {
			return gen.SaleDraft{}, fmt.Errorf("sales: list sale draft items: %w", err)
		}
		priced, err := priceDraftItems(ctx, qtx, authCtx.ShopID, existingRows, defs, locale, now, loc)
		if err != nil {
			return gen.SaleDraft{}, err
		}
		for _, l := range priced {
			subtotal = subtotal.Add(l.lineTotal)
		}
	}

	params := db.UpdateSaleDraftParams{ShopID: authCtx.ShopID, ID: id, LocationID: body.LocationId}
	if customerField != nil {
		params.ClearCustomer = *customerField == nil
		params.CustomerID = *customerField
	}

	discountTypeField := optionalString(body.DiscountType)
	discountValueField := optionalString(body.DiscountValue)
	clearDiscount := (discountTypeField != nil && *discountTypeField == nil) || (discountValueField != nil && *discountValueField == nil)
	params.ClearDiscount = clearDiscount
	if !clearDiscount {
		if discountTypeField != nil && *discountTypeField != nil {
			t := db.DiscountType(**discountTypeField)
			if t != db.DiscountTypePercent && t != db.DiscountTypeFixed {
				return gen.SaleDraft{}, apierr.Validation(map[string]string{"discountType": "invalid"})
			}
			params.DiscountType = &t
		}
		if discountValueField != nil && *discountValueField != nil {
			v, apiErr := money.ParseAmount(**discountValueField)
			if apiErr != nil {
				return gen.SaleDraft{}, apiErr
			}
			params.DiscountValue = money.ToNumeric(v)
		}
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
		if _, err := qtx.DeleteSaleDraftItems(ctx, db.DeleteSaleDraftItemsParams{ShopID: authCtx.ShopID, DraftID: id}); err != nil {
			return gen.SaleDraft{}, fmt.Errorf("sales: delete sale draft items: %w", err)
		}
		for i, l := range newItems {
			if _, err := qtx.InsertSaleDraftItem(ctx, db.InsertSaleDraftItemParams{
				ID: newID(), ShopID: authCtx.ShopID, DraftID: id, VariantID: l.variantID,
				Qty: money.ToNumeric(l.qty), Position: int32(i),
			}); err != nil {
				return gen.SaleDraft{}, fmt.Errorf("sales: insert sale draft item: %w", err)
			}
		}
	}

	if updated.DiscountType != nil {
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

	return h.buildSaleDraftResponse(ctx, qtx, authCtx.ShopID, updated, now, loc, locale, defs)
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
// /sales/drafts/{id}/complete, D-87): locks the draft
// (GetSaleDraftForUpdate) against a concurrent second completion or
// edit, builds an equivalent SaleCreate from its stored
// location/customer/discount/note/lines and the request's
// paymentMethod, and hands it to CreateSaleTx (create.go) completely
// unchanged — the same server-side price/stock/discount recomputation a
// real `POST /sales` performs (D-56/D-67), including its own 409
// STOCK_INSUFFICIENT/DISCOUNT_EXCEEDS_SUBTOTAL checks (D-88's "the
// client shows which line and lets the user edit" is exactly
// CreateSaleTx's own existing error shape, reused verbatim rather than
// reimplemented) — then deletes the draft (cascading its items) in the
// same transaction, so a completion and its draft's disappearance are
// atomic: a failed CreateSaleTx call returns before the delete ever
// runs, and any error from either step rolls the whole transaction back
// (httpx/sales.go only commits — and only stores the Idempotency-Key
// row — once this method returns no error), so the draft is left
// intact and no sale exists on any failure path. Requires sales.create,
// the same as CreateSaleTx — any staff who may create a sale may
// complete any draft, not only its own creator (D-87's "shared" drafts).
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

	rows, err := qtx.ListSaleDraftItems(ctx, db.ListSaleDraftItemsParams{ShopID: authCtx.ShopID, DraftID: draftID})
	if err != nil {
		return gen.Sale{}, fmt.Errorf("sales: list sale draft items: %w", err)
	}
	if len(rows) == 0 {
		return gen.Sale{}, apierr.Validation(map[string]string{"items": "required"})
	}
	items := make([]gen.SaleItemCreate, len(rows))
	for i, r := range rows {
		qty, err := money.FromNumeric(r.Qty)
		if err != nil {
			return gen.Sale{}, fmt.Errorf("sales: draft item qty: %w", err)
		}
		items[i] = gen.SaleItemCreate{VariantId: r.VariantID, Qty: qtyString(qty)}
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
