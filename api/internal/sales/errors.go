package sales

import (
	"errors"
	"net/http"
	"regexp"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/stock"
)

// maxSaleItems bounds SaleCreate.items (contracts/openapi.yaml
// `maxItems: 100`) — enforced here too since the strict server does not
// check JSON Schema array bounds at runtime (mirrors
// stock.maxPurchaseItems' own doc comment).
const maxSaleItems = 100

// saleRefType is the stock_movements.ref_type CreateSale writes on every
// sale_out movement — the same "kind carries the story, ref_type carries
// what to blame it on" convention purchases' purchaseRefType/
// purchaseCancelRefType use (stock/errors.go).
const saleRefType = "sale"

// qtyPattern is a sale line's wire quantity shape: an unsigned integer
// with an optional up-to-three-digit fractional part — no leading '-'.
// Unlike stock.qtyPattern (which also accepts an adjustment/transfer's
// signed quantity), a SaleItemCreate.qty "must be greater than zero"
// (contracts/openapi.yaml), so a sale never has a legitimate reason to
// parse a negative one.
var qtyPattern = regexp.MustCompile(`^\d+(\.\d{1,3})?$`)

// maxQty is the largest magnitude a numeric(12,3) column can hold (12
// total digits, 3 fractional) — mirrors stock.maxQty.
var maxQty = decimal.RequireFromString("999999999.999")

// parseQty parses s as an unsigned decimal string with at most three
// decimal places and a magnitude that fits numeric(12,3). ok is false for
// anything else: malformed input, a negative sign, more than three
// decimal digits (never silently rounded), or a magnitude over
// 999999999.999 — mirrors stock.parseQty, minus the sign it never needs.
func parseQty(s string) (d decimal.Decimal, ok bool) {
	if !qtyPattern.MatchString(s) {
		return decimal.Decimal{}, false
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Decimal{}, false
	}
	if d.GreaterThan(maxQty) {
		return decimal.Decimal{}, false
	}
	return d, true
}

// validPaymentMethods is the fixed enum D-54 specifies, checked here
// rather than trusted from the wire: gen.SalePaymentCreate.Method is a Go
// string underneath (gen.PaymentMethod), so a client sending anything
// outside the three known values would otherwise reach
// db.PaymentMethod(body.Payment.Method) unchecked and fail as an opaque
// 500 (a Postgres enum-input error) instead of a 400 VALIDATION_FAILED —
// mirrors stock.validAdjustmentReasons.
var validPaymentMethods = map[gen.PaymentMethod]bool{
	gen.Cash:     true,
	gen.Card:     true,
	gen.Transfer: true,
}

// deadlockSQLState is Postgres' "deadlock_detected" SQLSTATE — duplicated
// from internal/stock's own copy (stock/errors.go) and internal/httpx's
// own copy (httpx/idempotency.go) rather than exported and called
// cross-package: each package that needs it keeps its own three-line
// check, the same trade-off httpx/idempotency.go's own doc comment on
// deadlockSQLState already explains.
const deadlockSQLState = "40P01"

// isDeadlock reports whether err is a Postgres deadlock (SQLSTATE 40P01).
func isDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == deadlockSQLState
}

// errDeadlock is CONFLICT (409) details.reason: "deadlock" — the shared
// "safe to retry" shape docs/05-API.md's POST /sales 409 description
// promises, the same value stock.errDeadlock/httpx.errDeadlock report.
var errDeadlock = &apierr.Error{
	Status: http.StatusConflict, Code: gen.CONFLICT,
	Details: map[string]any{"reason": "deadlock"},
}

// errDiscountExceedsSubtotal is DISCOUNT_EXCEEDS_SUBTOTAL (409): the
// computed discount_amount is greater than the sale's subtotal (D-57,
// contracts/openapi.yaml's POST /sales 409 description).
var errDiscountExceedsSubtotal = &apierr.Error{Status: http.StatusConflict, Code: gen.DISCOUNTEXCEEDSSUBTOTAL}

// mapMoveError turns a stock.Move error into the *apierr.Error
// docs/05-API.md's POST /sales promises: a deadlock (SQLSTATE 40P01,
// isDeadlock) becomes 409 CONFLICT details.reason: "deadlock" (checked
// first, same reasoning as stock.mapMoveError's own doc comment).
// *stock.ErrInsufficient becomes 409 STOCK_INSUFFICIENT with
// details.{variantId,locationId,available}. Anything else — Move's own
// apierr.NotFound("variant"/"location") or a wrapped internal error — is
// already the right shape (or deliberately opaque) and passes through
// unchanged. Mirrors stock.mapMoveError; duplicated rather than exported
// cross-package for the same reason isDeadlock is.
func mapMoveError(err error) error {
	if isDeadlock(err) {
		return errDeadlock
	}
	var insufficient *stock.ErrInsufficient
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
