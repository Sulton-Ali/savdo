package stock

import (
	"context"
	"fmt"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// CreateStockTransfer transfers stock between two locations. Requires
// stock.write (manager+). Writes transfer_out at fromLocationId and
// transfer_in at toLocationId in one transaction (ADR-006), sharing one
// ref_id (ref_type "transfer") so the two rows can be found together; the
// transfer_out leg fails with 409 STOCK_INSUFFICIENT if it would take the
// source level below zero (D-41), which rolls back both legs. Unlike
// CreateAdjustment, this operation has no Idempotency-Key in the contract
// (docs/04-DATA-MODEL.md § 3's idempotency_keys note lists purchase
// receive and stock adjustments for Phase 3, not transfers) and writes no
// audit_log row (D-47 names adjustments, purchase receives and purchase
// cancels; transfers are not in that list) — so this method matches
// gen.StrictServerInterface directly and httpx/stock.go forwards to it
// unwrapped, the same as ListStockLevels/ListStockMovements/ListLowStock.
func (h *Handler) CreateStockTransfer(ctx context.Context, req gen.CreateStockTransferRequestObject) (gen.CreateStockTransferResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermStockWrite); err != nil {
		return nil, err
	}
	body := req.Body

	if body.FromLocationId == body.ToLocationId {
		return nil, errSameLocation
	}
	qty, ok := parseQty(body.Qty)
	if !ok || !qty.IsPositive() {
		return nil, apierr.Validation(map[string]string{"qty": "invalid"})
	}

	refID := newID()
	refType := "transfer"

	tx, err := h.svc.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("stock: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := h.svc.q.WithTx(tx)

	out, err := Move(ctx, qtx, MoveParams{
		ShopID: authCtx.ShopID, VariantID: body.VariantId, LocationID: body.FromLocationId,
		Kind: db.StockMovementKindTransferOut, Qty: qty.Neg(), RefType: &refType, RefID: &refID,
		ActorID: &authCtx.UserID,
	})
	if err != nil {
		return nil, mapMoveError(err)
	}
	in, err := Move(ctx, qtx, MoveParams{
		ShopID: authCtx.ShopID, VariantID: body.VariantId, LocationID: body.ToLocationId,
		Kind: db.StockMovementKindTransferIn, Qty: qty, RefType: &refType, RefID: &refID,
		ActorID: &authCtx.UserID,
	})
	if err != nil {
		return nil, mapMoveError(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("stock: commit transfer: %w", err)
	}

	name, err := h.svc.createdByName(ctx, authCtx.ShopID, &authCtx.UserID)
	if err != nil {
		return nil, err
	}
	includeCost := auth.Require(ctx, auth.PermCostRead) == nil

	outGen, err := toGenMovement(out, name, includeCost)
	if err != nil {
		return nil, fmt.Errorf("stock: convert transfer_out: %w", err)
	}
	inGen, err := toGenMovement(in, name, includeCost)
	if err != nil {
		return nil, fmt.Errorf("stock: convert transfer_in: %w", err)
	}

	return gen.CreateStockTransfer201JSONResponse(gen.StockTransferResult{Items: []gen.StockMovement{outGen, inGen}}), nil
}
