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

// CreateAdjustment records a manual stock adjustment (D-46). Requires
// stock.write (manager+). Not itself one of gen.StrictServerInterface's
// methods: httpx.server.CreateStockAdjustment (httpx/stock.go) calls this
// after wrapping it in httpx.Idempotent, since that helper — needed here
// because POST /stock/adjustments accepts an Idempotency-Key — lives in
// internal/httpx and this package cannot import it without a cycle
// (internal/httpx already imports internal/stock to wire the router).
//
// Runs Move (kind adjustment) and audit.Write (action stock.adjust,
// D-47) in one transaction: both commit together, or — on
// ErrInsufficient or any other error — both roll back, so a partially
// recorded adjustment (a movement with no audit trail, or vice versa)
// can never happen.
func (h *Handler) CreateAdjustment(ctx context.Context, body gen.StockAdjustmentCreate) (gen.StockMovement, error) {
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

	tx, err := h.svc.pool.Begin(ctx)
	if err != nil {
		return gen.StockMovement{}, fmt.Errorf("stock: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := h.svc.q.WithTx(tx)

	mv, err := Move(ctx, qtx, MoveParams{
		ShopID: authCtx.ShopID, VariantID: body.VariantId, LocationID: body.LocationId,
		Kind: db.StockMovementKindAdjustment, Qty: qty, AdjustmentReason: &reason, Note: note,
		ActorID: &authCtx.UserID,
	})
	if err != nil {
		return gen.StockMovement{}, mapMoveError(err)
	}

	after, err := json.Marshal(mv)
	if err != nil {
		return gen.StockMovement{}, fmt.Errorf("stock: marshal movement for audit: %w", err)
	}
	if err := audit.Write(ctx, qtx, audit.Entry{
		ShopID: authCtx.ShopID, ActorID: authCtx.UserID, Action: "stock.adjust",
		EntityType: "stock_movement", EntityID: mv.ID, After: after,
	}); err != nil {
		return gen.StockMovement{}, fmt.Errorf("stock: write audit: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return gen.StockMovement{}, fmt.Errorf("stock: commit adjustment: %w", err)
	}

	name, err := h.svc.createdByName(ctx, authCtx.ShopID, &authCtx.UserID)
	if err != nil {
		return gen.StockMovement{}, err
	}
	includeCost := auth.Require(ctx, auth.PermCostRead) == nil
	return toGenMovement(mv, name, includeCost)
}
