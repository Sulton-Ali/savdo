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

// decodeLowCursor resolves GET /stock/low's `?cursor=` using
// internal/pagination directly — ListLow's keyset (D-92:
// variant_created_at DESC, variant_id DESC) is exactly the shape that
// package already codes for catalog/shop/movements' own cursor-paginated
// lists.
func decodeLowCursor(requested *gen.Cursor) (time.Time, uuid.UUID, error) {
	if requested == nil || *requested == "" {
		return time.Time{}, uuid.Nil, nil
	}
	return pagination.Decode(*requested)
}

// ListLowStock lists variants at or below their effective low-stock
// threshold (D-44). Requires stock.write (manager+, per docs/05-API.md's
// endpoint table). The underlying query (api/db/queries/stock.sql's
// ListLow) already restricts to active products/variants that have at
// least one stock_levels row; this handler only paginates its result.
// Newest variant first (D-92).
func (h *Handler) ListLowStock(ctx context.Context, req gen.ListLowStockRequestObject) (gen.ListLowStockResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermStockWrite); err != nil {
		return nil, err
	}

	limit := clampLimit(req.Params.Limit)
	cCreatedAt, cVariant, err := decodeLowCursor(req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	cursorCreatedAt, cursorVariant := lowCursorPtr(cCreatedAt, cVariant)

	rows, err := h.svc.q.ListLow(ctx, db.ListLowParams{
		ShopID: authCtx.ShopID, CursorVariantCreatedAt: cursorCreatedAt, CursorVariantID: cursorVariant, Limit: limit + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("stock: list low stock: %w", err)
	}

	items, nextCursor := paginateLow(rows, limit)
	genItems := make([]gen.StockLowItem, len(items))
	for i, r := range items {
		g, err := toGenLow(r)
		if err != nil {
			return nil, fmt.Errorf("stock: convert low stock item: %w", err)
		}
		genItems[i] = g
	}
	return gen.ListLowStock200JSONResponse(gen.StockLowList{Items: genItems, NextCursor: nullableString(nextCursor)}), nil
}
