package public_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/catalog"
	"github.com/Sulton-Ali/savdo/api/internal/config"
	"github.com/Sulton-Ali/savdo/api/internal/content"
	"github.com/Sulton-Ali/savdo/api/internal/crm"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
	"github.com/Sulton-Ali/savdo/api/internal/httpx"
	"github.com/Sulton-Ali/savdo/api/internal/media"
	"github.com/Sulton-Ali/savdo/api/internal/public"
	"github.com/Sulton-Ali/savdo/api/internal/reports"
	"github.com/Sulton-Ali/savdo/api/internal/sales"
	"github.com/Sulton-Ali/savdo/api/internal/shop"
	"github.com/Sulton-Ali/savdo/api/internal/stock"
)

// queryCounter is a db.DBTX that counts every Query/QueryRow call it
// forwards — used to prove a cache hit never reaches the database
// (TestPublicCache_secondCallServedFromCache_noExtraDBHit).
type queryCounter struct {
	db.DBTX
	n atomic.Int64
}

func (c *queryCounter) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	c.n.Add(1)
	return c.DBTX.Query(ctx, sql, args...)
}

func (c *queryCounter) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	c.n.Add(1)
	return c.DBTX.QueryRow(ctx, sql, args...)
}

func (c *queryCounter) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	c.n.Add(1)
	return c.DBTX.Exec(ctx, sql, args...)
}

// newTestRouter builds the real internal/httpx router (the actual `mux` a
// deployed process serves) with a real public.Service/content.Service
// pair backed by pool, wired the way cmd/api/main.go wires them
// (contentSvc.SetInvalidator(publicSvc)) — every other service is a nil-
// safe stand-in, mirroring internal/httpx/router_test.go's own
// testXxxService helpers, since these tests never exercise a route
// besides `/public/*` and `/healthz`.
func newTestRouter(pool *pgxpool.Pool, q *db.Queries, publicShopSlug string) (http.Handler, *public.Service) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	authSvc := auth.NewService(nil, config.Config{}, uuid.New())
	shopSvc := shop.NewService(nil, nil)
	mediaSvc := media.NewService(nil, nil, "/media", 10<<20, 2, 10)
	catalogSvc := catalog.NewService(nil, nil, "uz", "/media")
	stockSvc := stock.NewService(nil, nil)
	crmSvc := crm.NewService(nil)
	reportsSvc := reports.NewService(nil)
	salesSvc := sales.NewService(nil)
	contentSvc := content.NewService(q)
	publicSvc := public.NewService(q, contentSvc, publicShopSlug, "/media")
	contentSvc.SetInvalidator(publicSvc)

	router := httpx.NewRouter(logger, pool, authSvc, shopSvc, mediaSvc, nil, catalogSvc, stockSvc, crmSvc, reportsSvc, salesSvc, contentSvc, publicSvc)
	return router, publicSvc
}

func TestPublicRoutes_noLoginRequired_bogusTokenIgnored(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	q := db.New(pool)
	ctx := context.Background()
	seedShop(ctx, t, q, "shop-a")

	router, _ := newTestRouter(pool, q, "shop-a")

	req := httptest.NewRequest(http.MethodGet, "/v1/public/shop", nil)
	req.Header.Set("Authorization", "Bearer this-is-not-a-real-session-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200 (a bogus bearer token must be ignored on an allow-listed public route)", rec.Code, rec.Body.String())
	}
}

