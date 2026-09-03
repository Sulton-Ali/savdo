package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// TestMalformedRequestBody proves the strict server's own JSON decode step
// runs (and is mapped to VALIDATION_FAILED) before Login — or its
// middleware — ever runs: a malformed body never reaches auth.Handler.Login
// or auth.Service.Middleware at all.
func TestMalformedRequestBody(t *testing.T) {
	router := NewRouter(testLogger(), nil, testAuthService())

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}

	var body gen.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Code != gen.VALIDATIONFAILED {
		t.Fatalf("error.code = %q, want %q", body.Error.Code, gen.VALIDATIONFAILED)
	}
	if body.Error.Details == nil || (*body.Error.Details)["reason"] != "bad_request" {
		t.Fatalf("error.details = %+v, want reason=bad_request", body.Error.Details)
	}
}
