package sales

import (
	"errors"
	"net/http"
	"regexp"

	"github.com/google/uuid"
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

// errSaleAlreadyVoided is SALE_ALREADY_VOIDED (409): VoidSaleTx was called
// on a sale whose status is already 'voided' — also the answer to a
// return whose original sale is voided (D-58/D-61 both require a
// completed original).
var errSaleAlreadyVoided = &apierr.Error{Status: http.StatusConflict, Code: gen.SALEALREADYVOIDED}

// errSaleVoidWindowClosed is SALE_VOID_WINDOW_CLOSED (409): the sale's
// completed_at calendar date, in the shop's own timezone, is not today
// (D-59) — the correction from here on is a return, not a void.
var errSaleVoidWindowClosed = &apierr.Error{Status: http.StatusConflict, Code: gen.SALEVOIDWINDOWCLOSED}

// errSaleHasReturns is SALE_HAS_RETURNS (409): a completed return already
// references this sale (D-62) — voiding it now would restore stock twice
// (once for the return, once for the void).
var errSaleHasReturns = &apierr.Error{Status: http.StatusConflict, Code: gen.SALEHASRETURNS}

// errSaleNotVoidable is SALE_NOT_VOIDABLE (409): VoidSaleTx was called on
// a return-kind sale (D-66) — a return is final; a wrong one is corrected
// by selling the item again, not by voiding the return.
var errSaleNotVoidable = &apierr.Error{Status: http.StatusConflict, Code: gen.SALENOTVOIDABLE}

// errSaleNotReturnable is SALE_NOT_RETURNABLE (409): CreateSaleReturnTx
// was called with a return-kind sale as its target (D-66's own "returns
// are final" — a return of a return) — a 409, not 400, because
// originalSaleId is a path parameter naming a real sale, not a malformed
// request field (review ruling: the request carries no `originalSaleId`
// field for a 400 VALIDATION_FAILED `details.fields` entry to name).
var errSaleNotReturnable = &apierr.Error{Status: http.StatusConflict, Code: gen.SALENOTRETURNABLE}

// errReturnExceedsSold builds RETURN_EXCEEDS_SOLD (409): the requested
// return quantity for body.items[index], added to what that line already
// has returned, would exceed what was sold (D-58). Both index (the
// request-array position, for a client rendering "line N") and
// saleItemId (the contract's own documented details field,
// contracts/openapi.yaml's POST /sales/{id}/return 409 description) are
// reported.
func errReturnExceedsSold(index int, saleItemID uuid.UUID) *apierr.Error {
	return &apierr.Error{
		Status: http.StatusConflict, Code: gen.RETURNEXCEEDSSOLD,
		Details: map[string]any{"index": index, "saleItemId": saleItemID.String()},
	}
}

// errDraftLineUnavailable builds a 422 VALIDATION_FAILED naming every
// unavailable line by its index in the request-equivalent items array
// (`items[<i>].variantId: invalid`) — apierr.Unprocessable's own
// `details.fields` shape (docs/05-API.md § Conventions, O-12
// vocabulary), but at 422: the request itself is well-formed, it is the
// draft's current state (a line's variant or product having gone
// inactive or soft-deleted since it was added) that cannot be processed
// (docs/05-API.md § Conventions' 422 bullet, D-88 — "the client shows
// which line and lets the user edit the draft"). Used by
// CompleteSaleDraftTx (drafts_write.go).
func errDraftLineUnavailable(fields map[string]string) *apierr.Error {
	return apierr.Unprocessable(fields)
}

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
