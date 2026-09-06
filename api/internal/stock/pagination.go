package stock

import (
	"encoding/base64"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/pagination"
)

// defaultLimit and maxLimit bound every stock collection endpoint
// (docs/05-API.md § Conventions: "limit is 1-200, default 50") — mirrors
// catalog.defaultLimit/maxLimit and shop's own copy.
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

// levelCursorSeparator joins the three encoded fields of a levels cursor —
// same role as internal/pagination's own separator, private to this file
// because ListLevels' keyset is (variant_created_at, variant_id,
// location_id), a three-column, mixed-direction shape unlike
// internal/pagination's fixed (created_at, id). ListLow's keyset (D-92:
// variant_created_at DESC, variant_id DESC) matches internal/pagination's
// shape exactly, so low.go reuses that package directly instead of a
// second codec here.
const levelCursorSeparator = "|"

// encodeLevelCursor builds GET /stock/levels' opaque cursor from the last
// row of a page: (variant_created_at, variant_id, location_id) — newest
// variant first, then location (D-92). variant_created_at is formatted
// RFC 3339 with nanosecond precision, so no ordering information is lost,
// the same convention internal/pagination.Encode uses.
func encodeLevelCursor(variantCreatedAt time.Time, variantID, locationID uuid.UUID) string {
	raw := variantCreatedAt.UTC().Format(time.RFC3339Nano) + levelCursorSeparator +
		variantID.String() + levelCursorSeparator + locationID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeLevelCursor reverses encodeLevelCursor. An empty/nil cursor means
// "first page" (zero time.Time, uuid.Nil, uuid.Nil, nil); anything
// malformed is a 400 VALIDATION_FAILED naming the cursor field, the same
// treatment internal/pagination.Decode gives a bad (created_at, id)
// cursor.
func decodeLevelCursor(requested *gen.Cursor) (time.Time, uuid.UUID, uuid.UUID, error) {
	if requested == nil || *requested == "" {
		return time.Time{}, uuid.Nil, uuid.Nil, nil
	}
	invalid := func() (time.Time, uuid.UUID, uuid.UUID, error) {
		return time.Time{}, uuid.Nil, uuid.Nil, apierr.Validation(map[string]string{"cursor": "invalid"})
	}
	raw, err := base64.RawURLEncoding.DecodeString(*requested)
	if err != nil {
		return invalid()
	}
	createdAtStr, rest, found := strings.Cut(string(raw), levelCursorSeparator)
	if !found {
		return invalid()
	}
	variantStr, locationStr, found := strings.Cut(rest, levelCursorSeparator)
	if !found {
		return invalid()
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtStr)
	if err != nil {
		return invalid()
	}
	variantID, err := uuid.Parse(variantStr)
	if err != nil {
		return invalid()
	}
	locationID, err := uuid.Parse(locationStr)
	if err != nil {
		return invalid()
	}
	return createdAt, variantID, locationID, nil
}

// levelCursorPtr returns (nil, nil, nil) for a zero-value variantCreatedAt
// (meaning "no cursor, first page") and pointers to all three fields
// otherwise — the shape db.ListLevelsParams'
// CursorVariantCreatedAt/CursorVariantID/CursorLocationID expect. Mirrors
// movements.go's movementCursorPtr idiom, extended to a third field.
func levelCursorPtr(variantCreatedAt time.Time, variantID, locationID uuid.UUID) (*time.Time, *uuid.UUID, *uuid.UUID) {
	if variantCreatedAt.IsZero() {
		return nil, nil, nil
	}
	return &variantCreatedAt, &variantID, &locationID
}

// lowCursorPtr is movements.go's movementCursorPtr for ListLow's
// (variant_created_at, variant_id) keyset (D-92) — both columns sort
// DESC, the same shape internal/pagination already codes for.
func lowCursorPtr(variantCreatedAt time.Time, variantID uuid.UUID) (*time.Time, *uuid.UUID) {
	if variantCreatedAt.IsZero() {
		return nil, nil
	}
	return &variantCreatedAt, &variantID
}

// paginateLevels trims rows (fetched with limit+1) down to at most limit
// items and reports the opaque cursor for the next page — non-nil exactly
// when a limit+1'th row proved more data exists. Mirrors catalog.paginateT,
// specialized to ListLevelsRow's (variant_created_at, variant_id,
// location_id) key.
func paginateLevels(rows []db.ListLevelsRow, limit int32) ([]db.ListLevelsRow, *string) {
	// Compare in int (widening limit, never narrowing len(rows)) — mirrors
	// catalog.paginateT/movements.go's own paginateMovements; avoids a
	// len(rows)->int32 narrowing conversion gosec (G115) flags on
	// principle even though a page can never realistically hold anywhere
	// near math.MaxInt32 rows.
	if len(rows) <= int(limit) {
		return rows, nil
	}
	items := rows[:limit]
	last := items[len(items)-1]
	cursor := encodeLevelCursor(last.VariantCreatedAt, last.VariantID, last.LocationID)
	return items, &cursor
}

// paginateLow is paginateLevels for ListLow's (variant_created_at,
// variant_id) key (D-92), encoded with internal/pagination since both
// columns sort DESC — the same shape movements.go's paginateMovements
// already uses.
func paginateLow(rows []db.ListLowRow, limit int32) ([]db.ListLowRow, *string) {
	// Compare in int (widening limit, never narrowing len(rows)) — mirrors
	// catalog.paginateT/movements.go's own paginateMovements; avoids a
	// len(rows)->int32 narrowing conversion gosec (G115) flags on
	// principle even though a page can never realistically hold anywhere
	// near math.MaxInt32 rows.
	if len(rows) <= int(limit) {
		return rows, nil
	}
	items := rows[:limit]
	last := items[len(items)-1]
	cursor := pagination.Encode(last.VariantCreatedAt, last.VariantID)
	return items, &cursor
}
