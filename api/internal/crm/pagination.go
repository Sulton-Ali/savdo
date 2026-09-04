package crm

import (
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/pagination"
)

// defaultLimit and maxLimit bound GET /suppliers (docs/05-API.md §
// Conventions: "limit is 1-200, default 50") — mirrors
// catalog.defaultLimit/maxLimit and stock's own copy.
const (
	defaultLimit = 50
	maxLimit     = 200
)

// clampLimit resolves the requested `?limit=` query parameter (nil or
// non-positive means "use the default") to a bound in [1, maxLimit].
func clampLimit(requested *gen.Limit) int32 {
	limit := defaultLimit
	if requested != nil {
		limit = *requested
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	return int32(limit)
}

// decodeCursor resolves the requested `?cursor=` query parameter to the
// (created_at, id) keyset it encodes, or (zero, zero) for a first page —
// mirrors catalog.decodeCursor, reusing internal/pagination directly since
// ListSuppliers' keyset is exactly that shape.
func decodeCursor(requested *gen.Cursor) (time.Time, uuid.UUID, error) {
	if requested == nil || *requested == "" {
		return time.Time{}, uuid.Nil, nil
	}
	return pagination.Decode(*requested)
}

// cursorPtr returns nil for a zero-value createdAt (meaning "no cursor,
// first page") and a pointer to it otherwise — the shape
// db.ListSuppliersParams' CursorCreatedAt/CursorID expect. Mirrors
// catalog.cursorPtr.
func cursorPtr(createdAt time.Time, id uuid.UUID) (*time.Time, *uuid.UUID) {
	if createdAt.IsZero() {
		return nil, nil
	}
	return &createdAt, &id
}

// paginateSuppliers trims rows (fetched with limit+1) down to at most
// limit items and reports the opaque cursor for the next page.
func paginateSuppliers(rows []db.Supplier, limit int32) ([]db.Supplier, *string) {
	if len(rows) <= int(limit) {
		return rows, nil
	}
	items := rows[:limit]
	last := items[len(items)-1]
	cursor := pagination.Encode(last.CreatedAt, last.ID)
	return items, &cursor
}
