package httpx

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/catalog"
	"github.com/Sulton-Ali/savdo/api/internal/config"
	"github.com/Sulton-Ali/savdo/api/internal/crm"
	"github.com/Sulton-Ali/savdo/api/internal/media"
	"github.com/Sulton-Ali/savdo/api/internal/reports"
	"github.com/Sulton-Ali/savdo/api/internal/sales"
	"github.com/Sulton-Ali/savdo/api/internal/shop"
	"github.com/Sulton-Ali/savdo/api/internal/stock"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

// testAuthService builds an auth.Service safe to wire into NewRouter for
// tests that never authenticate a real session — GetHealthz/GetReadyz/
// Login pass through Middleware's allow-list without touching its
// db.Queries, and every other operation these tests hit is unauthenticated
// on purpose (they assert the 401/not-implemented shape, not a successful
// call), so a nil *db.Queries is never dereferenced.
func testAuthService() *auth.Service {
	return auth.NewService(nil, config.Config{}, uuid.New())
}

// testShopService builds a shop.Service safe to wire into NewRouter for
// tests that never exercise a `/shop`, `/locations` or `/staff` route — a
// nil pool and nil *db.Queries are never dereferenced in that case.
func testShopService() *shop.Service {
	return shop.NewService(nil, nil)
}

// testMediaService builds a media.Service safe to wire into NewRouter for
// tests that never exercise POST /media — a nil *db.Queries and a nil
// Storage are never dereferenced in that case.
func testMediaService() *media.Service {
	return media.NewService(nil, nil, "/media", 10<<20, 2, 10)
}

// testCatalogService builds a catalog.Service safe to wire into NewRouter
// for tests that never exercise a catalogue/product-image route — a nil
// pool and nil *db.Queries are never dereferenced in that case.
func testCatalogService() *catalog.Service {
	return catalog.NewService(nil, nil, "uz", "/media")
}

// testStockService builds a stock.Service safe to wire into NewRouter for
// tests that never exercise a `/stock/*` or `/purchases*` route — a nil
// pool and nil *db.Queries are never dereferenced in that case.
func testStockService() *stock.Service {
	return stock.NewService(nil, nil)
}

// testCrmService builds a crm.Service safe to wire into NewRouter for
// tests that never exercise a `/suppliers` route — a nil *db.Queries is
// never dereferenced in that case.
func testCrmService() *crm.Service {
	return crm.NewService(nil)
}

// testReportsService builds a reports.Service safe to wire into NewRouter
// for tests that never exercise a `/reports/sales/*` route — a nil
// *db.Queries is never dereferenced in that case.
func testReportsService() *reports.Service {
	return reports.NewService(nil)
}

// testSalesService builds a sales.Service safe to wire into NewRouter for
// tests that never exercise a `/sales*` route — a nil *db.Queries is
// never dereferenced in that case.
func testSalesService() *sales.Service {
	return sales.NewService(nil)
}

func TestHealthz(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		wantStatus     int
		wantBody       *gen.Healthz
		wantAllowedHdr bool
	}{
		{
			name:       "GET returns ok",
			method:     http.MethodGet,
			wantStatus: http.StatusOK,
			wantBody:   &gen.Healthz{Status: gen.HealthzStatusOk},
		},
		{
			// The generated std-http-server mux (Go 1.22 method+path
			// patterns) responds 405 with an Allow header when a path it
			// knows is hit with a method it doesn't serve.
			name:           "POST is not allowed",
			method:         http.MethodPost,
			wantStatus:     http.StatusMethodNotAllowed,
			wantAllowedHdr: true,
		},
	}

	router := NewRouter(testLogger(), nil, testAuthService(), testShopService(), testMediaService(), nil, testCatalogService(), testStockService(), testCrmService(), testReportsService(), testSalesService())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/v1/healthz", nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if tt.wantBody != nil {
				if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
					t.Fatalf("Content-Type = %q, want application/json", ct)
				}

				var got gen.Healthz
				if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				if got != *tt.wantBody {
					t.Fatalf("body = %+v, want %+v", got, *tt.wantBody)
				}
			}

			if tt.wantAllowedHdr && rec.Header().Get("Allow") == "" {
				t.Fatalf("expected an Allow header on 405")
			}

			if rec.Header().Get("X-Request-Id") == "" {
				t.Fatalf("expected X-Request-Id header to be set")
			}
		})
	}
}
