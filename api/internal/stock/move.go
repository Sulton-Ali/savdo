package stock

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/money"
)

// MoveParams is one stock movement to write. Qty is signed (positive in,
// negative out, docs/04-DATA-MODEL.md § 3) — callers pass the sign, Move
// never infers it from Kind. UnitCost/RefType/RefID/AdjustmentReason/Note
// are all optional (nil = SQL NULL); ActorID is nil for a system-written
// movement (e.g. a seed) and set to the acting user's id otherwise.
//
// Multi-line callers (a transfer's two legs, a future purchase receive's
// per-line writes): call Move for your lines in ascending (variant_id,
// location_id) order. Move locks each line's stock_levels row (step 3
// below) in whatever order you call it, so two callers touching the same
// two rows in opposite orders can deadlock under Postgres (SQLSTATE
// 40P01) — sorting lines the same way in every caller makes every
// transaction acquire the rows in the same order, so they queue instead of
// deadlocking. CreateStockTransfer (transfers.go) is the first caller and
// follows this rule; T4's purchase receive must too.
type MoveParams struct {
	ShopID           uuid.UUID
	VariantID        uuid.UUID
	LocationID       uuid.UUID
	Kind             db.StockMovementKind
	Qty              decimal.Decimal
	UnitCost         *decimal.Decimal
	RefType          *string
	RefID            *uuid.UUID
	AdjustmentReason *db.AdjustmentReason
	Note             *string
	ActorID          *uuid.UUID
}

// MoveResult is what a successful Move produced: the movement row itself,
// plus the level's quantity immediately before and after it — callers that
// need to record both (e.g. CreateAdjustmentTx's audit_log row, NIT 11)
// would otherwise have to re-derive Before by subtracting Qty back out
// (error-prone with a signed decimal) or re-read the row (a second query,
// racy outside the lock Move already held it under); Move already computed
// both internally, so it returns them.
type MoveResult struct {
	Movement db.StockMovement
	Before   decimal.Decimal
	After    decimal.Decimal
}

// ErrInsufficient is Move's signal that applying Qty would take the level
// below zero and the shop does not allow that (D-41, D-48:
// shops.allow_negative_stock). Callers map it to 409 STOCK_INSUFFICIENT
// (mapMoveError) and — this is the part that matters — MUST let it
// propagate out of their transaction without committing: Move itself never
// rolls back (it does not own the transaction, qtx's caller does), so a
// caller that swallows this error and commits anyway would durably record
// UpsertLevelRow's zero-row insert while still correctly skipping the
// movement and the delta, silently drifting stock_levels' identity from
// "materialized view of stock_movements" (ADR-006) even though the drift
// is harmless in this one case (a zero row) — never rely on that; always
// roll back on any Move error.
type ErrInsufficient struct {
	VariantID  uuid.UUID
	LocationID uuid.UUID
	Available  decimal.Decimal
}

func (e *ErrInsufficient) Error() string {
	return fmt.Sprintf("stock: insufficient: variant=%s location=%s available=%s", e.VariantID, e.LocationID, e.Available)
}

