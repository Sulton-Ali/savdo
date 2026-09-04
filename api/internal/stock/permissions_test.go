package stock_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/stock"
)

// TestStockPermissions_cashier is the 200-on-levels/403-on-everything-else
// matrix for the narrowest role that reaches any stock endpoint at all:
// `GET /stock/levels` is open to any authenticated role (D-40), every
// other stock route is manager+.
func TestStockPermissions_cashier(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	h := stock.NewHandler(stock.NewService(pool, q))

	shop := seedShop(ctx, t, q, "perm-cashier")
	cashier := seedUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "perm-product")
	variant := seedVariant(ctx, t, q, shop.ID, product.ID, "{}")
	loc := seedLocation(ctx, t, q, shop.ID, "Main")

	cashierCtx := ctxAs(shop.ID, cashier)

	// GET /stock/levels: any authenticated role (D-40) — 200.
	if _, err := h.ListStockLevels(cashierCtx, gen.ListStockLevelsRequestObject{}); err != nil {
		t.Fatalf("ListStockLevels (cashier): want 200, got error: %v", err)
	}

	// GET /stock/movements: manager+ — 403.
	assertForbidden(t, "ListStockMovements (cashier)", func() error {
		_, err := h.ListStockMovements(cashierCtx, gen.ListStockMovementsRequestObject{})
		return err
	})

	// GET /stock/low: manager+ — 403.
	assertForbidden(t, "ListLowStock (cashier)", func() error {
		_, err := h.ListLowStock(cashierCtx, gen.ListLowStockRequestObject{})
		return err
	})

	// POST /stock/adjustments: manager+ — 403 (CreateAdjustment checks
	// permission before validating the body, so an otherwise-invalid body
	// is fine here).
	assertForbidden(t, "CreateAdjustment (cashier)", func() error {
		_, err := h.CreateAdjustment(cashierCtx, gen.StockAdjustmentCreate{
			VariantId: variant.ID, LocationId: loc.ID, Qty: "1.000", Reason: gen.Found,
		})
		return err
	})

	// POST /stock/transfers: manager+ — 403.
	assertForbidden(t, "CreateStockTransfer (cashier)", func() error {
		_, err := h.CreateStockTransfer(cashierCtx, transferReq(variant.ID, loc.ID, loc.ID, "1.000"))
		return err
	})
}

func assertForbidden(t *testing.T, label string, call func() error) {
	t.Helper()
	err := call()
	if err == nil {
		t.Fatalf("%s: want 403 FORBIDDEN, got no error", label)
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 403 {
		t.Fatalf("%s: error = %v, want 403 FORBIDDEN", label, err)
	}
}
