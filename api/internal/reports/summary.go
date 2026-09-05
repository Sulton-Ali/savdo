package reports

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/money"
)

// GetSalesSummaryReport implements GET /reports/sales/summary
// (docs/00-DECISIONS.md D-55). Requires cashier+: manager+
// (auth.PermReportsRead) gets any date range, scoped to the whole shop
// unless locationId narrows it, with cost/margin (ADR-010 — no separate
// `cost.read` check exists below because every role holding
// PermReportsRead also holds auth.PermCostRead, docs/04-DATA-MODEL.md §
// 7 — "manager without cost.read" is not a reachable role). A cashier
// (auth.PermReportsOwnDay only) is answered for today in the shop
// timezone, that cashier's own sales only, ignoring every query parameter
// sent, without cost/margin, echoing the effective from/to/cashierId it
// used.
func (h *Handler) GetSalesSummaryReport(ctx context.Context, req gen.GetSalesSummaryReportRequestObject) (gen.GetSalesSummaryReportResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}

	// Permission comes before any database work — mirrors
	// ListSalesByProduct's ordering, and means an unauthorized caller
	// never costs a shop-row lookup.
	staff := auth.Require(ctx, auth.PermReportsRead) == nil
	if !staff {
		// Unreachable with today's three roles (owner/manager hold
		// PermReportsRead, cashier holds PermReportsOwnDay — every role
		// has one or the other; pinned by
		// auth.TestReportsReadImpliesCostRead's sibling invariant), kept
		// as a real check rather than an assumption: a future role with
		// neither gets 403, the same outcome auth.Require's own
		// zero-permission fallback would give it anywhere else in the
		// API.
		if err := auth.Require(ctx, auth.PermReportsOwnDay); err != nil {
			return nil, err
		}
	}

	shopRow, err := loadShop(ctx, h.svc.q, authCtx.ShopID)
	if err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(shopRow.Timezone)
	if err != nil {
		return nil, fmt.Errorf("reports: load shop timezone %q: %w", shopRow.Timezone, err)
	}

	if staff {
		return h.summaryForStaff(ctx, authCtx, req.Params, loc)
	}
	return h.summaryForCashier(ctx, authCtx, loc)
}

// summaryForStaff answers a manager+ caller: from/to are required
// (`YYYY-MM-DD`, already parsed by the generated request binder — an
// unparsable value never reaches here, docs/05-API.md § Conventions),
// from must not be after to, and locationId, if given, must belong to the
// shop.
func (h *Handler) summaryForStaff(ctx context.Context, authCtx auth.Context, params gen.GetSalesSummaryReportParams, loc *time.Location) (gen.GetSalesSummaryReportResponseObject, error) {
	fields := map[string]string{}
	if params.From == nil {
		fields["from"] = "required"
	}
	if params.To == nil {
		fields["to"] = "required"
	}
	if len(fields) > 0 {
		return nil, apierr.Validation(fields)
	}
	if params.To.Before(params.From.Time) {
		return nil, apierr.Validation(map[string]string{"to": "invalid"})
	}

	locationID, err := validateLocation(ctx, h.svc.q, authCtx.ShopID, params.LocationId)
	if err != nil {
		return nil, err
	}

	from, to := dayBounds(loc, *params.From, *params.To)
	row, err := h.svc.q.SalesSummaryForStaff(ctx, db.SalesSummaryForStaffParams{
		ShopID: authCtx.ShopID, From: from, To: to, LocationID: locationID,
	})
	if err != nil {
		return nil, fmt.Errorf("reports: sales summary for staff: %w", err)
	}

	resp, err := toStaffSummary(*params.From, *params.To, locationID, row)
	if err != nil {
		return nil, err
	}
	return gen.GetSalesSummaryReport200JSONResponse(resp), nil
}

