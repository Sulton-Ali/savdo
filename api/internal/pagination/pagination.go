// Package pagination implements the opaque cursor used by every
// cursor-paginated collection endpoint (docs/05-API.md § Conventions):
// `?limit=&cursor=` in, `{ items, nextCursor }` out. The cursor encodes the
// keyset (created_at, id) of the last row on the current page so the next
// page can resume with `(created_at, id) < (cursor_created_at, cursor_id)`
// — the same keyset-pagination shape sqlc's generated ListX queries already
// use (see e.g. internal/db.ListUsersParams).
package pagination

import (
	"encoding/base64"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/internal/apierr"
)

// separator joins the two encoded fields. It is not a character RFC 3339
// Nano or a UUID's canonical string form can ever contain, so splitting on
// it is unambiguous.
const separator = "|"

// Encode builds an opaque cursor from the last row of a page: the row's
// created_at (RFC 3339 with nanosecond precision, so no ordering
// information is lost) and id, base64url-encoded so the result is safe in
// a query string without further escaping.
func Encode(createdAt time.Time, id uuid.UUID) string {
	raw := createdAt.UTC().Format(time.RFC3339Nano) + separator + id.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// Decode reverses Encode. Any malformed cursor — not base64, missing the
// separator, an unparseable timestamp or UUID — is a client error: it maps
// to a 400 VALIDATION_FAILED naming the "cursor" field, never a 500 or a
// panic, since a cursor is client-supplied input from a previous response
// that the client could have tampered with or truncated.
func Decode(cursor string) (time.Time, uuid.UUID, error) {
	invalid := func() (time.Time, uuid.UUID, error) {
		return time.Time{}, uuid.Nil, apierr.Validation(map[string]string{"cursor": "invalid"})
	}

	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return invalid()
	}

	createdAtStr, idStr, found := strings.Cut(string(raw), separator)
	if !found {
		return invalid()
	}

	createdAt, err := time.Parse(time.RFC3339Nano, createdAtStr)
	if err != nil {
		return invalid()
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		return invalid()
	}

	return createdAt, id, nil
}
