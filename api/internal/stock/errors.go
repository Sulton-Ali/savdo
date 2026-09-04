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
// stock endpoints promise: a deadlock (SQLSTATE 40P01, isDeadlock) becomes
// the same 409 CONFLICT details.reason: "deadlock" errDeadlock reports
// after CreateStockTransfer's own retry exhausts (MINOR 7, T4 review) —
// checked first, since a deadlocked statement can also be the one that was
// about to detect ErrInsufficient, and "the transaction was killed to
// break a lock cycle" is the more accurate, more actionable (retryable)
// signal of the two. ErrInsufficient becomes 409 STOCK_INSUFFICIENT with
// details.{variantId,locationId,available} (the exact shape
// contracts/openapi.yaml documents on POST /stock/adjustments and
// POST /stock/transfers). Anything else — Move's own apierr.NotFound
// ("variant"/"location") or a wrapped internal error — is already the
// right shape (or deliberately opaque) and passes through unchanged.
func mapMoveError(err error) error {
	if isDeadlock(err) {
		return errDeadlock
	}
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

// errDeadlock is CONFLICT (409) details.reason: "deadlock" — the shared
// "safe to retry" shape docs/05-API.md's /stock/transfers,
// /purchases/{id}/receive and /purchases/{id}/cancel 409 descriptions all
// promise (MINOR 7, T4 review). mapMoveError returns it for any Move
// error that is itself a deadlock; CreateStockTransfer's own retry
// (transfers.go) additionally returns it once its own commit-time retry
// is exhausted (a deadlock detected at COMMIT, after every one of
// runTransfer's Move calls already succeeded, so mapMoveError never saw
// it) — same error value, same client-visible shape, from the two
// different points a deadlock can surface.
var errDeadlock = &apierr.Error{
	Status: http.StatusConflict, Code: gen.CONFLICT,
	Details: map[string]any{"reason": "deadlock"},
}

// errPurchaseNotDraft is PURCHASE_NOT_DRAFT (409): UpdatePurchase was
// called on a purchase that is no longer `draft` (contracts/openapi.yaml's
// PATCH /purchases/{id} 409).
var errPurchaseNotDraft = &apierr.Error{Status: http.StatusConflict, Code: gen.PURCHASENOTDRAFT}

// errPurchaseAlreadyReceived is PURCHASE_ALREADY_RECEIVED (409):
// ReceivePurchase was called on a purchase already `received`.
var errPurchaseAlreadyReceived = &apierr.Error{Status: http.StatusConflict, Code: gen.PURCHASEALREADYRECEIVED}

// errPurchaseAlreadyCancelled is PURCHASE_ALREADY_CANCELLED (409):
// ReceivePurchase or CancelPurchase was called on a purchase already
// `cancelled`.
var errPurchaseAlreadyCancelled = &apierr.Error{Status: http.StatusConflict, Code: gen.PURCHASEALREADYCANCELLED}

// purchaseRefType/purchaseCancelRefType are the ref_type Move writes on a
// purchase's stock_movements rows: "purchase" for the original receive,
// "purchase_cancel" for a cancelled-after-received purchase's reversing
// movements (both kind purchase_in, ADR-006 — there is no separate enum
// value for a cancellation; ref_type is what tells the two apart in
// history, per the pre-emptive ruling on T4's cancel-of-received
// ambiguity).
const (
	purchaseRefType       = "purchase"
	purchaseCancelRefType = "purchase_cancel"
)

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