// summaryForCashier answers a cashier caller: every query parameter is
// ignored (D-55) — the window is always today in loc, and cashierId is
// always the caller's own id.
func (h *Handler) summaryForCashier(ctx context.Context, authCtx auth.Context, loc *time.Location) (gen.GetSalesSummaryReportResponseObject, error) {
	from, to, day := todayBounds(loc, time.Now())
	cashierID := authCtx.UserID

	row, err := h.svc.q.SalesSummaryForCashier(ctx, db.SalesSummaryForCashierParams{
		ShopID: authCtx.ShopID, From: from, To: to, CashierID: &cashierID,
	})
	if err != nil {
		return nil, fmt.Errorf("reports: sales summary for cashier: %w", err)
	}

	resp, err := toCashierSummary(day, cashierID, row)
	if err != nil {
		return nil, err
	}
	return gen.GetSalesSummaryReport200JSONResponse(resp), nil
}

// toStaffSummary converts SalesSummaryForStaff's row plus the request's
// own from/to/locationId into the wire shape. netRevenue and margin are
// computed here, not by SQL (D-64/D-55: netRevenue = revenue - refunds,
// margin = netRevenue - cost) — the query returns only the four sums SQL
// aggregates cleanly (revenue, refunds, discounts, cost); this arithmetic
// needs nothing SQL does better, and keeping it here means both this and
// SalesSummaryForCashier's response share one formula end to end.
func toStaffSummary(from, to openapi_types.Date, locationID *uuid.UUID, row db.SalesSummaryForStaffRow) (gen.SalesSummaryReport, error) {
	revenueStr, revenue, err := requiredDecimal(row.Revenue)
	if err != nil {
		return gen.SalesSummaryReport{}, err
	}
	discountsStr, _, err := requiredDecimal(row.Discounts)
	if err != nil {
		return gen.SalesSummaryReport{}, err
	}
	refundsStr, refunds, err := requiredDecimal(row.Refunds)
	if err != nil {
		return gen.SalesSummaryReport{}, err
	}
	costStr, cost, err := requiredDecimal(row.Cost)
	if err != nil {
		return gen.SalesSummaryReport{}, err
	}

	netRevenue := revenue.Sub(refunds)
	margin := netRevenue.Sub(cost)
	marginStr := money.String(margin)

	return gen.SalesSummaryReport{
		From: from, To: to,
		LocationId:   nullableUUID(locationID),
		CashierId:    nullableUUID(nil),
		SalesCount:   int(row.SalesCount),
		ReturnsCount: int(row.ReturnsCount),
		Revenue:      revenueStr,
		Discounts:    discountsStr,
		Refunds:      refundsStr,
		NetRevenue:   money.String(netRevenue),
		Cost:         &costStr,
		Margin:       &marginStr,
	}, nil
}

// toCashierSummary converts SalesSummaryForCashier's row (no cost column
// at all — D-63/hard rule 5, filtered at the query layer, not here) into
// the wire shape, echoing day as both from and to and cashierID as
// cashierId (D-55).
func toCashierSummary(day openapi_types.Date, cashierID uuid.UUID, row db.SalesSummaryForCashierRow) (gen.SalesSummaryReport, error) {
	revenueStr, revenue, err := requiredDecimal(row.Revenue)
	if err != nil {
		return gen.SalesSummaryReport{}, err
	}
	discountsStr, _, err := requiredDecimal(row.Discounts)
	if err != nil {
		return gen.SalesSummaryReport{}, err
	}
	refundsStr, refunds, err := requiredDecimal(row.Refunds)
	if err != nil {
		return gen.SalesSummaryReport{}, err
	}

	netRevenue := revenue.Sub(refunds)

	return gen.SalesSummaryReport{
		From: day, To: day,
		LocationId:   nullableUUID(nil),
		CashierId:    nullableUUID(&cashierID),
		SalesCount:   int(row.SalesCount),
		ReturnsCount: int(row.ReturnsCount),
		Revenue:      revenueStr,
		Discounts:    discountsStr,
		Refunds:      refundsStr,
		NetRevenue:   money.String(netRevenue),
	}, nil
}
