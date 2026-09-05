package sales

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oapi-codegen/nullable"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/catalog"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/money"
)

// qtyString formats d as a fixed 3-decimal-place string
// (docs/04-DATA-MODEL.md § 3: numeric(12,3)) — mirrors stock.qtyString.
func qtyString(d decimal.Decimal) string {
	return d.StringFixed(3)
}

// numericQtyString converts a scanned numeric(12,3) column to its wire
// string — mirrors stock.numericQtyString.
func numericQtyString(n pgtype.Numeric) (string, error) {
	d, err := money.FromNumeric(n)
	if err != nil {
		return "", fmt.Errorf("sales: numeric qty: %w", err)
	}
	return qtyString(d), nil
}

// nullableString converts a *string (nil = SQL NULL) to the tri-state
// nullable.Nullable the generated schemas use — mirrors
// stock.nullableString/catalog.nullableString.
func nullableString(v *string) nullable.Nullable[string] {
	if v == nil {
		return nullable.NewNullNullable[string]()
	}
	return nullable.NewNullableWithValue(*v)
}

// nullableUUID converts a *uuid.UUID (nil = SQL NULL) to the tri-state
// nullable.Nullable the generated schemas use — mirrors
// stock.nullableUUID/catalog.nullableUUID.
func nullableUUID(v *uuid.UUID) nullable.Nullable[uuid.UUID] {
	if v == nil {
		return nullable.NewNullNullable[uuid.UUID]()
	}
	return nullable.NewNullableWithValue(*v)
}

// nullableTime converts a *time.Time (nil = SQL NULL) to the tri-state
// nullable.Nullable the generated schemas use — mirrors
// stock.nullableTime/catalog.nullableTime.
func nullableTime(v *time.Time) nullable.Nullable[time.Time] {
	if v == nil {
		return nullable.NewNullNullable[time.Time]()
	}
	return nullable.NewNullableWithValue(*v)
}

// requestLocaleFor resolves the locale a sale response's
// productName/variantLabel fields render in: the caller's
// `Accept-Language` (catalog.ResolveLocale, requested -> uz -> any)
// falling back to shopID's own default_locale — mirrors
// stock.requestLocaleFor (purchases.go).
func requestLocaleFor(ctx context.Context, q *db.Queries, shopID uuid.UUID) (string, error) {
	shopRow, err := q.GetShop(ctx, shopID)
	if err != nil {
		return "", fmt.Errorf("sales: get shop: %w", err)
	}
	return catalog.ResolveLocale(ctx, shopRow.DefaultLocale), nil
}

// joinSlash joins parts with " / " — mirrors stock.joinSlash.
func joinSlash(parts []string) string {
	out := parts[0]
	for _, p := range parts[1:] {
		out += " / " + p
	}
	return out
}

