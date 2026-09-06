package sales

// This file: GET /sales/drafts and GET /sales/drafts/{id}
// (docs/00-DECISIONS.md D-87; docs/05-API.md's two `GET` `/sales/drafts`
// rows). Both are plain reads on h.svc.q — no transaction, no
// Idempotency-Key — forwarded from httpx/sales.go the same way
// GetSale/ListSales are (sales.go's own doc comment).

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/pagination"
)

// GetSaleDraft gets a draft by id. Requires sales.create (cashier+, the
// same permission CreateSale gates on) — any staff who may create a sale
// may open any draft for the whole shop, shared across devices and
// staff (D-87); 404 for another shop's id or an id that never existed.
func (h *Handler) GetSaleDraft(ctx context.Context, req gen.GetSaleDraftRequestObject) (gen.GetSaleDraftResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermSalesCreate); err != nil {
		return nil, err
	}

	row, err := h.svc.q.GetSaleDraft(ctx, db.GetSaleDraftParams{ShopID: authCtx.ShopID, ID: req.Id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("draft")
		}
		return nil, fmt.Errorf("sales: get sale draft: %w", err)
	}
	draft := saleDraftFromGetRow(row)

	locale, loc, err := shopClock(ctx, h.svc.q, authCtx.ShopID)
	if err != nil {
		return nil, err
	}
	defs, err := h.svc.q.ListAttributeDefinitions(ctx, db.ListAttributeDefinitionsParams{ShopID: authCtx.ShopID, Locale: locale})
	if err != nil {
		return nil, fmt.Errorf("sales: list attribute definitions: %w", err)
	}
	resp, err := buildSaleDraftResponse(ctx, h.svc.q, authCtx.ShopID, draft, row.CreatedByName, h.svc.now(), loc, locale, defs)
	if err != nil {
		return nil, err
	}
	return gen.GetSaleDraft200JSONResponse(resp), nil
}

// saleDraftFromGetRow strips GetSaleDraft's own LEFT JOIN column
// (created_by_name) down to the plain db.SaleDraft shape every other
// draft query already returns, so buildSaleDraftResponse/assembleSaleDraft
// take one struct type regardless of how their caller resolved the name —
// mirrors ListStockMovements' own db.StockMovement{...} conversion off
// ListMovementsWithCreatedByNameRow (stock/movements.go).
func saleDraftFromGetRow(r db.GetSaleDraftRow) db.SaleDraft {
	return db.SaleDraft{
		ID: r.ID, ShopID: r.ShopID, LocationID: r.LocationID, CustomerID: r.CustomerID,
		DiscountType: r.DiscountType, DiscountValue: r.DiscountValue, DiscountReason: r.DiscountReason,
		Note: r.Note, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// saleDraftFromListRow is saleDraftFromGetRow for ListSaleDrafts' own
// identically-shaped LEFT JOIN row.
func saleDraftFromListRow(r db.ListSaleDraftsRow) db.SaleDraft {
	return db.SaleDraft{
		ID: r.ID, ShopID: r.ShopID, LocationID: r.LocationID, CustomerID: r.CustomerID,
		DiscountType: r.DiscountType, DiscountValue: r.DiscountValue, DiscountReason: r.DiscountReason,
		Note: r.Note, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// paginateSaleDrafts trims rows (fetched with limit+1) down to at most
// limit items and reports the opaque cursor for the next page — mirrors
// paginateSalesForStaff (list.go), specialized to db.SaleDraft's own
// (created_at, id) keyset.
func paginateSaleDrafts(rows []db.ListSaleDraftsRow, limit int32) ([]db.ListSaleDraftsRow, *string) {
	if len(rows) <= int(limit) {
		return rows, nil
	}
	items := rows[:limit]
	last := items[len(items)-1]
	cursor := pagination.Encode(last.CreatedAt, last.ID)
	return items, &cursor
}

// ListSaleDrafts lists the shop's draft sales, newest first. Requires
// sales.create (cashier+); optional `createdBy` narrows to one creator's
// drafts (docs/05-API.md). Cursor-paginated on (created_at, id) — the
// same keyset shape ListSales already uses, decoded through the same
// internal/pagination helpers (decodeSalesCursor/salesCursorPtr, list.go,
// specialized to `completed_at` there but generic enough to reuse for
// `created_at` here — both are a plain (time.Time, uuid.UUID) keyset).
// maxLimit/defaultLimit (list.go) are unchanged — this only fixes how
// many *queries* one page costs, not the page size itself: every
// draft's items are priced in one batched call
// (priceDraftItemsBatch/ListSaleDraftItemsForPricing, drafts.go) across
// the whole page, not one GetVariantForCashier/GetProductForCashier pair
// per line per draft (review MAJOR: that was an N+1 — up to
// `2 * maxLimit * (lines per draft)` extra round trips on a full page).
func (h *Handler) ListSaleDrafts(ctx context.Context, req gen.ListSaleDraftsRequestObject) (gen.ListSaleDraftsResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermSalesCreate); err != nil {
		return nil, err
	}

	limit := clampLimit(req.Params.Limit)
	cCreatedAt, cID, err := decodeSalesCursor(req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	cursorCreatedAt, cursorID := salesCursorPtr(cCreatedAt, cID)

	rows, err := h.svc.q.ListSaleDrafts(ctx, db.ListSaleDraftsParams{
		ShopID: authCtx.ShopID, CreatedBy: req.Params.CreatedBy,
		CursorCreatedAt: cursorCreatedAt, CursorID: cursorID, Limit: limit + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("sales: list sale drafts: %w", err)
	}
	pageRows, nextCursor := paginateSaleDrafts(rows, limit)

	locale, loc, err := shopClock(ctx, h.svc.q, authCtx.ShopID)
	if err != nil {
		return nil, err
	}
	defs, err := h.svc.q.ListAttributeDefinitions(ctx, db.ListAttributeDefinitionsParams{ShopID: authCtx.ShopID, Locale: locale})
	if err != nil {
		return nil, fmt.Errorf("sales: list attribute definitions: %w", err)
	}

	draftIDs := make([]uuid.UUID, len(pageRows))
	for i, r := range pageRows {
		draftIDs[i] = r.ID
	}
	now := h.svc.now()
	byDraft, err := priceDraftItemsBatch(ctx, h.svc.q, authCtx.ShopID, draftIDs, locale, now, loc, defs)
	if err != nil {
		return nil, err
	}

	items := make([]gen.SaleDraft, len(pageRows))
	for i, r := range pageRows {
		g, err := assembleSaleDraft(saleDraftFromListRow(r), byDraft[r.ID], r.CreatedByName)
		if err != nil {
			return nil, err
		}
		items[i] = g
	}
	return gen.ListSaleDrafts200JSONResponse(gen.SaleDraftList{Items: items, NextCursor: nullableString(nextCursor)}), nil
}
