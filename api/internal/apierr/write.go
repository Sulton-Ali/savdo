package apierr

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// Write maps err to the shared ADR-013 Error envelope and writes it to w.
// A *Error is used as is; any other error (a panic value, a driver error,
// anything a service forgot to wrap) becomes a bare 500 INTERNAL — its
// message is logged server-side but never sent to the client. Every 5xx is
// logged with the request id (read back off w's already-set
// X-Request-Id response header — see RequestIDHeader). A 429 gets a
// Retry-After header from its retryAfterSeconds detail.
func Write(w http.ResponseWriter, err error) {
	apiErr := asError(err)

	if apiErr.Status >= http.StatusInternalServerError {
		slog.Error("api_error",
			"status", apiErr.Status,
			"code", string(apiErr.Code),
			"request_id", w.Header().Get(RequestIDHeader),
			"error", err,
		)
	}

	if apiErr.Status == http.StatusTooManyRequests {
		if seconds, ok := apiErr.Details["retryAfterSeconds"].(int); ok {
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
		}
	}

	body := gen.Error{}
	body.Error.Code = apiErr.Code
	if len(apiErr.Details) > 0 {
		details := apiErr.Details
		body.Error.Details = &details
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(apiErr.Status)
	_ = json.NewEncoder(w).Encode(body)
}

// asError unwraps err to a *Error when it (or something it wraps) is one.
// A *http.MaxBytesError anywhere in the chain — reached whenever a
// handler reads its own request body past the route's limit
// (bodylimit.go), rather than the strict server's own pre-handler decode
// step catching it first (router.go's writeRequestError, for a plain JSON
// body) — maps to the same 400 VALIDATION_FAILED / body_too_large shape
// writeRequestError uses, instead of falling through to a bare 500:
// POST /media reads its multipart body itself (internal/media), so an
// over-limit upload's *http.MaxBytesError only ever reaches Write, never
// writeRequestError. Anything else maps to a bare Internal().
func asError(err error) *Error {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr
	}

	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return &Error{
			Status:  http.StatusBadRequest,
			Code:    gen.VALIDATIONFAILED,
			Details: map[string]any{"reason": "body_too_large"},
		}
	}

	return Internal()
}
