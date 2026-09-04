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
