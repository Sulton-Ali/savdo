package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// TestUnimplementedOperations proves the strict server routes every Phase 1
// operation added by contracts/openapi.yaml — even before its owning module
// (T3/T4/T5) implements it — and that each unimplemented one answers with
// the shared Error envelope (ADR-013) rather than a generic 500 or panic.
func TestUnimplementedOperations(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"getMe", http.MethodGet, "/v1/auth/me"},
		{"logout", http.MethodPost, "/v1/auth/logout"},
		{"listSessions", http.MethodGet, "/v1/auth/sessions"},
		{"getShop", http.MethodGet, "/v1/shop"},
		{"listLocations", http.MethodGet, "/v1/locations"},
		{"listStaff", http.MethodGet, "/v1/staff"},
	}

	router := NewRouter(testLogger(), nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
			}

			var body gen.Error
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.Error.Code != gen.INTERNAL {
				t.Fatalf("error.code = %q, want %q", body.Error.Code, gen.INTERNAL)
			}
			if body.Error.Details == nil || (*body.Error.Details)["reason"] != "not_implemented" {
				t.Fatalf("error.details = %+v, want reason=not_implemented", body.Error.Details)
			}
		})
	}
}
