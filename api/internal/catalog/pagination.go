package catalog

import (
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/pagination"
)

// defaultLimit and maxLimit bound `GET /products`, the only cursor-paginated
// collection this module serves (docs/05-API.md § Conventions: "limit is
// 1-200, default 50").
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
// keyset it encodes, or (zero, zero) for a first page.
func decodeCursor(requested *gen.Cursor) (time.Time, uuid.UUID, error) {
	if requested == nil || *requested == "" {
		return time.Time{}, uuid.Nil, nil
	}
	return pagination.Decode(*requested)
}

// cursorPtr returns nil for a zero-value createdAt (meaning "no cursor,
// first page") and a pointer to it otherwise — the shape
// db.ListProductsFor{Staff,Cashier}Params expect.
func cursorPtr(createdAt time.Time, id uuid.UUID) (*time.Time, *uuid.UUID) {
	if createdAt.IsZero() {
		return nil, nil
	}
	return &createdAt, &id
}

// paginateT trims rows (fetched with limit+1) down to at most limit items
// and reports the opaque cursor for the next page — non-nil exactly when
// a limit+1'th row proved more data exists.
func paginateT[T any](rows []T, limit int32, keyOf func(T) (time.Time, uuid.UUID)) ([]T, *string) {
	if len(rows) <= int(limit) {
		return rows, nil
	}
	items := rows[:limit]
	createdAt, id := keyOf(items[len(items)-1])
	cursor := pagination.Encode(createdAt, id)
	return items, &cursor
}
