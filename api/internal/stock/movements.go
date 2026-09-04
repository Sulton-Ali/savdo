package stock

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/pagination"
)

// decodeMovementCursor resolves GET /stock/movements' `?cursor=` using
// internal/pagination directly — ListMovements'/ListMovementsWithCreatedByName's
// keyset, (created_at, id) newest-first, is exactly the shape that package
// already codes for catalog/shop's own cursor-paginated lists.
func decodeMovementCursor(requested *gen.Cursor) (time.Time, uuid.UUID, error) {
	if requested == nil || *requested == "" {
		return time.Time{}, uuid.Nil, nil
	}
	return pagination.Decode(*requested)
}

func movementCursorPtr(createdAt time.Time, id uuid.UUID) (*time.Time, *uuid.UUID) {
	if createdAt.IsZero() {
		return nil, nil
	}
	return &createdAt, &id
}

// paginateMovements trims rows (fetched with limit+1) down to at most
// limit items and reports the opaque cursor for the next page.
func paginateMovements(rows []db.ListMovementsWithCreatedByNameRow, limit int32) ([]db.ListMovementsWithCreatedByNameRow, *string) {
	// Compare in int (widening limit, never narrowing len(rows)) — mirrors
	// catalog.paginateT; avoids a len(rows)->int32 narrowing conversion
	// gosec (G115) flags on principle even though a page can never
	// realistically hold anywhere near math.MaxInt32 rows.
	if len(rows) <= int(limit) {
		return rows, nil
	}
	items := rows[:limit]
	last := items[len(items)-1]
	cursor := pagination.Encode(last.CreatedAt, last.ID)
	return items, &cursor
}

// ListStockMovements lists the append-only stock movement ledger. Requires
// stock.write (manager+). unitCost is present only for a caller with
// cost.read — every stock.write role currently also has cost.read (see
// auth.rolePermissions), so this branch is exercised by a direct
// toGenMovement test (convert_test.go) rather than a role that can reach
// this handler but lacks the permission, since no such role exists today.
func (h *Handler) ListStockMovements(ctx context.Context, req gen.ListStockMovementsRequestObject) (gen.ListStockMovementsResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermStockWrite); err != nil {
		return nil, err
	}

	limit := clampLimit(req.Params.Limit)
	cCreatedAt, cID, err := decodeMovementCursor(req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	cursorCreatedAt, cursorID := movementCursorPtr(cCreatedAt, cID)

	var kind *db.StockMovementKind
	if req.Params.Kind != nil {
		k := db.StockMovementKind(*req.Params.Kind)
		kind = &k
	}

	rows, err := h.svc.q.ListMovementsWithCreatedByName(ctx, db.ListMovementsWithCreatedByNameParams{
		ShopID: authCtx.ShopID, VariantID: req.Params.VariantId, LocationID: req.Params.LocationId,
		Kind: kind, From: req.Params.From, To: req.Params.To,
		CursorCreatedAt: cursorCreatedAt, CursorID: cursorID, Limit: limit + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("stock: list movements: %w", err)
	}

	includeCost := auth.Require(ctx, auth.PermCostRead) == nil
	items, nextCursor := paginateMovements(rows, limit)
	genItems := make([]gen.StockMovement, len(items))
	for i, r := range items {
		mv := db.StockMovement{
			ID: r.ID, ShopID: r.ShopID, VariantID: r.VariantID, LocationID: r.LocationID, Kind: r.Kind,
			Qty: r.Qty, UnitCost: r.UnitCost, RefType: r.RefType, RefID: r.RefID,
			AdjustmentReason: r.AdjustmentReason, Reason: r.Reason, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt,
		}
		g, err := toGenMovement(mv, r.CreatedByName, includeCost)
		if err != nil {
			return nil, fmt.Errorf("stock: convert movement: %w", err)
		}
		genItems[i] = g
	}
	return gen.ListStockMovements200JSONResponse(gen.StockMovementList{Items: genItems, NextCursor: nullableString(nextCursor)}), nil
}