// variantLabel builds SaleItem.variantLabel: a variant's attribute values
// in attribute-definition order (e.g. "L / Blue"), falling back to its
// SKU, then its id — mirrors stock.variantLabel (purchases.go), the same
// rule PurchaseItem.variantLabel and admin/src/catalog/variants.ts's own
// client-side variantLabel apply. defs must already be ordered by
// sort_order (db.ListAttributeDefinitions' own ORDER BY).
func variantLabel(sku *string, variantID uuid.UUID, attrsRaw json.RawMessage, defs []db.ListAttributeDefinitionsRow) (string, error) {
	var attrs map[string]string
	if err := json.Unmarshal(attrsRaw, &attrs); err != nil {
		return "", fmt.Errorf("sales: unmarshal variant attributes: %w", err)
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

// saleHeaderRow is the field set GetSaleForStaffRow and GetSaleForCashierRow
// share byte-for-byte (neither carries a cost/margin column — D-63's own
// doc comment on GetSaleForCashier) — toGenSale converts from this common
// shape so the caller (get.go) only has to adapt whichever role-specific
// row it fetched, once, rather than duplicate the whole conversion twice.
type saleHeaderRow struct {
	ID             uuid.UUID
	Number         int64
	Kind           db.SaleKind
	Status         db.SaleStatus
	LocationID     uuid.UUID
	CustomerID     *uuid.UUID
	CashierID      uuid.UUID
	OriginalSaleID *uuid.UUID
	Subtotal       pgtype.Numeric
	DiscountAmount pgtype.Numeric
	DiscountReason *string
	Total          pgtype.Numeric
	Note           *string
	CompletedAt    time.Time
	VoidedAt       *time.Time
	VoidedBy       *uuid.UUID
	VoidReason     *string
	LocationName   string
	CustomerName   *string
	CashierName    string
	PaymentMethod  *db.PaymentMethod
	PaymentAmount  pgtype.Numeric
	HasReturns     bool
}

func saleHeaderFromStaffRow(r db.GetSaleForStaffRow) saleHeaderRow {
	return saleHeaderRow{
		ID: r.ID, Number: r.Number, Kind: r.Kind, Status: r.Status, LocationID: r.LocationID,
		CustomerID: r.CustomerID, CashierID: r.CashierID, OriginalSaleID: r.OriginalSaleID,
		Subtotal: r.Subtotal, DiscountAmount: r.DiscountAmount, DiscountReason: r.DiscountReason,
		Total: r.Total, Note: r.Note, CompletedAt: r.CompletedAt, VoidedAt: r.VoidedAt,
		VoidedBy: r.VoidedBy, VoidReason: r.VoidReason, LocationName: r.LocationName,
		CustomerName: r.CustomerName, CashierName: r.CashierName, PaymentMethod: r.PaymentMethod,
		PaymentAmount: r.PaymentAmount, HasReturns: r.HasReturns,
	}
}

func saleHeaderFromCashierRow(r db.GetSaleForCashierRow) saleHeaderRow {
	return saleHeaderRow{
		ID: r.ID, Number: r.Number, Kind: r.Kind, Status: r.Status, LocationID: r.LocationID,
		CustomerID: r.CustomerID, CashierID: r.CashierID, OriginalSaleID: r.OriginalSaleID,
		Subtotal: r.Subtotal, DiscountAmount: r.DiscountAmount, DiscountReason: r.DiscountReason,
		Total: r.Total, Note: r.Note, CompletedAt: r.CompletedAt, VoidedAt: r.VoidedAt,
		VoidedBy: r.VoidedBy, VoidReason: r.VoidReason, LocationName: r.LocationName,
		CustomerName: r.CustomerName, CashierName: r.CashierName, PaymentMethod: r.PaymentMethod,
		PaymentAmount: r.PaymentAmount, HasReturns: r.HasReturns,
	}
}

// toGenSale converts h plus its already-converted items to the wire Sale
// shape. payment.amount falls back to "0.00" in the — currently
// impossible, but defensive — case the LEFT JOIN found no payment row
// (GetSaleForStaff/ForCashier's own doc comment).
func toGenSale(h saleHeaderRow, items []gen.SaleItem) (gen.Sale, error) {
	subtotal, err := money.FromNumeric(h.Subtotal)
	if err != nil {
		return gen.Sale{}, fmt.Errorf("sales: subtotal: %w", err)
	}
	discountAmount, err := money.FromNumeric(h.DiscountAmount)
	if err != nil {
		return gen.Sale{}, fmt.Errorf("sales: discount amount: %w", err)
	}
	total, err := money.FromNumeric(h.Total)
	if err != nil {
		return gen.Sale{}, fmt.Errorf("sales: total: %w", err)
	}
	paymentAmount := decimal.Zero
	if h.PaymentAmount.Valid {
		paymentAmount, err = money.FromNumeric(h.PaymentAmount)
		if err != nil {
			return gen.Sale{}, fmt.Errorf("sales: payment amount: %w", err)
		}
	}
	paymentMethod := gen.PaymentMethod("")
	if h.PaymentMethod != nil {
		paymentMethod = gen.PaymentMethod(*h.PaymentMethod)
	}

	return gen.Sale{
		Id: h.ID, Number: int(h.Number), Kind: gen.SaleKind(h.Kind), Status: gen.SaleStatus(h.Status),
		LocationId: h.LocationID, LocationName: h.LocationName,
		CustomerId: nullableUUID(h.CustomerID), CustomerName: nullableString(h.CustomerName),
		CashierId: h.CashierID, CashierName: h.CashierName,
		OriginalSaleId: nullableUUID(h.OriginalSaleID),
		Subtotal:       money.String(subtotal),
		DiscountAmount: money.String(discountAmount),
		DiscountReason: nullableString(h.DiscountReason),
		Total:          money.String(total),
		Note:           nullableString(h.Note),
		CompletedAt:    h.CompletedAt,
		VoidedAt:       nullableTime(h.VoidedAt),
		VoidedBy:       nullableUUID(h.VoidedBy),
		VoidReason:     nullableString(h.VoidReason),
		Payment:        gen.SalePayment{Method: paymentMethod, Amount: money.String(paymentAmount)},
		Items:          items,
		HasReturns:     h.HasReturns,
	}, nil
}

// saleSummaryRow is the field set ListSalesForStaffRow and
// ListSalesForCashierRow share byte-for-byte — same reasoning as
// saleHeaderRow above, specialized to the list shape (no payment amount).
type saleSummaryRow struct {
	ID             uuid.UUID
	Number         int64
	Kind           db.SaleKind
	Status         db.SaleStatus
	LocationID     uuid.UUID
	CustomerID     *uuid.UUID
	CashierID      uuid.UUID
	OriginalSaleID *uuid.UUID
	Subtotal       pgtype.Numeric
	DiscountAmount pgtype.Numeric
	DiscountReason *string
	Total          pgtype.Numeric
	Note           *string
	CompletedAt    time.Time
	VoidedAt       *time.Time
	VoidedBy       *uuid.UUID
	VoidReason     *string
	LocationName   string
	CustomerName   *string
	CashierName    string
	PaymentMethod  *db.PaymentMethod
	HasReturns     bool
}

func saleSummaryFromStaffRow(r db.ListSalesForStaffRow) saleSummaryRow {
	return saleSummaryRow{
		ID: r.ID, Number: r.Number, Kind: r.Kind, Status: r.Status, LocationID: r.LocationID,
		CustomerID: r.CustomerID, CashierID: r.CashierID, OriginalSaleID: r.OriginalSaleID,
		Subtotal: r.Subtotal, DiscountAmount: r.DiscountAmount, DiscountReason: r.DiscountReason,
		Total: r.Total, Note: r.Note, CompletedAt: r.CompletedAt, VoidedAt: r.VoidedAt,
		VoidedBy: r.VoidedBy, VoidReason: r.VoidReason, LocationName: r.LocationName,
		CustomerName: r.CustomerName, CashierName: r.CashierName, PaymentMethod: r.PaymentMethod,
		HasReturns: r.HasReturns,
	}
}

func saleSummaryFromCashierRow(r db.ListSalesForCashierRow) saleSummaryRow {
	return saleSummaryRow{
		ID: r.ID, Number: r.Number, Kind: r.Kind, Status: r.Status, LocationID: r.LocationID,
		CustomerID: r.CustomerID, CashierID: r.CashierID, OriginalSaleID: r.OriginalSaleID,
		Subtotal: r.Subtotal, DiscountAmount: r.DiscountAmount, DiscountReason: r.DiscountReason,
		Total: r.Total, Note: r.Note, CompletedAt: r.CompletedAt, VoidedAt: r.VoidedAt,
		VoidedBy: r.VoidedBy, VoidReason: r.VoidReason, LocationName: r.LocationName,
		CustomerName: r.CustomerName, CashierName: r.CashierName, PaymentMethod: r.PaymentMethod,
		HasReturns: r.HasReturns,
	}
}

// toGenSaleSummary converts one ListSales row (already adapted to
// saleSummaryRow) to the wire SaleSummary shape.
func toGenSaleSummary(r saleSummaryRow) (gen.SaleSummary, error) {
	subtotal, err := money.FromNumeric(r.Subtotal)
	if err != nil {
		return gen.SaleSummary{}, fmt.Errorf("sales: subtotal: %w", err)
	}
	discountAmount, err := money.FromNumeric(r.DiscountAmount)
	if err != nil {
		return gen.SaleSummary{}, fmt.Errorf("sales: discount amount: %w", err)
	}
	total, err := money.FromNumeric(r.Total)
	if err != nil {
		return gen.SaleSummary{}, fmt.Errorf("sales: total: %w", err)
	}
	paymentMethod := gen.PaymentMethod("")
	if r.PaymentMethod != nil {
		paymentMethod = gen.PaymentMethod(*r.PaymentMethod)
	}

	return gen.SaleSummary{
		Id: r.ID, Number: int(r.Number), Kind: gen.SaleKind(r.Kind), Status: gen.SaleStatus(r.Status),
		LocationId: r.LocationID, LocationName: r.LocationName,
		CustomerId: nullableUUID(r.CustomerID), CustomerName: nullableString(r.CustomerName),
		CashierId: r.CashierID, CashierName: r.CashierName,
		OriginalSaleId: nullableUUID(r.OriginalSaleID),
		Subtotal:       money.String(subtotal),
		DiscountAmount: money.String(discountAmount),
		DiscountReason: nullableString(r.DiscountReason),
		Total:          money.String(total),
		Note:           nullableString(r.Note),
		CompletedAt:    r.CompletedAt,
		VoidedAt:       nullableTime(r.VoidedAt),
		VoidedBy:       nullableUUID(r.VoidedBy),
		VoidReason:     nullableString(r.VoidReason),
		PaymentMethod:  paymentMethod,
		HasReturns:     r.HasReturns,
	}, nil
}

// saleItemRow is the field set ListSaleItemsForStaffRow and
// ListSaleItemsForCashierRow share, minus unit_cost (present only for the
// staff row — D-63, hard rule 8) — toGenSaleItem converts from this common
// shape, with UnitCost left nil for a cashier's row.
type saleItemRow struct {
	ID                uuid.UUID
	VariantID         uuid.UUID
	ProductID         uuid.UUID
	Qty               pgtype.Numeric
	UnitPrice         pgtype.Numeric
	UnitCost          *pgtype.Numeric
	LineTotal         pgtype.Numeric
	ReturnedQty       pgtype.Numeric
	VariantSku        *string
	VariantAttributes json.RawMessage
	ProductName       string
}

func saleItemFromStaffRow(r db.ListSaleItemsForStaffRow) saleItemRow {
	return saleItemRow{
		ID: r.ID, VariantID: r.VariantID, ProductID: r.ProductID, Qty: r.Qty, UnitPrice: r.UnitPrice,
		UnitCost: &r.UnitCost, LineTotal: r.LineTotal, ReturnedQty: r.ReturnedQty,
		VariantSku: r.VariantSku, VariantAttributes: r.VariantAttributes, ProductName: r.ProductName,
	}
}

func saleItemFromCashierRow(r db.ListSaleItemsForCashierRow) saleItemRow {
	return saleItemRow{
		ID: r.ID, VariantID: r.VariantID, ProductID: r.ProductID, Qty: r.Qty, UnitPrice: r.UnitPrice,
		UnitCost: nil, LineTotal: r.LineTotal, ReturnedQty: r.ReturnedQty,
		VariantSku: r.VariantSku, VariantAttributes: r.VariantAttributes, ProductName: r.ProductName,
	}
}

// toGenSaleItem converts row to the wire SaleItem shape. unitCost is
// present only when row.UnitCost is non-nil (the staff row) — absent
// (never null: gen.SaleItem.UnitCost is `*Decimal`, omitted from the JSON
// entirely when nil) for a cashier (D-63, ADR-010, hard rule 5).
func toGenSaleItem(row saleItemRow, defs []db.ListAttributeDefinitionsRow) (gen.SaleItem, error) {
	qty, err := numericQtyString(row.Qty)
	if err != nil {
		return gen.SaleItem{}, fmt.Errorf("sales: item qty: %w", err)
	}
	unitPrice, err := money.FromNumeric(row.UnitPrice)
	if err != nil {
		return gen.SaleItem{}, fmt.Errorf("sales: item unit price: %w", err)
	}
	lineTotal, err := money.FromNumeric(row.LineTotal)
	if err != nil {
		return gen.SaleItem{}, fmt.Errorf("sales: item line total: %w", err)
	}
	returnedQty, err := numericQtyString(row.ReturnedQty)
	if err != nil {
		return gen.SaleItem{}, fmt.Errorf("sales: item returned qty: %w", err)
	}
	label, err := variantLabel(row.VariantSku, row.VariantID, row.VariantAttributes, defs)
	if err != nil {
		return gen.SaleItem{}, err
	}

	g := gen.SaleItem{
		Id: row.ID, VariantId: row.VariantID, ProductId: row.ProductID, ProductName: row.ProductName,
		VariantLabel: label, Qty: qty, UnitPrice: money.String(unitPrice), LineTotal: money.String(lineTotal),
		ReturnedQty: returnedQty,
	}
	if row.UnitCost != nil && row.UnitCost.Valid {
		unitCost, err := money.FromNumeric(*row.UnitCost)
		if err != nil {
			return gen.SaleItem{}, fmt.Errorf("sales: item unit cost: %w", err)
		}
		s := money.String(unitCost)
		g.UnitCost = &s
	}
	return g, nil
}
