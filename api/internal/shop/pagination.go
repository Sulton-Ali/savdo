package shop

import (
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/pagination"
)

// defaultLimit and maxLimit bound every cursor-paginated collection this
// module serves (docs/06-ROADMAP.md Phase 1 T5 spec: "Limit default 50,
// max 200").
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
// keyset it encodes, or (zero, zero) for a first page. A malformed cursor
// is a 400 VALIDATION_FAILED naming the "cursor" field (pagination.Decode
// already builds that error).
func decodeCursor(requested *gen.Cursor) (time.Time, uuid.UUID, error) {
	if requested == nil || *requested == "" {
		return time.Time{}, uuid.Nil, nil
	}
	return pagination.Decode(*requested)
}

// cursorPtr returns nil for a zero-value createdAt (meaning "no cursor,
// first page") and a pointer to it otherwise — the shape
// db.ListLocationsParams/db.ListUsersParams expect.
func cursorPtr(createdAt time.Time, id uuid.UUID) (*time.Time, *uuid.UUID) {
	if createdAt.IsZero() {
		return nil, nil
	}
	return &createdAt, &id
}

// paginateT trims rows (fetched with limit+1) down to at most limit items
// and reports the opaque cursor for the next page — non-nil exactly when
// a limit+1'th row proved more data exists — without any sqlc query
// needing to compute "is there another page" itself.
func paginateT[T any](rows []T, limit int32, keyOf func(T) (time.Time, uuid.UUID)) ([]T, *string) {
	if len(rows) <= int(limit) {
		return rows, nil
	}
	items := rows[:limit]
	createdAt, id := keyOf(items[len(items)-1])
	cursor := pagination.Encode(createdAt, id)
	return items, &cursor
}

// nextCursorResponse converts paginateT's *string (nil = no further page)
// into the tri-state nullable.Nullable[string] the generated `nextCursor`
// field now requires (D-35's nullable-type ripple: every `["T", "null"]`
// schema in the spec gets this type, not just the PATCH fields D-35
// introduced it for). The field is always required in a response, so the
// result is always explicitly specified: null when there is no next page,
// the cursor value otherwise.
func nextCursorResponse(cursor *string) nullable.Nullable[string] {
	if cursor == nil {
		return nullable.NewNullNullable[string]()
	}
	return nullable.NewNullableWithValue(*cursor)
}
