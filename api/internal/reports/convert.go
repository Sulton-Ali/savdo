package reports

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oapi-codegen/nullable"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/money"
)

// defaultLimit and maxLimit bound GET /reports/sales/by-product, the only
// paginated operation in this package — mirrors stock.defaultLimit/
// maxLimit and every other collection endpoint (docs/05-API.md §
// Conventions: "limit is 1-200, default 50").
const (
	defaultLimit = 50
	maxLimit     = 200
)

// clampLimit resolves the requested `?limit=` query parameter (nil or
// non-positive means "use the default") to a bound in [1, maxLimit].
// Mirrors stock.clampLimit.
func clampLimit(requested *int) int32 {
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

// requiredDecimal converts a NOT NULL NUMERIC column's scanned value to
// its wire decimal string, at money's 2-decimal-place precision — every
// money field this package renders (revenue, discounts, refunds,
// netRevenue, cost, margin) is a NUMERIC(14,2), never NULL, per
// api/db/queries/reports.sql's own COALESCE(..., 0) on every aggregate.
func requiredDecimal(n pgtype.Numeric) (string, decimal.Decimal, error) {
	d, err := money.FromNumeric(n)
	if err != nil {
		return "", decimal.Decimal{}, fmt.Errorf("reports: numeric amount: %w", err)
	}
	return money.String(d), d, nil
}

// requiredQty converts a NOT NULL NUMERIC(12,3) column (qty_sold,
// qty_returned) to its wire string at quantity precision — mirrors
// stock.qtyString/numericQtyString.
func requiredQty(n pgtype.Numeric) (string, error) {
	d, err := money.FromNumeric(n)
	if err != nil {
		return "", fmt.Errorf("reports: numeric qty: %w", err)
	}
	return d.StringFixed(3), nil
}

// nullableUUID converts a *uuid.UUID (nil = absent) to the tri-state
// nullable.Nullable the generated SalesSummaryReport.locationId/cashierId
// fields use — mirrors stock.nullableUUID/catalog.nullableUUID.
// openapi_types.UUID is a type alias for uuid.UUID (`go doc
// github.com/oapi-codegen/runtime/types.UUID`), so this satisfies both.
func nullableUUID(v *uuid.UUID) nullable.Nullable[openapi_types.UUID] {
	if v == nil {
		return nullable.NewNullNullable[openapi_types.UUID]()
	}
	return nullable.NewNullableWithValue(*v)
}

// nullableString converts a *string (nil = absent) to the tri-state
// nullable.Nullable SalesByProductList.nextCursor uses — mirrors
// stock.nullableString.
func nullableString(v *string) nullable.Nullable[string] {
	if v == nil {
		return nullable.NewNullNullable[string]()
	}
	return nullable.NewNullableWithValue(*v)
}

// dateOf builds an openapi_types.Date for year/month/day at UTC midnight —
// the same representation openapi_types.Date.Bind produces parsing a
// `YYYY-MM-DD` request parameter (time.Parse defaults to UTC), so a value
// this package builds itself (the cashier report's "today") round-trips
// through JSON exactly like one bound off the wire.
func dateOf(year int, month time.Month, day int) openapi_types.Date {
	return openapi_types.Date{Time: time.Date(year, month, day, 0, 0, 0, 0, time.UTC)}
}

// dayBounds turns two inclusive calendar dates (from, to — already parsed
// by oapi-codegen off `YYYY-MM-DD` request parameters, so their Time value
// is UTC midnight of that day regardless of loc) into the half-open
// [from 00:00, to+1 day 00:00) timestamptz bounds the sqlc queries filter
// on, computed in loc (the shop's own timezone — § 04-DATA-MODEL.md rule
// 9: shop timezone is applied in reports only). Only the Y/M/D components
// of from/to are used; time.Date re-normalizes them into loc, which is
// what makes a `to+1 day` boundary in Tashkent differ from one computed in
// UTC (the timezone-boundary trap this whole package exists to avoid).
func dayBounds(loc *time.Location, from, to openapi_types.Date) (time.Time, time.Time) {
	fy, fm, fd := from.Date()
	ty, tm, td := to.Date()
	start := time.Date(fy, fm, fd, 0, 0, 0, 0, loc)
	end := time.Date(ty, tm, td+1, 0, 0, 0, 0, loc)
	return start, end
}

// todayBounds is dayBounds for the cashier report's forced "today" window
// (D-55): loc's current wall-clock date, [00:00, 24:00) in loc. Returns
// the calendar date too, for GetSalesSummaryReport to echo back as the
// effective `from`/`to` (D-55's "echoes the effective from/to/cashierId").
func todayBounds(loc *time.Location, now time.Time) (start, end time.Time, day openapi_types.Date) {
	y, m, d := now.In(loc).Date()
	start = time.Date(y, m, d, 0, 0, 0, 0, loc)
	end = start.AddDate(0, 0, 1)
	return start, end, dateOf(y, m, d)
}

// byProductCursorSeparator joins the two encoded fields of a by-product
// cursor — mirrors stock's levelCursorSeparator.
const byProductCursorSeparator = "|"

// encodeByProductCursor builds GET /reports/sales/by-product's opaque
// cursor from the last row of a page: (revenue, product_id), exactly the
// tuple api/db/queries/reports.sql's SalesByProduct keyset compares
// against. revenue is rendered at money's fixed 2-decimal precision (the
// same string the row itself reports), which round-trips exactly back
// through decimal.NewFromString in decodeByProductCursor.
func encodeByProductCursor(revenue decimal.Decimal, productID uuid.UUID) string {
	raw := money.String(revenue) + byProductCursorSeparator + productID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// maxByProductCursorLen bounds the wire `?cursor=` parameter's length in
// bytes before any decoding work happens — a server-generated cursor
// (money.String's fixed-2-decimal revenue, the separator, and a 36-byte
// UUID, base64-encoded) is well under 64 bytes, so 128 leaves comfortable
// room without allowing a client to hand this handler an arbitrarily
// large string (a 10 KB cursor is a documented attack input, see
// decodeByProductCursor's own doc comment).
const maxByProductCursorLen = 128

// decodeByProductCursor reverses encodeByProductCursor. A nil/empty cursor
// means "first page" (an invalid pgtype.Numeric and a nil *uuid.UUID,
// which api/db/queries/reports.sql's `$1::numeric IS NULL` guard treats as
// "no lower bound"). Anything malformed — oversized, malformed base64,
// missing the separator, an unparsable revenue or product id — is 400
// VALIDATION_FAILED naming the cursor field, mirroring
// stock.decodeLowCursor.
//
// revenue is parsed with money.ParseSignedAmount, not a bare
// decimal.NewFromString: a period's net revenue can be negative (returns
// without an offsetting sale in the same window), which is exactly what
// ParseSignedAmount allows over money.ParseAmount, but — unlike a raw
// decimal.NewFromString — it never accepts exponent notation, so a
// crafted cursor cannot smuggle in a Decimal whose Exponent() is
// astronomical (e.g. "1e-1000000"); feeding one of those into
// money.ToNumeric and on into pgx's numeric encoder measured tens of
// seconds of CPU per request in review, since the encoder rescales by
// 10^exponent as a big.Int. This cursor is normally server-generated, not
// client-typed free text, but the `?cursor=` parameter itself is still
// client-controlled input (a replayed, edited, or hand-crafted one), so it
// gets the same untrusted-input treatment as anything else on the wire.
func decodeByProductCursor(requested *string) (pgtype.Numeric, *uuid.UUID, error) {
	if requested == nil || *requested == "" {
		return pgtype.Numeric{}, nil, nil
	}
	invalid := func() (pgtype.Numeric, *uuid.UUID, error) {
		return pgtype.Numeric{}, nil, apierr.Validation(map[string]string{"cursor": "invalid"})
	}
	if len(*requested) > maxByProductCursorLen {
		return invalid()
	}
	raw, err := base64.RawURLEncoding.DecodeString(*requested)
	if err != nil {
		return invalid()
	}
	revenueStr, idStr, found := strings.Cut(string(raw), byProductCursorSeparator)
	if !found {
		return invalid()
	}
	revenue, apiErr := money.ParseSignedAmount(revenueStr)
	if apiErr != nil {
		return invalid()
	}
	productID, err := uuid.Parse(idStr)
	if err != nil {
		return invalid()
	}
	return money.ToNumeric(revenue), &productID, nil
}
