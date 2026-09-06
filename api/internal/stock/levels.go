package stock

import (
	"context"
	"fmt"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// ListStockLevels lists the shop's stock levels per variant and location.
// Any authenticated role (D-40) — cashiers see exact quantities, never
// cost; this schema carries no cost field at all. Cursor-paginated,
// newest variant first, then location (D-92): (variant_created_at,
// variant_id, location_id).
func (h *Handler) ListStockLevels(ctx context.Context, req gen.ListStockLevelsRequestObject) (gen.ListStockLevelsResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}

	limit := clampLimit(req.Params.Limit)
	cCreatedAt, cVariant, cLocation, err := decodeLevelCursor(req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	cursorCreatedAt, cursorVariant, cursorLocation := levelCursorPtr(cCreatedAt, cVariant, cLocation)

	rows, err := h.svc.q.ListLevels(ctx, db.ListLevelsParams{
		ShopID: authCtx.ShopID, VariantID: req.Params.VariantId, ProductID: req.Params.ProductId,
		LocationID: req.Params.LocationId, CursorVariantCreatedAt: cursorCreatedAt,
		CursorVariantID: cursorVariant, CursorLocationID: cursorLocation,
		Limit: limit + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("stock: list levels: %w", err)
	}

	items, nextCursor := paginateLevels(rows, limit)
	genItems := make([]gen.StockLevel, len(items))
	for i, r := range items {
		g, err := toGenLevel(r)
		if err != nil {
			return nil, fmt.Errorf("stock: convert level: %w", err)
		}
		genItems[i] = g
	}
	return gen.ListStockLevels200JSONResponse(gen.StockLevelList{Items: genItems, NextCursor: nullableString(nextCursor)}), nil
}
