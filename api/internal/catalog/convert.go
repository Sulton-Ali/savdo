package catalog

import (
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
)

// nullableUUID converts a *uuid.UUID (nil = SQL NULL) to the tri-state
// nullable.Nullable the generated schemas use for an optional id field —
// the same conversion auth/handler.go's nullableString applies to *string.
func nullableUUID(v *uuid.UUID) nullable.Nullable[uuid.UUID] {
	if v == nil {
		return nullable.NewNullNullable[uuid.UUID]()
	}
	return nullable.NewNullableWithValue(*v)
}

// nullableString converts a *string (nil = SQL NULL) to the tri-state
// nullable.Nullable the generated schemas use for an optional string
// field.
func nullableString(v *string) nullable.Nullable[string] {
	if v == nil {
		return nullable.NewNullNullable[string]()
	}
	return nullable.NewNullableWithValue(*v)
}

// nullableTime converts a *time.Time (nil = SQL NULL) to the tri-state
// nullable.Nullable the generated schemas use for an optional timestamp
// field.
func nullableTime(v *time.Time) nullable.Nullable[time.Time] {
	if v == nil {
		return nullable.NewNullNullable[time.Time]()
	}
	return nullable.NewNullableWithValue(*v)
}

// optionalUUID reads a nullable.Nullable[uuid.UUID] patch field into the
// three states a request can mean: nil (not specified — leave unchanged),
// a pointer to nil (explicit `null` — clear), or a pointer to a value
// (set). Mirrors shop/staff.go's `**string` idiom for StaffPatch.Phone,
// generalized to uuid.UUID and reused by every nullable-uuid patch field
// (Category.ParentId/ImageId, Product.CategoryId, ProductImageOrder's
// CoverImageId is a plain optional, not nullable, so it does not need
// this).
func optionalUUID(n nullable.Nullable[uuid.UUID]) **uuid.UUID {
	if !n.IsSpecified() {
		return nil
	}
	if n.IsNull() {
		var nilPtr *uuid.UUID
		return &nilPtr
	}
	v := n.MustGet()
	return &[]*uuid.UUID{&v}[0]
}

// optionalString is optionalUUID for a nullable.Nullable[string] patch
// field (Product.Sku/CostPrice/PromoPrice, Variant.Sku/Barcode/
// CostOverride/PriceOverride are all decimal-as-string or plain string
// fields using this same tri-state).
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

// optionalTime is optionalUUID for a nullable.Nullable[time.Time] patch
// field (Product.PromoFrom/PromoTo).
func optionalTime(n nullable.Nullable[time.Time]) **time.Time {
	if !n.IsSpecified() {
		return nil
	}
	if n.IsNull() {
		var nilPtr *time.Time
		return &nilPtr
	}
	v := n.MustGet()
	return &[]*time.Time{&v}[0]
}

// int32Field validates that v (a sort-order-like field the generated
// schemas expose as a bare, unbounded JSON `integer`/Go int) fits the
// int32 the sqlc `integer` column behind it actually stores, recording
// fields[field] = "invalid" and returning 0 when it does not — never a
// silent, wrapped-around int32(v) conversion of an out-of-range value.
func int32Field(field string, v int, fields map[string]string) int32 {
	if v < math.MinInt32 || v > math.MaxInt32 {
		fields[field] = "invalid"
		return 0
	}
	return int32(v) // #nosec G115 -- range-checked immediately above
}

// nullableInt32 converts a *int32 (nil = SQL NULL) to the tri-state
// nullable.Nullable[int] the generated Product schema uses for
// lowStockThreshold — same idea as nullableUUID/nullableString/
// nullableTime, just widened from the sqlc column's int32 to the bare Go
// int a JSON Schema `integer` field generates.
func nullableInt32(v *int32) nullable.Nullable[int] {
	if v == nil {
		return nullable.NewNullNullable[int]()
	}
	return nullable.NewNullableWithValue(int(*v))
}

// validatedLowStockThreshold checks v is non-negative and fits int32 (via
// int32Field), recording fields["lowStockThreshold"] = "invalid" and
// returning nil on either violation rather than clamping or wrapping.
// Shared by CreateProduct's plain *int field and
// optionalLowStockThreshold's nullable one.
func validatedLowStockThreshold(v int, fields map[string]string) *int32 {
	const field = "lowStockThreshold"
	if v < 0 {
		fields[field] = "invalid"
		return nil
	}
	iv := int32Field(field, v, fields)
	if _, bad := fields[field]; bad {
		return nil
	}
	return &iv
}

// optionalLowStockThreshold is optionalString for
// Product.LowStockThreshold's nullable.Nullable[int] (ProductPatch): nil
// (not specified — leave unchanged), a pointer to nil (explicit `null` —
// clear the override, D-35), or a pointer to a validated int32 (set).
func optionalLowStockThreshold(n nullable.Nullable[int], fields map[string]string) **int32 {
	if !n.IsSpecified() {
		return nil
	}
	if n.IsNull() {
		var nilPtr *int32
		return &nilPtr
	}
	v := validatedLowStockThreshold(n.MustGet(), fields)
	if v == nil {
		return nil
	}
	return &v
}
