package crm

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Sulton-Ali/savdo/api/internal/apierr"
)

// conflictField maps a unique-violation (pgx error code 23505) to the
// API's `details.field` name by constraint name — never by parsing the
// driver's error message text. Mirrors catalog.conflictField. Returns
// ok=false for any other error, so callers fall back to wrapping the
// error as a 500 rather than mis-reporting it as a conflict.
func conflictField(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return "", false
	}
	switch pgErr.ConstraintName {
	case "suppliers_shop_id_name_key":
		return "name", true
	default:
		return "", false
	}
}

// mapWriteError maps a write's error against the constraint this package
// knows how to translate: a unique violation (409 CONFLICT, conflictField).
// Callers fall back to wrapping the error as a 500 when ok is false.
func mapWriteError(err error) (*apierr.Error, bool) {
	if field, ok := conflictField(err); ok {
		return apierr.Conflict(field), true
	}
	return nil, false
}
