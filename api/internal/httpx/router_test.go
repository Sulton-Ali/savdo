package httpx

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Sulton-Ali/savdo/api/gen"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
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
			wantBody:   &gen.Healthz{Status: gen.Ok},
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

	router := NewRouter(testLogger())

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
