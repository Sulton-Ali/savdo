package apierr_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
)

func TestConstructors(t *testing.T) {
	tests := []struct {
		name       string
		err        *apierr.Error
		wantStatus int
		wantCode   gen.ErrorCode
		wantDetail func(t *testing.T, details map[string]any)
	}{
		{
			name:       "Validation",
			err:        apierr.Validation(map[string]string{"email": "required"}),
			wantStatus: http.StatusBadRequest,
			wantCode:   gen.VALIDATIONFAILED,
			wantDetail: func(t *testing.T, d map[string]any) {
				fields, ok := d["fields"].(map[string]string)
				if !ok || fields["email"] != "required" {
					t.Fatalf("details.fields = %+v, want {email: required}", d["fields"])
				}
			},
		},
		{
			name:       "Unprocessable",
			err:        apierr.Unprocessable(map[string]string{"items[0].variantId": "invalid"}),
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   gen.VALIDATIONFAILED,
			wantDetail: func(t *testing.T, d map[string]any) {
				fields, ok := d["fields"].(map[string]string)
				if !ok || fields["items[0].variantId"] != "invalid" {
					t.Fatalf("details.fields = %+v, want {items[0].variantId: invalid}", d["fields"])
				}
			},
		},
		{
			name:       "Unauthenticated",
			err:        apierr.Unauthenticated(),
			wantStatus: http.StatusUnauthorized,
			wantCode:   gen.UNAUTHENTICATED,
		},
		{
			name:       "Forbidden",
			err:        apierr.Forbidden(),
			wantStatus: http.StatusForbidden,
			wantCode:   gen.FORBIDDEN,
		},
		{
			name:       "NotFound",
			err:        apierr.NotFound("product"),
			wantStatus: http.StatusNotFound,
			wantCode:   gen.NOTFOUND,
			wantDetail: func(t *testing.T, d map[string]any) {
				if d["entity"] != "product" {
					t.Fatalf("details.entity = %v, want product", d["entity"])
				}
			},
		},
		{
			name:       "Conflict",
			err:        apierr.Conflict("sku"),
			wantStatus: http.StatusConflict,
			wantCode:   gen.CONFLICT,
			wantDetail: func(t *testing.T, d map[string]any) {
				if d["field"] != "sku" {
					t.Fatalf("details.field = %v, want sku", d["field"])
				}
			},
		},
		{
			name:       "RateLimited",
			err:        apierr.RateLimited(30),
			wantStatus: http.StatusTooManyRequests,
			wantCode:   gen.RATELIMITED,
			wantDetail: func(t *testing.T, d map[string]any) {
				if d["retryAfterSeconds"] != 30 {
					t.Fatalf("details.retryAfterSeconds = %v, want 30", d["retryAfterSeconds"])
				}
			},
		},
		{
			name:       "Internal",
			err:        apierr.Internal(),
			wantStatus: http.StatusInternalServerError,
			wantCode:   gen.INTERNAL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Status != tt.wantStatus {
				t.Fatalf("Status = %d, want %d", tt.err.Status, tt.wantStatus)
			}
			if tt.err.Code != tt.wantCode {
				t.Fatalf("Code = %q, want %q", tt.err.Code, tt.wantCode)
			}
			if tt.wantDetail != nil {
				tt.wantDetail(t, tt.err.Details)
			}
			if tt.err.Error() == "" {
				t.Fatal("Error() = \"\", want a non-empty message")
			}
		})
	}
}

