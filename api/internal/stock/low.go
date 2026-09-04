package stock

import (
	"context"
	"fmt"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// ListLowStock lists variants at or below their effective low-stock
// threshold (D-44). Requires stock.write (manager+, per docs/05-API.md's
// endpoint table). The underlying query (api/db/queries/stock.sql's
// ListLow) already restricts to active products/variants that have at
// least one stock_levels row; this handler only paginates its result.
func (h *Handler) ListLowStock(ctx context.Context, req gen.ListLowStockRequestObject) (gen.ListLowStockResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermStockWrite); err != nil {
		return nil, err
	}

	limit := clampLimit(req.Params.Limit)
	cVariant, err := decodeLowCursor(req.Params.Cursor)
	if err != nil {
		return nil, err
	}

	rows, err := h.svc.q.ListLow(ctx, db.ListLowParams{
		ShopID: authCtx.ShopID, CursorVariantID: lowCursorPtr(cVariant), Limit: limit + 1,
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
