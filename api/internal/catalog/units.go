package catalog

import (
	"context"
	"fmt"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// ListUnits lists the shop's units of measure. Any authenticated role;
// read-only in Phase 2 (docs/04-DATA-MODEL.md § 2: "Seeded per shop").
func (h *Handler) ListUnits(ctx context.Context, _ gen.ListUnitsRequestObject) (gen.ListUnitsResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}

	locale := h.svc.resolveLocale(ctx)
	rows, err := h.svc.q.ListUnits(ctx, db.ListUnitsParams{Locale: locale, ShopID: authCtx.ShopID})
	if err != nil {
		return nil, fmt.Errorf("catalog: list units: %w", err)
	}

	items := make([]gen.Unit, len(rows))
	for i, r := range rows {
		items[i] = gen.Unit{
			Id:                  r.ID,
			Code:                r.Code,
			Precision:           int(r.Precision),
			Locale:              effectiveLocale(r.LocaleUsed, locale),
			Name:                r.Name,
			TranslationFallback: translationFallback(r.LocaleUsed, locale),
		}
	}
	return gen.ListUnits200JSONResponse(gen.UnitList{Items: items}), nil
}