func TestWrite_typedErrorPassesThrough(t *testing.T) {
	rec := httptest.NewRecorder()
	apierr.Write(rec, apierr.NotFound("shop"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}

	var body gen.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Code != gen.NOTFOUND {
		t.Fatalf("error.code = %q, want %q", body.Error.Code, gen.NOTFOUND)
	}
	if body.Error.Details == nil || (*body.Error.Details)["entity"] != "shop" {
		t.Fatalf("error.details = %+v, want entity=shop", body.Error.Details)
	}
}

func TestWrite_unknownErrorMapsToInternalWithoutLeakingMessage(t *testing.T) {
	rec := httptest.NewRecorder()
	secret := "pq: password authentication failed for user \"savdo\""
	apierr.Write(rec, errors.New(secret))

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
	if strings.Contains(rec.Body.String(), secret) {
		t.Fatalf("response body leaked the underlying error message: %s", rec.Body.String())
	}
}

// TestWrite_maxBytesErrorMapsToBodyTooLarge is Review A MAJOR 1's test at
// the apierr layer: a handler that reads its own request body past its
// route's limit (internal/media, for POST /media's multipart body) gets
// a bare *http.MaxBytesError back from that read, not something already
// wrapped in *apierr.Error the way every other handler-raised error in
// this codebase is — asError must still map it to the same 400
// VALIDATION_FAILED / body_too_large shape router.go's writeRequestError
// uses for the pre-handler decode case, not fall through to a bare 500.
func TestWrite_maxBytesErrorMapsToBodyTooLarge(t *testing.T) {
	maxBytesErr := &http.MaxBytesError{Limit: 12 << 20}

	tests := []struct {
		name string
		err  error
	}{
		{"bare", maxBytesErr},
		{"wrapped", fmt.Errorf("media: spool upload: %w", maxBytesErr)},
		{"doubly wrapped", fmt.Errorf("media: upload: %w", fmt.Errorf("media: spool upload: %w", maxBytesErr))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			apierr.Write(rec, tt.err)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}

			var body gen.Error
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.Error.Code != gen.VALIDATIONFAILED {
				t.Fatalf("error.code = %q, want %q", body.Error.Code, gen.VALIDATIONFAILED)
			}
			if body.Error.Details == nil {
				t.Fatalf("error has no details: %+v", body)
			}
			if reason, _ := (*body.Error.Details)["reason"].(string); reason != "body_too_large" {
				t.Fatalf("details.reason = %v, want body_too_large", reason)
			}
		})
	}
}

func TestWrite_wrappedTypedErrorIsStillMapped(t *testing.T) {
	rec := httptest.NewRecorder()
	apierr.Write(rec, errFmt(apierr.Conflict("sku")))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func errFmt(inner *apierr.Error) error {
	return &wrapped{inner: inner}
}

type wrapped struct{ inner *apierr.Error }

func (w *wrapped) Error() string { return "wrapped: " + w.inner.Error() }
func (w *wrapped) Unwrap() error { return w.inner }

func TestWrite_rateLimitedSetsRetryAfter(t *testing.T) {
	rec := httptest.NewRecorder()
	apierr.Write(rec, apierr.RateLimited(42))

	if got := rec.Header().Get("Retry-After"); got != "42" {
		t.Fatalf("Retry-After = %q, want %q", got, "42")
	}
}

func TestWrite_setsContentType(t *testing.T) {
	rec := httptest.NewRecorder()
	apierr.Write(rec, apierr.Forbidden())

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
}

func TestWrite_logs5xxWithRequestID(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	rec := httptest.NewRecorder()
	rec.Header().Set(apierr.RequestIDHeader, "req-123")
	apierr.Write(rec, apierr.Internal())

	logged := buf.String()
	if !strings.Contains(logged, "req-123") {
		t.Fatalf("log output = %q, want it to contain the request id", logged)
	}
}

func TestWrite_doesNotLog4xx(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	rec := httptest.NewRecorder()
	apierr.Write(rec, apierr.Forbidden())

	if buf.Len() != 0 {
		t.Fatalf("log output = %q, want no log line for a 4xx", buf.String())
	}
}

func TestWrite_readsRequestIDFromResponseHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set(apierr.RequestIDHeader, "test-request-id")

	// A 4xx does not need to prove the header round-trips through a log
	// line (that is exercised by the httpx integration test that checks
	// 5xx logging), but Write must not choke on it being present either.
	apierr.Write(rec, apierr.Validation(map[string]string{"name": "required"}))

	if rec.Header().Get(apierr.RequestIDHeader) != "test-request-id" {
		t.Fatalf("Write must not clear a pre-set X-Request-Id header")
	}
}
