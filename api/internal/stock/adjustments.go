package stock

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/audit"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// auditAfter is the JSON shape CreateAdjustmentTx's audit_log.after column
// carries (NIT 11): the resulting level quantity alongside the movement
// itself, so an auditor reading the row does not have to separately look
// up the movement to see what the adjustment actually did to the level.
type auditAfter struct {
	Qty      string          `json:"qty"`
	Movement db.StockMovement `json:"movement"`
}

// auditBefore is audit_log.before's shape: just the level quantity
// immediately before the movement (there is no "before" movement to
// pair it with).
type auditBefore struct {
	Qty string `json:"qty"`
}

// CreateAdjustmentTx records a manual stock adjustment (D-46) using qtx —
// the caller's own transaction — rather than opening one itself. Requires
// stock.write (manager+).
//
// Callers: httpx.server.CreateStockAdjustment (httpx/stock.go) is the only
// one today. It runs this inside httpx.Idempotent's own transaction, on
// the same connection Idempotent's advisory lock and idempotency_keys
// bookkeeping already hold — not a second, separately-opened transaction —
// so the movement, the audit row and the idempotency_keys row all commit
// or roll back together, and a request never needs two pooled connections
// at once (a prior version opened its own transaction here, which under a
// small connection pool and many concurrent Idempotency-Key'd requests
// could exhaust the pool: every in-flight request held one connection for
// Idempotent's transaction while waiting for a second, for this one, that
// only becomes available once another request's pair releases both —
// nothing ever could). If a future caller needs the write without
// Idempotent (nothing does yet), it opens its own transaction the way
// MoveInTx does for a single Move and calls this with that transaction's
// *db.Queries.
//
// Not itself one of gen.StrictServerInterface's methods, and does not
// itself commit or roll back qtx's transaction — the caller does, after
// (for httpx.server.CreateStockAdjustment) also storing the
// Idempotency-Key response in the same transaction.
func (h *Handler) CreateAdjustmentTx(ctx context.Context, qtx *db.Queries, body gen.StockAdjustmentCreate) (gen.StockMovement, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return gen.StockMovement{}, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermStockWrite); err != nil {
		return gen.StockMovement{}, err
	}

	fields := map[string]string{}
	qty, ok := parseQty(body.Qty)
	if !ok || qty.IsZero() {
		fields["qty"] = "invalid"
	}
	reason := db.AdjustmentReason(body.Reason)
	if !validAdjustmentReasons[reason] {
		fields["reason"] = "invalid"
	}
	if len(fields) > 0 {
		return gen.StockMovement{}, apierr.Validation(fields)
	}

	var note *string
	if body.Note != nil {
		trimmed := strings.TrimSpace(*body.Note)
		if trimmed != "" {
			note = &trimmed
		}
	}

	result, err := Move(ctx, qtx, MoveParams{
		ShopID: authCtx.ShopID, VariantID: body.VariantId, LocationID: body.LocationId,
		Kind: db.StockMovementKindAdjustment, Qty: qty, AdjustmentReason: &reason, Note: note,
		ActorID: &authCtx.UserID,
	})
	if err != nil {
		return gen.StockMovement{}, mapMoveError(err)
	}

	before, err := json.Marshal(auditBefore{Qty: qtyString(result.Before)})
	if err != nil {
		return gen.StockMovement{}, fmt.Errorf("stock: marshal audit before: %w", err)
	}
	after, err := json.Marshal(auditAfter{Qty: qtyString(result.After), Movement: result.Movement})
	if err != nil {
		return gen.StockMovement{}, fmt.Errorf("stock: marshal audit after: %w", err)
	}
	if err := audit.Write(ctx, qtx, audit.Entry{
		ShopID: authCtx.ShopID, ActorID: authCtx.UserID, Action: "stock.adjust",
		EntityType: "stock_movement", EntityID: result.Movement.ID, Before: before, After: after,
	}); err != nil {
		return gen.StockMovement{}, fmt.Errorf("stock: write audit: %w", err)
	}

	name, err := createdByName(ctx, qtx, authCtx.ShopID, &authCtx.UserID)
	if err != nil {
		return gen.StockMovement{}, err
	}
	includeCost := auth.Require(ctx, auth.PermCostRead) == nil
	return toGenMovement(result.Movement, name, includeCost)
}
