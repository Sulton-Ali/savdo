// Package money converts between the three representations a money or
// quantity value passes through in this codebase (ADR-007): the
// `pgtype.Numeric` a `NUMERIC(14,2)` column scans into, the
// `shopspring/decimal.Decimal` Go code should compute with, and the
// decimal string (`"125000.00"`) the API sends and accepts. Nothing here
// ever touches a `float64` — that conversion is exactly what ADR-007
// forbids.
package money

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/internal/apierr"
)

// amountPattern is the shape ParseAmount accepts: an unsigned integer with
// an optional 1-2 digit fractional part. No sign, no scientific notation,
// no more than two decimal places — the same "decimal strings" format
// ADR-007 and docs/05-API.md § Conventions specify for money and
// quantities. A separate, explicit check (rather than leaning on
// decimal.Decimal's own parsing rules) so the accepted wire format is
// exactly this, not "whatever the decimal library happens to tolerate".
var amountPattern = regexp.MustCompile(`^\d+(\.\d{1,2})?$`)

// maxAmount is the largest value a `NUMERIC(14,2)` column can hold: 14
// total digits, 2 of them fractional, so 12 integer digits.
var maxAmount = decimal.RequireFromString("999999999999.99")

// OutOfRangeSQLState is Postgres' "numeric_value_out_of_range" SQLSTATE
// (22003). ParseAmount already rejects anything that would overflow
// NUMERIC(14,2) before it ever reaches a query, so IsOutOfRange exists
// purely as a defense-in-depth backstop for a write path that somehow
// still produced an out-of-range value.
const OutOfRangeSQLState = "22003"

// IsOutOfRange reports whether err is a Postgres numeric_value_out_of_range
// error (SQLSTATE 22003), for callers mapping it to a 400 VALIDATION_FAILED
// `invalid` alongside ParseAmount's own bound.
func IsOutOfRange(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == OutOfRangeSQLState
}

// FromNumeric converts a scanned `NUMERIC` column to a Decimal. n must be
// Valid (callers check n.Valid themselves — a NULL column has no decimal
// value to convert, and is a service-layer concern: "field absent" vs.
// "field zero" are different things a nullable money column can mean)
// and must not be NaN or Infinity (PostgreSQL's `numeric` type allows
// those, but no column this codebase writes ever produces one — a
// database that somehow has one is a data bug worth surfacing as an
// error, not silently coercing to zero).
func FromNumeric(n pgtype.Numeric) (decimal.Decimal, error) {
	if !n.Valid {
		return decimal.Decimal{}, fmt.Errorf("money: FromNumeric: not valid (NULL)")
	}
	if n.NaN {
		return decimal.Decimal{}, fmt.Errorf("money: FromNumeric: NaN")
	}
	if n.InfinityModifier != pgtype.Finite {
		return decimal.Decimal{}, fmt.Errorf("money: FromNumeric: infinite")
	}
	return decimal.NewFromBigInt(n.Int, n.Exp), nil
}

// ToNumeric converts a Decimal to a `pgtype.Numeric` ready to bind as a
// query parameter. The result is always Valid: true — clearing a nullable
// money column to NULL goes through the sqlc queries' explicit clear
// flags (e.g. UpdateProductParams.ClearCost), never a NULL Numeric, so
// ToNumeric never needs to produce one.
func ToNumeric(d decimal.Decimal) pgtype.Numeric {
	return pgtype.Numeric{Int: d.Coefficient(), Exp: d.Exponent(), Valid: true}
}

// ParseAmount parses s as a non-negative decimal string with at most two
// decimal places and a magnitude that fits `NUMERIC(14,2)` — the shape
// every money and quantity field on the wire must have (ADR-007). Returns
// a *apierr.Error with reason "invalid" for anything else: malformed
// input, a negative amount, more than two decimal digits (so "12.345" is
// rejected, never silently rounded), or a value over 999999999999.99.
func ParseAmount(s string) (decimal.Decimal, *apierr.Error) {
	if !amountPattern.MatchString(s) {
		return decimal.Decimal{}, invalidAmount()
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Decimal{}, invalidAmount()
	}
	if d.GreaterThan(maxAmount) {
		return decimal.Decimal{}, invalidAmount()
	}
	return d, nil
}

// ParseAmountField is ParseAmount naming field in the returned error's
// `details.fields`, for the common case of validating one named request
// field.
func ParseAmountField(field, s string) (decimal.Decimal, *apierr.Error) {
	d, apiErr := ParseAmount(s)
	if apiErr != nil {
		return decimal.Decimal{}, apierr.Validation(map[string]string{field: "invalid"})
	}
	return d, nil
}

func invalidAmount() *apierr.Error {
	return apierr.Validation(map[string]string{"amount": "invalid"})
}

// String formats d as a fixed 2-decimal-place string (`"125000.00"`), the
// canonical wire representation for money (ADR-007). Quantities, which can
// need three decimal places (docs/04-DATA-MODEL.md § 3: `NUMERIC(12,3)`),
// are not this package's concern in Phase 2 — catalog only ever prices in
// money, never quantities.
func String(d decimal.Decimal) string {
	return d.StringFixed(2)
}
