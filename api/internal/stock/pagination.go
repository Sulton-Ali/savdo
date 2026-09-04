package stock

import (
	"encoding/base64"
	"strings"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
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

// levelCursorSeparator joins the two encoded fields of a levels cursor —
// same role as internal/pagination's own separator, private to this file
// because ListLevels' keyset is (variant_id, location_id), a different
// shape from internal/pagination's fixed (created_at, id). ListMovements'
// keyset matches internal/pagination's shape exactly, so movements.go
// reuses that package directly instead of a third codec here.
const levelCursorSeparator = "|"

// encodeLevelCursor builds GET /stock/levels' opaque cursor from the last
// row of a page: (variant_id, location_id), the immutable identity part of
// stock_levels' primary key (stable across concurrent stock moves, unlike
// updated_at — see api/db/queries/stock.sql's own ListLevels comment).
func encodeLevelCursor(variantID, locationID uuid.UUID) string {
	raw := variantID.String() + levelCursorSeparator + locationID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeLevelCursor reverses encodeLevelCursor. An empty/nil cursor means
// "first page" (uuid.Nil, uuid.Nil, nil); anything malformed is a 400
// VALIDATION_FAILED naming the cursor field, the same treatment
// internal/pagination.Decode gives a bad (created_at, id) cursor.
func decodeLevelCursor(requested *gen.Cursor) (uuid.UUID, uuid.UUID, error) {
	if requested == nil || *requested == "" {
		return uuid.Nil, uuid.Nil, nil
	}
	invalid := func() (uuid.UUID, uuid.UUID, error) {
		return uuid.Nil, uuid.Nil, apierr.Validation(map[string]string{"cursor": "invalid"})
	}
	raw, err := base64.RawURLEncoding.DecodeString(*requested)
	if err != nil {
		return invalid()
	}
	variantStr, locationStr, found := strings.Cut(string(raw), levelCursorSeparator)
	if !found {
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
	return variantID, locationID, nil
}

// levelCursorPtr returns (nil, nil) for a zero-value (variantID, locationID)
// (meaning "no cursor, first page") and pointers to them otherwise — the
// shape db.ListLevelsParams' CursorVariantID/CursorLocationID expect.
// Mirrors catalog.cursorPtr's *time.Time/*uuid.UUID idiom, adapted to two
// uuid.UUID values.
func levelCursorPtr(variantID, locationID uuid.UUID) (*uuid.UUID, *uuid.UUID) {
	if variantID == uuid.Nil {
		return nil, nil
	}
	return &variantID, &locationID
}

// decodeLowCursor resolves GET /stock/low's `?cursor=` to the variant_id
// it encodes (ListLow's cursor is a single column — variant_id is stable
// and unique, per api/db/queries/stock.sql's own ListLow comment), or
// uuid.Nil for a first page. Malformed input is 400 VALIDATION_FAILED
// naming cursor, same as decodeLevelCursor.
func decodeLowCursor(requested *gen.Cursor) (uuid.UUID, error) {
	if requested == nil || *requested == "" {
		return uuid.Nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(*requested)
	if err != nil {
		return uuid.Nil, apierr.Validation(map[string]string{"cursor": "invalid"})
	}
	id, err := uuid.Parse(string(raw))
	if err != nil {
		return uuid.Nil, apierr.Validation(map[string]string{"cursor": "invalid"})
	}
	return id, nil
}

// encodeLowCursor builds the opaque cursor for the last row of a
// GET /stock/low page.
func encodeLowCursor(variantID uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(variantID.String()))
}

// lowCursorPtr is levelCursorPtr for ListLow's single-column cursor.
func lowCursorPtr(variantID uuid.UUID) *uuid.UUID {
	if variantID == uuid.Nil {
		return nil
	}
	return &variantID
}

// paginateLevels trims rows (fetched with limit+1) down to at most limit
// items and reports the opaque cursor for the next page — non-nil exactly
// when a limit+1'th row proved more data exists. Mirrors catalog.paginateT,
// specialized to ListLevelsRow's (variant_id, location_id) key.
func paginateLevels(rows []db.ListLevelsRow, limit int32) ([]db.ListLevelsRow, *string) {
	if int32(len(rows)) <= limit {
		return rows, nil
	}
	items := rows[:limit]
	last := items[len(items)-1]
	cursor := encodeLevelCursor(last.VariantID, last.LocationID)
	return items, &cursor
}

// paginateLow is paginateLevels for ListLow's single-column key.
func paginateLow(rows []db.ListLowRow, limit int32) ([]db.ListLowRow, *string) {
	if int32(len(rows)) <= limit {
		return rows, nil
	}
	items := rows[:limit]
	last := items[len(items)-1]
	cursor := encodeLowCursor(last.VariantID)
	return items, &cursor
}
