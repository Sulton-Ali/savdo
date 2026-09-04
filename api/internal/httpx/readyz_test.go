package httpx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

func TestReadyz_okWhenDatabaseIsReachable(t *testing.T) {
	pool := testdb.New(t)
	router := NewRouter(testLogger(), pool, testAuthService(), testShopService(), testMediaService(), nil, testCatalogService())

	req := httptest.NewRequest(http.MethodGet, "/v1/readyz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body gen.Readiness
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != gen.ReadinessStatusOk {
		t.Fatalf("status = %q, want %q", body.Status, gen.ReadinessStatusOk)
	}
	if body.Checks.Db != gen.ReadinessChecksDbOk {
		t.Fatalf("checks.db = %q, want %q", body.Checks.Db, gen.ReadinessChecksDbOk)
	}
}

func TestReadyz_degradedWhenDatabaseIsUnreachable(t *testing.T) {
	// A pool pointed at a port nothing listens on: pgxpool.New is lazy (no
	// dial at construction), so this succeeds, and the subsequent Ping
	// inside GetReadyz fails fast and deterministically with "connection
	// refused" well inside the 1s readyz timeout — no shared fixture
	// (internal/db/testdb's container/pool) is touched or closed.
	pool, err := pgxpool.New(context.Background(), "postgres://savdo:savdo@127.0.0.1:5999/savdo?sslmode=disable")
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	router := NewRouter(testLogger(), pool, testAuthService(), testShopService(), testMediaService(), nil, testCatalogService())

	req := httptest.NewRequest(http.MethodGet, "/v1/readyz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}

	var body gen.Readiness
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != gen.ReadinessStatusDegraded {
		t.Fatalf("status = %q, want %q", body.Status, gen.ReadinessStatusDegraded)
	}
	if body.Checks.Db != gen.ReadinessChecksDbError {
		t.Fatalf("checks.db = %q, want %q", body.Checks.Db, gen.ReadinessChecksDbError)
	}
}
