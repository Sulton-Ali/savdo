package stock

import (
	"errors"
	"net/http"
	"regexp"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// mapMoveError turns a Move error into the *apierr.Error docs/05-API.md's
// stock endpoints promise: ErrInsufficient becomes 409 STOCK_INSUFFICIENT
// with details.{variantId,locationId,available} (the exact shape
// contracts/openapi.yaml documents on POST /stock/adjustments and
// POST /stock/transfers). Anything else — Move's own apierr.NotFound
// ("variant"/"location") or a wrapped internal error — is already the
// right shape (or deliberately opaque) and passes through unchanged.
func mapMoveError(err error) error {
	var insufficient *ErrInsufficient
	if errors.As(err, &insufficient) {
		return &apierr.Error{
			Status: http.StatusConflict,
			Code:   gen.STOCKINSUFFICIENT,
			Details: map[string]any{
				"variantId":  insufficient.VariantID.String(),
				"locationId": insufficient.LocationID.String(),
				"available":  qtyString(insufficient.Available),
			},
		}
	}
	return err
}

// errSameLocation is SAME_LOCATION (409): a transfer whose fromLocationId
// equals toLocationId, per contracts/openapi.yaml's POST /stock/transfers
// 409 description.
var errSameLocation = &apierr.Error{Status: http.StatusConflict, Code: gen.SAMELOCATION}

// deadlockSQLState is Postgres' "deadlock_detected" SQLSTATE — what a
// transaction Postgres chose to kill to break a lock cycle fails with
// (MINOR 4). Checked at commit time too, not just per-statement: pgx
// surfaces a deadlock either way depending on exactly which statement lost.
const deadlockSQLState = "40P01"

// isDeadlock reports whether err is a Postgres deadlock (SQLSTATE 40P01).
func isDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == deadlockSQLState
}

// errTransferDeadlock is CONFLICT (409): CreateStockTransfer's own retry
// (transfers.go) already tried runTransfer twice and both attempts
// deadlocked against some other concurrent transfer — reported as a clear,
// retryable-by-the-client conflict rather than an opaque 500.
var errTransferDeadlock = &apierr.Error{
	Status: http.StatusConflict, Code: gen.CONFLICT,
	Details: map[string]any{"reason": "deadlock"},
}

// qtyPattern is the wire shape a quantity field (docs/05-API.md §
// Conventions: "Quantities: decimal strings") must have: an optional sign,
// an unsigned integer part, an optional up-to-three-digit fractional part
// — matching stock_movements.qty's numeric(12,3) scale (§ 04-DATA-MODEL.md
// § 3). Unlike money.ParseAmount, this accepts a leading '-': adjustment
// and transfer-out quantities are signed.
var qtyPattern = regexp.MustCompile(`^-?\d+(\.\d{1,3})?$`)

// maxQty is the largest magnitude a numeric(12,3) column can hold: 12
// total digits, 3 fractional, so 9 integer digits.
var maxQty = decimal.RequireFromString("999999999.999")

// parseQty parses s as a signed decimal string with at most three decimal
// places and a magnitude that fits numeric(12,3). ok is false for anything
// else: malformed input, more than three decimal digits (never silently
// rounded), or a magnitude over 999999999.999 — callers report their own
// field name (StockAdjustmentCreate.qty vs StockTransferCreate.qty)
// rather than this function fixing one.
func parseQty(s string) (d decimal.Decimal, ok bool) {
	if !qtyPattern.MatchString(s) {
		return decimal.Decimal{}, false
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Decimal{}, false
	}
	if d.Abs().GreaterThan(maxQty) {
		return decimal.Decimal{}, false
	}
	return d, true
}

// validAdjustmentReasons is the fixed enum D-46 specifies, checked here
// rather than trusted from the wire: gen.StockAdjustmentCreate.Reason is a
// Go string underneath (gen.AdjustmentReason), so a client sending
// anything outside the five known values would otherwise reach
// db.AdjustmentReason(body.Reason) unchecked and fail as an opaque 500
// (a Postgres enum-input error) instead of a 400 VALIDATION_FAILED.
var validAdjustmentReasons = map[db.AdjustmentReason]bool{
	db.AdjustmentReasonCountCorrection: true,
	db.AdjustmentReasonDamaged:         true,
	db.AdjustmentReasonLost:            true,
	db.AdjustmentReasonFound:           true,
	db.AdjustmentReasonOther:           true,
}
