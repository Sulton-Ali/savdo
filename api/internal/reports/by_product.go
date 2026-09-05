package reports

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/catalog"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/money"
)

// ListSalesByProduct implements GET /reports/sales/by-product
// (docs/00-DECISIONS.md D-55). Manager+ only (auth.PermReportsRead) — a
// cashier gets 403, there is no own-day variant of this report. `from`/
// `to` are required by the contract itself (non-pointer
// ListSalesByProductParams fields — an absent or unparsable value is
// already a 400 VALIDATION_FAILED before this method runs,
// docs/05-API.md § Conventions); this method still checks from <= to,
// which the generated binder cannot.
func (h *Handler) ListSalesByProduct(ctx context.Context, req gen.ListSalesByProductRequestObject) (gen.ListSalesByProductResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermReportsRead); err != nil {
		return nil, err
	}

	if req.Params.To.Before(req.Params.From.Time) {
		return nil, apierr.Validation(map[string]string{"to": "invalid"})
	}

	shopRow, err := loadShop(ctx, h.svc.q, authCtx.ShopID)
	if err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(shopRow.Timezone)
	if err != nil {
		return nil, fmt.Errorf("reports: load shop timezone %q: %w", shopRow.Timezone, err)
	}

	locationID, err := validateLocation(ctx, h.svc.q, authCtx.ShopID, req.Params.LocationId)
	if err != nil {
		return nil, err
	}

	cursorRevenue, cursorProductID, err := decodeByProductCursor(req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	limit := clampLimit(req.Params.Limit)
	locale := catalog.ResolveLocale(ctx, shopRow.DefaultLocale)

	from, to := dayBounds(loc, req.Params.From, req.Params.To)
	rows, err := h.svc.q.SalesByProduct(ctx, db.SalesByProductParams{
		CursorRevenue: cursorRevenue, CursorProductID: cursorProductID, Limit: limit + 1,
		Locale: locale, ShopID: authCtx.ShopID, From: from, To: to, LocationID: locationID,
	})
	if err != nil {
		return nil, fmt.Errorf("reports: sales by product: %w", err)
	}

	items, nextCursor, err := paginateByProduct(rows, limit)
	if err != nil {
		return nil, err
	}
	return gen.ListSalesByProduct200JSONResponse(gen.SalesByProductList{
		Items: items, NextCursor: nullableString(nextCursor),
	}), nil
}

// paginateByProduct trims rows (fetched with limit+1) down to at most
// limit items, converting each to the wire shape, and reports the opaque
// cursor for the next page — non-nil exactly when a limit+1'th row proved
// more data exists. Mirrors stock.paginateLow, specialized to
// SalesByProductRow's (revenue, product_id) key and its own conversion
// step (toByProductRow can fail on a malformed NUMERIC, unlike
// stock.toGenLow's simpler qty-only rows).
func paginateByProduct(rows []db.SalesByProductRow, limit int32) ([]gen.SalesByProductRow, *string, error) {
	// Compare in int (widening limit, never narrowing len(rows)) — mirrors
	// stock.paginateLow's own reasoning (gosec G115).
	hasMore := len(rows) > int(limit)
	if hasMore {
		rows = rows[:limit]
	}

	items := make([]gen.SalesByProductRow, len(rows))
	for i, r := range rows {
		item, revenue, err := toByProductRow(r)
		if err != nil {
			return nil, nil, err
		}
		items[i] = item
		if hasMore && i == len(rows)-1 {
			cursor := encodeByProductCursor(revenue, r.ProductID)
			return items, &cursor, nil
		}
	}
	return items, nil, nil
}

// toByProductRow converts one SalesByProduct row to the wire shape,
// returning the row's own revenue decimal alongside it so
// paginateByProduct's cursor encoder does not need to re-parse the string
// it just built.
func toByProductRow(r db.SalesByProductRow) (gen.SalesByProductRow, decimal.Decimal, error) {
	qtySold, err := requiredQty(r.QtySold)
	if err != nil {
		return gen.SalesByProductRow{}, decimal.Decimal{}, err
	}
	qtyReturned, err := requiredQty(r.QtyReturned)
	if err != nil {
		return gen.SalesByProductRow{}, decimal.Decimal{}, err
	}
	revenueStr, revenue, err := requiredDecimal(r.Revenue)
	if err != nil {
		return gen.SalesByProductRow{}, decimal.Decimal{}, err
	}
	costStr, cost, err := requiredDecimal(r.Cost)
	if err != nil {
		return gen.SalesByProductRow{}, decimal.Decimal{}, err
	}
	marginStr := money.String(revenue.Sub(cost))

	return gen.SalesByProductRow{
		ProductId: r.ProductID, ProductName: r.ProductName,
		QtySold: qtySold, QtyReturned: qtyReturned,
		Revenue: revenueStr, Cost: &costStr, Margin: &marginStr,
	}, revenue, nil
}
