package sales

// This file: GET /sales (docs/05-API.md's ListSales description; D-63).
// SaleSummary carries no items/productName, so — unlike GetSale — this
// operation never needs a locale or the attribute-definition table.

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	oapitypes "github.com/oapi-codegen/runtime/types"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/pagination"
)

// defaultLimit and maxLimit bound GET /sales (docs/05-API.md § Conventions:
// "limit is 1-200, default 50") — mirrors stock.defaultLimit/maxLimit.
const (
	defaultLimit = 50
	maxLimit     = 200
)

// clampLimit resolves the requested `?limit=` query parameter (nil or
// non-positive means "use the default") to a bound in [1, maxLimit] —
// mirrors stock.clampLimit.
func clampLimit(requested *gen.Limit) int32 {
	limit := defaultLimit
	if requested != nil {
		limit = *requested
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	return int32(limit)
}

// decodeSalesCursor resolves GET /sales' `?cursor=` using internal/pagination
// directly — ListSalesForStaff/ForCashier's keyset, (completed_at, id)
// newest-first, is exactly the shape that package already codes for
// (mirrors stock.decodeMovementCursor).
func decodeSalesCursor(requested *gen.Cursor) (time.Time, uuid.UUID, error) {
	if requested == nil || *requested == "" {
		return time.Time{}, uuid.Nil, nil
	}
	return pagination.Decode(*requested)
}

// salesCursorPtr mirrors stock.movementCursorPtr, specialized to
// (completed_at, id).
func salesCursorPtr(completedAt time.Time, id uuid.UUID) (*time.Time, *uuid.UUID) {
	if completedAt.IsZero() {
		return nil, nil
	}
	return &completedAt, &id
}

// dayBounds anchors d's Y/M/D (an openapi_types.Date is always parsed at
// midnight UTC, docs/05-API.md's `YYYY-MM-DD` format — see
// oapi-codegen/runtime/types.Date.Bind) in loc: the shop's own timezone,
// not the UTC the wire value happens to carry — so `from`/`to` name a
// calendar day in the shop's own clock (docs/05-API.md's ListSales
// description), matching promoActive's own "anchor by Y/M/D, not by
// instant" reasoning (create.go).
func dayBounds(d oapitypes.Date, loc *time.Location) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
}

// saleDateRange resolves ListSalesParams' from/to into the half-open
// [from 00:00, to+1 day 00:00) timestamptz bounds ListSalesForStaff/
// ForCashier's own from/to query params expect (docs/05-API.md: "from"
// inclusive lower bound, "to" inclusive upper bound, both calendar days in
// the shop timezone) — nil when the caller did not send that bound at
// all. from > to is 400 VALIDATION_FAILED naming "to" (the later of the
// two, mirroring catalog's own promoFrom/promoTo ordering check).
func saleDateRange(from, to *oapitypes.Date, loc *time.Location) (*time.Time, *time.Time, error) {
	if from != nil && to != nil && from.After(to.Time) {
		return nil, nil, apierr.Validation(map[string]string{"to": "invalid"})
	}
	var fromPtr, toPtr *time.Time
	if from != nil {
		f := dayBounds(*from, loc)
		fromPtr = &f
	}
	if to != nil {
		t := dayBounds(*to, loc).AddDate(0, 0, 1)
		toPtr = &t
	}
	return fromPtr, toPtr, nil
}

// paginateSalesForStaff trims rows (fetched with limit+1) down to at most
// limit items and reports the opaque cursor for the next page — mirrors
// stock.paginateMovements, specialized to ListSalesForStaffRow's own
// (completed_at, id).
func paginateSalesForStaff(rows []db.ListSalesForStaffRow, limit int32) ([]db.ListSalesForStaffRow, *string) {
	if len(rows) <= int(limit) {
		return rows, nil
	}
	items := rows[:limit]
	last := items[len(items)-1]
	cursor := pagination.Encode(last.CompletedAt, last.ID)
	return items, &cursor
}

