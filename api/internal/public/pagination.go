package public

import (
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/pagination"
)

// defaultLimit and maxLimit mirror catalog's own (docs/05-API.md §
// Conventions: "limit is 1-200, default 50") — the same bounds, applied
// to GET /public/products, this package's only cursor-paginated
// collection.
const (
	defaultLimit = 50
	maxLimit     = 200
)

// clampLimit resolves `?limit=` (nil or non-positive means "use the
// default") to [1, maxLimit] — the same rule catalog.clampLimit applies.
func clampLimit(requested *int) int32 {
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

// decodeCursor resolves `?cursor=` to the keyset it encodes — nil, nil
// for a first page (empty or absent cursor), a 400 VALIDATION_FAILED
// error (pagination.Decode) for a malformed one.
func decodeCursor(requested *string) (*time.Time, *uuid.UUID, error) {
	if requested == nil || *requested == "" {
		return nil, nil, nil
	}
	createdAt, id, err := pagination.Decode(*requested)
	if err != nil {
		return nil, nil, err
	}
	return &createdAt, &id, nil
}

// paginateProducts trims rows (fetched with limit+1) down to at most
// limit items and reports the opaque cursor for the next page — the same
// shape catalog.paginateT builds, specialized to
// db.ListPublicProductsRow since this package has no shared generic
// row-with-(created_at,id) type to parametrize over without adding one
// (not worth it for the single caller here).
func paginateProducts(rows []db.ListPublicProductsRow, limit int32) ([]db.ListPublicProductsRow, *string) {
	if len(rows) <= int(limit) {
		return rows, nil
	}
	items := rows[:limit]
	last := items[len(items)-1]
	cursor := pagination.Encode(last.CreatedAt, last.ID)
	return items, &cursor
}

// productIDs extracts each row's id, for the single batched
// ListCoverImagesForProducts/SumVariantQtyForProducts call per page
// (D-83's own "one query per page, not per product" pattern).
func productIDs(rows []db.ListPublicProductsRow) []uuid.UUID {
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids
}
