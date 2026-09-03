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

// asError unwraps err to a *Error when it (or something it wraps) is one;
// anything else maps to a bare Internal().
func asError(err error) *Error {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return Internal()
}