// paginateSalesForCashier is paginateSalesForStaff for
// ListSalesForCashierRow's identical (completed_at, id) shape.
func paginateSalesForCashier(rows []db.ListSalesForCashierRow, limit int32) ([]db.ListSalesForCashierRow, *string) {
	if len(rows) <= int(limit) {
		return rows, nil
	}
	items := rows[:limit]
	last := items[len(items)-1]
	cursor := pagination.Encode(last.CompletedAt, last.ID)
	return items, &cursor
}

// ListSales lists the shop's sales. Any authenticated staff — cashier,
// manager or owner — may list every sale for the whole shop and any day,
// not only their own (D-63); unitCost/margin never appear on this shape
// regardless (SaleSummary carries no items at all). Cursor-paginated,
// newest first.
func (h *Handler) ListSales(ctx context.Context, req gen.ListSalesRequestObject) (gen.ListSalesResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}

	shopRow, err := h.svc.q.GetShop(ctx, authCtx.ShopID)
	if err != nil {
		return nil, fmt.Errorf("sales: get shop: %w", err)
	}
	shopLoc, err := time.LoadLocation(shopRow.Timezone)
	if err != nil {
		return nil, fmt.Errorf("sales: load shop timezone: %w", err)
	}
	fromPtr, toPtr, err := saleDateRange(req.Params.From, req.Params.To, shopLoc)
	if err != nil {
		return nil, err
	}

	limit := clampLimit(req.Params.Limit)
	cCompletedAt, cID, err := decodeSalesCursor(req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	cursorCompletedAt, cursorID := salesCursorPtr(cCompletedAt, cID)

	var kind *db.SaleKind
	if req.Params.Kind != nil {
		k := db.SaleKind(*req.Params.Kind)
		kind = &k
	}
	var status *db.SaleStatus
	if req.Params.Status != nil {
		s := db.SaleStatus(*req.Params.Status)
		status = &s
	}

	genItems := make([]gen.SaleSummary, 0)
	var nextCursor *string
	// Both queries are cost-free (SaleSummary has no unitCost/margin field
	// at all) — the ForStaff/ForCashier split here is for rule-8 symmetry
	// with GetSale/buildSaleResponse, not because either query needs
	// filtering.
	if auth.Require(ctx, auth.PermCostRead) == nil {
		rows, err := h.svc.q.ListSalesForStaff(ctx, db.ListSalesForStaffParams{
			ShopID: authCtx.ShopID, From: fromPtr, To: toPtr,
			LocationID: req.Params.LocationId, CashierID: req.Params.CashierId, CustomerID: req.Params.CustomerId,
			Kind: kind, Status: status, CursorCompletedAt: cursorCompletedAt, CursorID: cursorID, Limit: limit + 1,
		})
		if err != nil {
			return nil, fmt.Errorf("sales: list sales for staff: %w", err)
		}
		var pageRows []db.ListSalesForStaffRow
		pageRows, nextCursor = paginateSalesForStaff(rows, limit)
		for _, r := range pageRows {
			g, err := toGenSaleSummary(saleSummaryFromStaffRow(r))
			if err != nil {
				return nil, err
			}
			genItems = append(genItems, g)
		}
	} else {
		rows, err := h.svc.q.ListSalesForCashier(ctx, db.ListSalesForCashierParams{
			ShopID: authCtx.ShopID, From: fromPtr, To: toPtr,
			LocationID: req.Params.LocationId, CashierID: req.Params.CashierId, CustomerID: req.Params.CustomerId,
			Kind: kind, Status: status, CursorCompletedAt: cursorCompletedAt, CursorID: cursorID, Limit: limit + 1,
		})
		if err != nil {
			return nil, fmt.Errorf("sales: list sales for cashier: %w", err)
		}
		var pageRows []db.ListSalesForCashierRow
		pageRows, nextCursor = paginateSalesForCashier(rows, limit)
		for _, r := range pageRows {
			g, err := toGenSaleSummary(saleSummaryFromCashierRow(r))
			if err != nil {
				return nil, err
			}
			genItems = append(genItems, g)
		}
	}

	return gen.ListSales200JSONResponse(gen.SaleList{Items: genItems, NextCursor: nullableString(nextCursor)}), nil
}
