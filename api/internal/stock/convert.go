package stock

import (
	"encoding/json"
	"fmt"
	"time"

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

// nullableTime converts a *time.Time (nil = SQL NULL) to the tri-state
// nullable.Nullable the generated schemas use for an optional timestamp
// field (Purchase.receivedAt) — mirrors catalog.nullableTime.
func nullableTime(v *time.Time) nullable.Nullable[time.Time] {
	if v == nil {
		return nullable.NewNullNullable[time.Time]()
	}
	return nullable.NewNullableWithValue(*v)
}

// optionalString reads a nullable.Nullable[string] patch field into the
// three states a request can mean: nil (not specified — leave unchanged),
// a pointer to nil (explicit `null` — clear), or a pointer to a value
// (set). Mirrors catalog.optionalString/crm.optionalString.
func optionalString(n nullable.Nullable[string]) **string {
	if !n.IsSpecified() {
		return nil
	}
	if n.IsNull() {
		var nilPtr *string
		return &nilPtr
	}
	v := n.MustGet()
	return &[]*string{&v}[0]
}

// variantLabel builds PurchaseItem.variantLabel: a variant's attribute
// values in attribute-definition order (e.g. "L / Blue"), falling back to
// its SKU, then its id — the same rule
// admin/src/catalog/variants.ts's variantLabel applies client-side,
// reimplemented here since a purchase's response is built server-side.
// defs must already be ordered by sort_order (db.ListAttributeDefinitions'
// own ORDER BY), the same order the admin's own attributeDefinitions prop
// is in.
func variantLabel(sku *string, variantID uuid.UUID, attrsRaw json.RawMessage, defs []db.ListAttributeDefinitionsRow) (string, error) {
	var attrs map[string]string
	if err := json.Unmarshal(attrsRaw, &attrs); err != nil {
		return "", fmt.Errorf("stock: unmarshal variant attributes: %w", err)
	}
	parts := make([]string, 0, len(defs))
	for _, def := range defs {
		if v, ok := attrs[def.Code]; ok && v != "" {
			parts = append(parts, v)
		}
	}
	if len(parts) > 0 {
		return joinSlash(parts), nil
	}
	if sku != nil && *sku != "" {
		return *sku, nil
	}
	return variantID.String(), nil
}

// joinSlash joins parts with " / " (variantLabel's separator) — a tiny
// helper so variantLabel itself reads as the rule it implements rather
// than a strings.Join call buried in it.
func joinSlash(parts []string) string {
	out := parts[0]
	for _, p := range parts[1:] {
		out += " / " + p
	}
	return out
}

// toGenPurchaseItem converts one ListPurchaseItemsWithLabels row, plus its
// already-built label, to the wire PurchaseItem shape.
func toGenPurchaseItem(r db.ListPurchaseItemsWithLabelsRow, label string) (gen.PurchaseItem, error) {
	qty, err := numericQtyString(r.Qty)
	if err != nil {
		return gen.PurchaseItem{}, err
	}
	unitCost, err := money.FromNumeric(r.UnitCost)
	if err != nil {
		return gen.PurchaseItem{}, fmt.Errorf("stock: purchase item unit cost: %w", err)
	}
	return gen.PurchaseItem{
		Id: r.ID, VariantId: r.VariantID, Qty: qty, UnitCost: money.String(unitCost),
		ProductName: r.ProductName, VariantLabel: label, Sku: nullableString(r.VariantSku),
	}, nil
}

// toGenPurchase converts a purchases row plus its already-converted items
// to the wire Purchase shape. totalCost is the sum of the items'
// line-total, always recomputed here rather than trusted from the
// purchases.total_cost column (hard rule 8): CreatePurchase/
// UpdatePurchaseHeader never set that column at all — it is written only
// once, at receive, by SetPurchaseReceived — so a draft (mutable via
// PATCH's item replacement) would otherwise report a stale 0 until
// received; summing the current items is correct for both a draft and a
// received purchase (whose items, once received, never change again).
func toGenPurchase(p db.Purchase, items []gen.PurchaseItem, itemTotals []decimal.Decimal) gen.Purchase {
	total := decimal.Zero
	for _, t := range itemTotals {
		total = total.Add(t)
	}
	return gen.Purchase{
		Id: p.ID, Number: p.Number, SupplierId: p.SupplierID, LocationId: p.LocationID,
		Status: gen.PurchaseStatus(p.Status), SupplierInvoiceNo: nullableString(p.SupplierInvoiceNo),
		ReceivedAt: nullableTime(p.ReceivedAt), Note: nullableString(p.Note),
		TotalCost: money.String(total), Items: items, CreatedAt: p.CreatedAt,
	}
}