func TestPublicCache_etagAndIfNoneMatch304_andNoExtraDBHit(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	counter := &queryCounter{DBTX: pool}
	q := db.New(counter)
	ctx := context.Background()
	seedShop(ctx, t, q, "shop-a")

	router, _ := newTestRouter(pool, q, "shop-a")

	req1 := httptest.NewRequest(http.MethodGet, "/v1/public/shop", nil)
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first call status = %d, body = %s", rec1.Code, rec1.Body.String())
	}
	cacheControl := rec1.Header().Get("Cache-Control")
	if cacheControl != "public, max-age=60" {
		t.Errorf("Cache-Control = %q, want %q", cacheControl, "public, max-age=60")
	}
	etag := rec1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("ETag header missing on the first (uncached) response")
	}
	body1 := rec1.Body.String()
	n1 := counter.n.Load()
	if n1 == 0 {
		t.Fatal("the first call made zero database queries — the test fixture is broken, not the cache")
	}

	// Second call, no If-None-Match: served from cache, identical ETag
	// and body, and — the part that matters — zero additional queries.
	req2 := httptest.NewRequest(http.MethodGet, "/v1/public/shop", nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second call status = %d", rec2.Code)
	}
	if rec2.Header().Get("ETag") != etag {
		t.Errorf("second call ETag = %q, want the same %q", rec2.Header().Get("ETag"), etag)
	}
	if rec2.Body.String() != body1 {
		t.Errorf("second call body differs from the first — cache should serve byte-identical bytes")
	}
	if got := counter.n.Load(); got != n1 {
		t.Errorf("queries after the second call = %d, want unchanged from %d (served from cache, no DB hit)", got, n1)
	}

	// Third call, with If-None-Match matching the ETag: 304, no body,
	// still no additional query.
	req3 := httptest.NewRequest(http.MethodGet, "/v1/public/shop", nil)
	req3.Header.Set("If-None-Match", etag)
	rec3 := httptest.NewRecorder()
	router.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", rec3.Code)
	}
	if rec3.Body.Len() != 0 {
		t.Errorf("304 body = %q, want empty", rec3.Body.String())
	}
	if got := counter.n.Load(); got != n1 {
		t.Errorf("queries after the 304 call = %d, want unchanged from %d", got, n1)
	}
}

func TestPublicCache_contentPUT_invalidatesSoNextGETReflectsChange(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	q := db.New(pool)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	user := seedUser(ctx, t, q, shopRow.ID, "owner")

	contentSvc := content.NewService(q)
	publicSvc := public.NewService(q, contentSvc, "shop-a", "/media")
	contentSvc.SetInvalidator(publicSvc)
	router := newTestRouterWithServices(pool, publicSvc, contentSvc)

	if _, err := contentSvc.Upsert(ctx, shopRow.ID, "hero", "uz", map[string]interface{}{"title": "Before"}, user.ID); err != nil {
		t.Fatalf("Upsert(before): %v", err)
	}

	req1 := httptest.NewRequest(http.MethodGet, "/v1/public/shop", nil)
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK || !strings.Contains(rec1.Body.String(), `"title":"Before"`) {
		t.Fatalf("first GET = %d %s, want 200 with the Before title", rec1.Code, rec1.Body.String())
	}

	// A content PUT (through the same content.Service the admin/content
	// endpoints use) must clear the cache — the next GET must not still
	// serve the pre-PUT bytes for the rest of the 60 s window.
	if _, err := contentSvc.Upsert(ctx, shopRow.ID, "hero", "uz", map[string]interface{}{"title": "After"}, user.ID); err != nil {
		t.Fatalf("Upsert(after): %v", err)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/v1/public/shop", nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK || !strings.Contains(rec2.Body.String(), `"title":"After"`) {
		t.Fatalf("second GET after PUT = %d %s, want 200 with the After title (cache must have been invalidated)", rec2.Code, rec2.Body.String())
	}
}

// newTestRouterWithServices is newTestRouter, but taking an
// already-built publicSvc/contentSvc pair instead of constructing its
// own — TestPublicCache_contentPUT_invalidatesSoNextGETReflectsChange
// needs to call contentSvc.Upsert directly, outside the router, so it
// must hold the same instances the router forwards to.
func newTestRouterWithServices(pool *pgxpool.Pool, publicSvc *public.Service, contentSvc *content.Service) http.Handler {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	authSvc := auth.NewService(nil, config.Config{}, uuid.New())
	shopSvc := shop.NewService(nil, nil)
	mediaSvc := media.NewService(nil, nil, "/media", 10<<20, 2, 10)
	catalogSvc := catalog.NewService(nil, nil, "uz", "/media")
	stockSvc := stock.NewService(nil, nil)
	crmSvc := crm.NewService(nil)
	reportsSvc := reports.NewService(nil)
	salesSvc := sales.NewService(nil)
	return httpx.NewRouter(logger, pool, authSvc, shopSvc, mediaSvc, nil, catalogSvc, stockSvc, crmSvc, reportsSvc, salesSvc, contentSvc, publicSvc)
}
