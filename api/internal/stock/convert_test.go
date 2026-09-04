package stock

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/money"
)

// TestToGenMovement_unitCostFilteredByIncludeCost is the direct
// service-level test the task calls for in place of a role-level one:
// every role that can reach ListStockMovements (stock.write, i.e. owner
// and manager) also has cost.read today (auth.rolePermissions), so there
// is no real caller that reaches the handler but should see unitCost
// nulled — this pins the filter itself, at the one function
// (toGenMovement) that applies it, so the behaviour is still covered and
// would immediately protect a future role that has stock.write without
// cost.read.
func TestToGenMovement_unitCostFilteredByIncludeCost(t *testing.T) {
	unitCost := money.ToNumeric(mustDecimal(t, "1500.00"))
	mv := db.StockMovement{
		ID: uuid.New(), VariantID: uuid.New(), LocationID: uuid.New(),
		Kind: db.StockMovementKindPurchaseIn, Qty: money.ToNumeric(mustDecimal(t, "5.000")),
		UnitCost: unitCost,
	}

	withCost, err := toGenMovement(mv, nil, true)
	if err != nil {
		t.Fatalf("toGenMovement(includeCost=true): %v", err)
	}
	if !withCost.UnitCost.IsSpecified() || withCost.UnitCost.IsNull() {
		t.Fatalf("UnitCost with includeCost=true = %+v, want a value", withCost.UnitCost)
	}
	if v, _ := withCost.UnitCost.Get(); v != "1500.00" {
		t.Fatalf("UnitCost with includeCost=true = %q, want 1500.00", v)
	}

	withoutCost, err := toGenMovement(mv, nil, false)
	if err != nil {
		t.Fatalf("toGenMovement(includeCost=false): %v", err)
	}
	if !withoutCost.UnitCost.IsNull() {
		t.Fatalf("UnitCost with includeCost=false = %+v, want null", withoutCost.UnitCost)
	}
}

func mustDecimal(t *testing.T, s string) decimal.Decimal {
	t.Helper()
	d, err := decimal.NewFromString(s)
	if err != nil {
		t.Fatalf("decimal(%q): %v", s, err)
	}
	return d
}
