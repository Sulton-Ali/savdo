package catalog

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Sulton-Ali/savdo/api/gen"
)

func TestMapWriteError(t *testing.T) {
	if _, ok := mapWriteError(nil); ok {
		t.Error("mapWriteError(nil) ok = true, want false")
	}

	unique := &pgconn.PgError{Code: "23505", ConstraintName: "products_shop_id_slug_key"}
	apiErr, ok := mapWriteError(unique)
	if !ok || apiErr.Code != gen.CONFLICT || apiErr.Details["field"] != "slug" {
		t.Errorf("mapWriteError(unique slug) = %+v, ok=%v, want 409 CONFLICT field=slug", apiErr, ok)
	}

	fk := &pgconn.PgError{Code: "23503", ConstraintName: "products_unit_id_fkey"}
	apiErr, ok = mapWriteError(fk)
	if !ok || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("mapWriteError(fk unitId) ok=%v, err=%+v, want 400 VALIDATION_FAILED", ok, apiErr)
	}
	if fields := apiErr.Details["fields"].(map[string]string); fields["unitId"] != "invalid" {
		t.Errorf("fields = %+v, want unitId=invalid", fields)
	}

	outOfRange := &pgconn.PgError{Code: "22003"}
	apiErr, ok = mapWriteError(outOfRange)
	if !ok || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("mapWriteError(22003) ok=%v, err=%+v, want 400 VALIDATION_FAILED", ok, apiErr)
	}
	if fields := apiErr.Details["fields"].(map[string]string); fields["amount"] != "invalid" {
		t.Errorf("fields = %+v, want amount=invalid", fields)
	}

	unrecognized := &pgconn.PgError{Code: "42P01"}
	if _, ok := mapWriteError(unrecognized); ok {
		t.Error("mapWriteError(unrecognized code) ok = true, want false")
	}
}
