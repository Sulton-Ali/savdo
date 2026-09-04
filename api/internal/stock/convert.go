package stock

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oapi-codegen/nullable"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/money"
)

// qtyString formats d as a fixed 3-decimal-place string
// (docs/04-DATA-MODEL.md § 3: numeric(12,3)) — the quantity counterpart of
// money.String's fixed-2 rendering for numeric(14,2).
func qtyString(d decimal.Decimal) string {
	return d.StringFixed(3)
}

// numericQtyString converts a scanned numeric(12,3) column to its wire
// string. Every qty this package reads back off the ledger is Valid — the
// column is NOT NULL — so an invalid Numeric here is a driver/schema bug,
// surfaced as an error rather than silently rendered as "0.000".
func numericQtyString(n pgtype.Numeric) (string, error) {
	d, err := money.FromNumeric(n)
	if err != nil {
		return "", fmt.Errorf("stock: numeric qty: %w", err)
	}
	return qtyString(d), nil
}

// nullableString converts a *string (nil = SQL NULL) to the tri-state
// nullable.Nullable the generated schemas use for an optional string
// field — mirrors catalog.nullableString/shop.nullableString.
func nullableString(v *string) nullable.Nullable[string] {
	if v == nil {
		return nullable.NewNullNullable[string]()
	}
	return nullable.NewNullableWithValue(*v)
}

// toGenLevel converts one ListLevels row to the wire StockLevel shape — no
// cost field exists on this schema at all (D-40: open to every
// authenticated role).
func toGenLevel(r db.ListLevelsRow) (gen.StockLevel, error) {
	qty, err := numericQtyString(r.Qty)
	if err != nil {
		return gen.StockLevel{}, err
	}
	return gen.StockLevel{
		VariantId: r.VariantID, ProductId: r.ProductID, LocationId: r.LocationID, Qty: qty,
	}, nil
}

// toGenLow converts one ListLow row to the wire StockLowItem shape.
func toGenLow(r db.ListLowRow) (gen.StockLowItem, error) {
	qty, err := numericQtyString(r.Qty)
	if err != nil {
		return gen.StockLowItem{}, err
	}
	return gen.StockLowItem{
		VariantId: r.VariantID, ProductId: r.ProductID, Qty: qty, Threshold: int(r.Threshold),
	}, nil
}

// toGenMovement converts one stock_movements row to the wire StockMovement
// shape. createdByName is the display name already resolved by the caller
// (ListMovementsWithCreatedByName's join, or a direct GetUserByID lookup
// for a single-movement response — see service.go's createdByName) — this
// function never queries anything itself. includeCost gates unitCost per
// ADR-010/hard rule 5: a caller without cost.read gets null regardless of
// what the movement actually cost, filtered here in the service layer, not
// left to the UI.
func toGenMovement(mv db.StockMovement, createdByName *string, includeCost bool) (gen.StockMovement, error) {
	qty, err := numericQtyString(mv.Qty)
	if err != nil {
		return gen.StockMovement{}, err
	}

	var unitCost nullable.Nullable[string]
	if includeCost && mv.UnitCost.Valid {
		d, err := money.FromNumeric(mv.UnitCost)
		if err != nil {
			return gen.StockMovement{}, fmt.Errorf("stock: movement unit cost: %w", err)
		}
		unitCost = nullable.NewNullableWithValue(money.String(d))
	} else {
		unitCost = nullable.NewNullNullable[string]()
	}

	var reason nullable.Nullable[string]
	if mv.AdjustmentReason != nil {
		reason = nullable.NewNullableWithValue(string(*mv.AdjustmentReason))
	} else {
		reason = nullable.NewNullNullable[string]()
	}

	var createdBy nullable.Nullable[uuid.UUID]
	if mv.CreatedBy != nil {
		createdBy = nullable.NewNullableWithValue(*mv.CreatedBy)
	} else {
		createdBy = nullable.NewNullNullable[uuid.UUID]()
	}

	return gen.StockMovement{
		Id: mv.ID, VariantId: mv.VariantID, LocationId: mv.LocationID, Kind: gen.StockMovementKind(mv.Kind),
		Qty: qty, UnitCost: unitCost, RefType: nullableString(mv.RefType), RefId: nullableUUID(mv.RefID),
		Reason: reason, Note: nullableString(mv.Reason), CreatedBy: createdBy,
		CreatedByName: nullableString(createdByName), CreatedAt: mv.CreatedAt,
	}, nil
}

// nullableUUID converts a *uuid.UUID (nil = SQL NULL) to the tri-state
// nullable.Nullable the generated schemas use for an optional id field —
// mirrors catalog.nullableUUID.
func nullableUUID(v *uuid.UUID) nullable.Nullable[uuid.UUID] {
	if v == nil {
		return nullable.NewNullNullable[uuid.UUID]()
	}
	return nullable.NewNullableWithValue(*v)
}
