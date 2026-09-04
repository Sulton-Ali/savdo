package stock

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// transferLeg is one side of a transfer's two Move calls.
type transferLeg struct {
	locationID uuid.UUID
	kind       db.StockMovementKind
	qty        decimal.Decimal
}

// sortedTransferLegs returns out and in sorted by locationID ascending
// (MoveParams' own doc comment: multi-line callers must lock rows in a
// consistent order). Two opposing concurrent transfers of the same
// variant between the same two locations (A->B and B->A) each build one
// out leg and one in leg with the *same pair* of locationIDs, just
// swapped between out/in — sorting by locationID rather than by "which
// leg is out" means both transactions call Move on the same location
// first, so they queue on that row's lock instead of each holding the one
// the other wants (the deadlock the review reproduced). The response
// still reports transfer_out before transfer_in regardless of this
// order — CreateStockTransfer keys the two Move results by kind, not by
// call order, before building it.
func sortedTransferLegs(out, in transferLeg) []transferLeg {
	legs := [2]transferLeg{out, in}
	if legs[1].locationID.String() < legs[0].locationID.String() {
		legs[0], legs[1] = legs[1], legs[0]
	}
	return legs[:]
}

// runTransfer runs both of a transfer's Move calls in one transaction, in
// sortedTransferLegs' deadlock-avoiding order, and commits. Split out from
// CreateStockTransfer so the deadlock retry below can call it a second
// time with a fresh transaction.
func (h *Handler) runTransfer(ctx context.Context, shopID, variantID, fromLocationID, toLocationID, actorID uuid.UUID, qty decimal.Decimal, refID uuid.UUID) (out, in db.StockMovement, err error) {
	refType := "transfer"
	outLeg := transferLeg{locationID: fromLocationID, kind: db.StockMovementKindTransferOut, qty: qty.Neg()}
	inLeg := transferLeg{locationID: toLocationID, kind: db.StockMovementKindTransferIn, qty: qty}

	tx, err := h.svc.pool.Begin(ctx)
	if err != nil {
		return db.StockMovement{}, db.StockMovement{}, fmt.Errorf("stock: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := h.svc.q.WithTx(tx)

	results := map[db.StockMovementKind]db.StockMovement{}
	for _, leg := range sortedTransferLegs(outLeg, inLeg) {
		result, err := Move(ctx, qtx, MoveParams{
			ShopID: shopID, VariantID: variantID, LocationID: leg.locationID,
			Kind: leg.kind, Qty: leg.qty, RefType: &refType, RefID: &refID, ActorID: &actorID,
		})
		if err != nil {
			return db.StockMovement{}, db.StockMovement{}, mapMoveError(err)
		}
		results[leg.kind] = result.Movement
	}

	if err := tx.Commit(ctx); err != nil {
		if isDeadlock(err) {
			return db.StockMovement{}, db.StockMovement{}, err
		}
		return db.StockMovement{}, db.StockMovement{}, fmt.Errorf("stock: commit transfer: %w", err)
	}
	return results[db.StockMovementKindTransferOut], results[db.StockMovementKindTransferIn], nil
}

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
//
// Deadlock handling (MINOR 4): runTransfer already locks its two rows in a
// consistent order, which is enough on its own for the common two-location
// case; the retry-once here is a backstop for a 3+ location cycle (e.g.
// concurrent A->B, B->C, C->A transfers) that sorted pairwise locking
// cannot rule out — Postgres kills one side of a genuine deadlock cycle
// with SQLSTATE 40P01. On that error, and only that error, runTransfer is
// retried exactly once with a fresh transaction (the previous one already
// rolled back — Postgres kills the whole transaction on deadlock, not just
// the statement); if the retry deadlocks again, this returns 409 CONFLICT
// rather than retrying forever or surfacing a raw 500.
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

	var out, in db.StockMovement
	var err error
	for attempt := 1; attempt <= 2; attempt++ {
		out, in, err = h.runTransfer(ctx, authCtx.ShopID, body.VariantId, body.FromLocationId, body.ToLocationId, authCtx.UserID, qty, refID)
		if err == nil || !isDeadlock(err) || attempt == 2 {
			break
		}
	}
	if err != nil {
		if isDeadlock(err) {
			return nil, errTransferDeadlock
		}
		return nil, err
	}

	// runTransfer's transaction has already committed by this point (its own
	// call site checked err == nil above), so it is safe — not a second
	// connection held open alongside a first — to use the Service's own
	// pool-bound q here rather than a qtx.
	name, err := createdByName(ctx, h.svc.q, authCtx.ShopID, &authCtx.UserID)
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