// Move is the only writer of stock_levels in this codebase (hard rule 2,
// ADR-006). qtx must already be bound to the caller's own transaction
// (db.Queries.WithTx) — Move neither begins nor commits/rolls back one,
// so a purchase receive (T4) can call it once per line, all inside a
// single transaction, and a caller that gets a non-nil error back MUST
// roll back that transaction (see ErrInsufficient's doc comment): Move
// never leaves stock_levels or stock_movements changed on its own error
// path, but it also never protects a caller who commits after a failure.
//
// Sequence, in order:
//  1. Validate variant and location both belong to ShopID (ADR-004) —
//     neither stock_movements nor stock_levels has a DB constraint tying
//     shop_id to the variant's or location's own shop_id (only their
//     shop_id and variant_id/location_id foreign keys, independently), so
//     this is the only thing standing between a caller-supplied id from
//     another shop and a cross-tenant write. 404s (not 400s) on either
//     miss — the id names something that, from this shop's perspective,
//     does not exist.
//  2. Load the shop row (for allow_negative_stock) inside the same
//     transaction — not cached, not read before qtx opened — so a
//     concurrent settings change can never be observed half-applied
//     against this movement.
//  3. UpsertLevelRow, then GetLevelForUpdate (SELECT ... FOR UPDATE): the
//     row now exists and is locked, serializing every concurrent Move
//     against the same (shop, variant, location) triple — this, not a
//     raised transaction isolation level, is what makes two concurrent
//     sales of the last unit race-free under Postgres' default READ
//     COMMITTED (the reviewer measured this; do not raise the isolation
//     level, it would only add contention/serialization-failure handling
//     the row lock already makes unnecessary). See MoveParams' own doc
//     comment for the deadlock a multi-line caller must avoid by sorting
//     its lines before calling Move.
//  4. Compute new = current + Qty. If new < 0 and the shop does not allow
//     negative stock, return ErrInsufficient without writing anything
//     else — UpsertLevelRow's own write (a zero row, ON CONFLICT DO
//     NOTHING) is undone by the caller's mandatory rollback.
//  5. InsertMovement, then ApplyLevelDelta — the actual, only write to
//     stock_levels. A resulting quantity too large for numeric(12,3) (12
//     total digits) maps to 400 VALIDATION_FAILED on "qty" (NIT 13)
//     rather than surfacing Postgres' own numeric_value_out_of_range as an
//     opaque 500 — this is a backstop (parseQty already bounds every
//     wire-supplied quantity before it reaches here), reachable only if
//     enough movements accumulate on one line to overflow the column.
func Move(ctx context.Context, qtx *db.Queries, p MoveParams) (MoveResult, error) {
	if _, err := qtx.GetVariantForStaff(ctx, db.GetVariantForStaffParams{ShopID: p.ShopID, ID: p.VariantID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MoveResult{}, apierr.NotFound("variant")
		}
		return MoveResult{}, fmt.Errorf("stock: get variant: %w", err)
	}
	if _, err := qtx.GetLocation(ctx, db.GetLocationParams{ShopID: p.ShopID, ID: p.LocationID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MoveResult{}, apierr.NotFound("location")
		}
		return MoveResult{}, fmt.Errorf("stock: get location: %w", err)
	}

	shopRow, err := qtx.GetShop(ctx, p.ShopID)
	if err != nil {
		return MoveResult{}, fmt.Errorf("stock: get shop: %w", err)
	}

	if err := qtx.UpsertLevelRow(ctx, db.UpsertLevelRowParams{ShopID: p.ShopID, VariantID: p.VariantID, LocationID: p.LocationID}); err != nil {
		return MoveResult{}, fmt.Errorf("stock: upsert level row: %w", err)
	}
	current, err := qtx.GetLevelForUpdate(ctx, db.GetLevelForUpdateParams{ShopID: p.ShopID, VariantID: p.VariantID, LocationID: p.LocationID})
	if err != nil {
		return MoveResult{}, fmt.Errorf("stock: get level for update: %w", err)
	}

	currentQty, err := money.FromNumeric(current.Qty)
	if err != nil {
		return MoveResult{}, fmt.Errorf("stock: current level qty: %w", err)
	}
	newQty := currentQty.Add(p.Qty)
	if newQty.IsNegative() && !shopRow.AllowNegativeStock {
		return MoveResult{}, &ErrInsufficient{VariantID: p.VariantID, LocationID: p.LocationID, Available: currentQty}
	}

	mv, err := qtx.InsertMovement(ctx, db.InsertMovementParams{
		ID: newID(), ShopID: p.ShopID, VariantID: p.VariantID, LocationID: p.LocationID,
		Kind: p.Kind, Qty: money.ToNumeric(p.Qty), UnitCost: optionalNumeric(p.UnitCost),
		RefType: p.RefType, RefID: p.RefID, AdjustmentReason: p.AdjustmentReason,
		Reason: p.Note, CreatedBy: p.ActorID,
	})
	if err != nil {
		if apiErr, ok := mapOutOfRange(err); ok {
			return MoveResult{}, apiErr
		}
		return MoveResult{}, fmt.Errorf("stock: insert movement: %w", err)
	}

	if _, err := qtx.ApplyLevelDelta(ctx, db.ApplyLevelDeltaParams{
		Delta: money.ToNumeric(p.Qty), ShopID: p.ShopID, VariantID: p.VariantID, LocationID: p.LocationID,
	}); err != nil {
		if apiErr, ok := mapOutOfRange(err); ok {
			return MoveResult{}, apiErr
		}
		return MoveResult{}, fmt.Errorf("stock: apply level delta: %w", err)
	}

	return MoveResult{Movement: mv, Before: currentQty, After: newQty}, nil
}

// mapOutOfRange reports whether err is Postgres' numeric_value_out_of_range
// (money.IsOutOfRange), returning the 400 VALIDATION_FAILED "qty" error
// Move's InsertMovement/ApplyLevelDelta call sites use in place of it
// (NIT 13).
func mapOutOfRange(err error) (*apierr.Error, bool) {
	if money.IsOutOfRange(err) {
		return apierr.Validation(map[string]string{"qty": "invalid"}), true
	}
	return nil, false
}

// MoveInTx is Move for a caller that only needs to write one movement and
// has no other statement to share its transaction with: it opens the
// transaction, calls Move, and commits — or rolls back on any error,
// including ErrInsufficient (Move's own doc comment on why that matters).
// A caller writing more than one movement atomically (a transfer's out+in
// pair, a purchase receive's per-line movements) opens its own transaction
// and calls Move directly instead, so every line shares one commit/
// rollback.
func (s *Service) MoveInTx(ctx context.Context, p MoveParams) (MoveResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MoveResult{}, fmt.Errorf("stock: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	result, err := Move(ctx, qtx, p)
	if err != nil {
		return MoveResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MoveResult{}, fmt.Errorf("stock: commit move: %w", err)
	}
	return result, nil
}

// optionalNumeric converts *decimal.Decimal to a pgtype.Numeric: nil (no
// unit cost — every mover but a purchase receive) becomes an explicitly
// invalid (SQL NULL) Numeric; a set value uses money.ToNumeric.
func optionalNumeric(d *decimal.Decimal) pgtype.Numeric {
	if d == nil {
		return pgtype.Numeric{Valid: false}
	}
	return money.ToNumeric(*d)
}
