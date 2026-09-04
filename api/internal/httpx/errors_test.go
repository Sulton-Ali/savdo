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
	router := NewRouter(testLogger(), nil, testAuthService(), testShopService())

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

// TestOversizedRequestBodyIsRejectedNotAServerError proves a body over
// maxRequestBodyBytes maps to 400 VALIDATION_FAILED with
// details.reason=body_too_large — not a 500, and not an unbounded read —
// via maxBytesBody (bodylimit.go) wrapping every request in
// http.MaxBytesReader before the strict server's JSON decode ever sees it.
func TestOversizedRequestBodyIsRejectedNotAServerError(t *testing.T) {
	router := NewRouter(testLogger(), nil, testAuthService(), testShopService())

	// Must be syntactically valid JSON up to the point it overruns the
	// limit — a decoder that hits invalid syntax at byte 0 never needs to
	// keep reading, so it would report a plain JSON syntax error instead
	// of ever exercising http.MaxBytesReader's own error.
	payload := `{"username":"` + strings.Repeat("a", maxRequestBodyBytes+1) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(payload))
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
	if body.Error.Details == nil || (*body.Error.Details)["reason"] != "body_too_large" {
		t.Fatalf("error.details = %+v, want reason=body_too_large", body.Error.Details)
	}
}
