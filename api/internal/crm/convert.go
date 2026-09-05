package crm

import (
	"github.com/oapi-codegen/nullable"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// nullableString converts a *string (nil = SQL NULL) to the tri-state
// nullable.Nullable the generated schemas use for an optional string
// field — mirrors catalog.nullableString/stock.nullableString.
func nullableString(v *string) nullable.Nullable[string] {
	if v == nil {
		return nullable.NewNullNullable[string]()
	}
	return nullable.NewNullableWithValue(*v)
}

// optionalString reads a nullable.Nullable[string] patch field into the
// three states a request can mean: nil (not specified — leave unchanged),
// a pointer to nil (explicit `null` — clear), or a pointer to a value
// (set) — mirrors catalog.optionalString/shop/staff.go's `**string` idiom.
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

// toGenSupplier maps a suppliers row onto the API schema.
func toGenSupplier(s db.Supplier) gen.Supplier {
	return gen.Supplier{
		Id: s.ID, Name: s.Name,
		ContactName:      nullableString(s.ContactName),
		Phone:            nullableString(s.Phone),
		TelegramUsername: nullableString(s.TelegramUsername),
		Note:             nullableString(s.Note),
	}
}

// toGenCustomer maps a customers row onto the API schema. tags is never
// null in the response even though the Go zero value for a nil []string
// column would marshal that way — the customers table's own `NOT NULL
// DEFAULT '{}'` (0015_customers.sql) already keeps the driver from
// returning nil in practice, but the explicit fallback here means the
// response contract holds even if that ever changes.
func toGenCustomer(c db.Customer) gen.Customer {
	tags := c.Tags
	if tags == nil {
		tags = []string{}
	}
	return gen.Customer{
		Id: c.ID, FullName: c.FullName,
		Phone:            nullableString(c.Phone),
		TelegramUsername: nullableString(c.TelegramUsername),
		Note:             nullableString(c.Note),
		Tags:             tags,
		CreatedAt:        c.CreatedAt,
		UpdatedAt:        c.UpdatedAt,
	}
}
