// Package apierr is the one place API errors turn into the ADR-013 error
// envelope. Every handler, service and middleware that needs to fail a
// request builds one of these constructors (or, for a genuinely unexpected
// failure, lets the underlying error flow into Write unchanged) instead of
// writing an HTTP status or a JSON body by hand.
package apierr

import (
	"net/http"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// RequestIDHeader is the response header the request-id middleware
// (internal/httpx) sets at the start of every request. Write reads it back
// off the ResponseWriter to correlate a logged 5xx with that request,
// without needing the request or its context threaded through every error
// path.
const RequestIDHeader = "X-Request-Id"

// Error is a typed API error carrying everything Write needs to render the
// ADR-013 envelope: the HTTP status, the machine-readable code and any
// machine-readable details. Construct one with the functions below rather
// than a literal, except from within this package's own tests.
type Error struct {
	Status  int
	Code    gen.ErrorCode
	Details map[string]any
}

// Error implements the error interface. It is never shown to a client —
// Write only ever sends Code and Details — so it is fine for it to be
// terse; it exists for logs and %w wrapping.
func (e *Error) Error() string {
	return "apierr: " + string(e.Code)
}

// Validation builds a 400 VALIDATION_FAILED error with a reason per invalid
// field, per docs/05-API.md § Conventions: `details.fields`.
func Validation(fields map[string]string) *Error {
	return &Error{
		Status:  http.StatusBadRequest,
		Code:    gen.VALIDATIONFAILED,
		Details: map[string]any{"fields": fields},
	}
}

// Unauthenticated builds a 401 UNAUTHENTICATED error.
func Unauthenticated() *Error {
	return &Error{Status: http.StatusUnauthorized, Code: gen.UNAUTHENTICATED}
}

// Forbidden builds a 403 FORBIDDEN error.
func Forbidden() *Error {
	return &Error{Status: http.StatusForbidden, Code: gen.FORBIDDEN}
}

// NotFound builds a 404 NOT_FOUND error naming the missing entity.
func NotFound(entity string) *Error {
	return &Error{
		Status:  http.StatusNotFound,
		Code:    gen.NOTFOUND,
		Details: map[string]any{"entity": entity},
	}
}

// Conflict builds a 409 CONFLICT error naming the field in conflict (e.g. a
// duplicate SKU, an already-voided sale).
func Conflict(field string) *Error {
	return &Error{
		Status:  http.StatusConflict,
		Code:    gen.CONFLICT,
		Details: map[string]any{"field": field},
	}
}

// RateLimited builds a 429 RATE_LIMITED error. Write sets the Retry-After
// header from retryAfterSeconds.
func RateLimited(retryAfterSeconds int) *Error {
	return &Error{
		Status:  http.StatusTooManyRequests,
		Code:    gen.RATELIMITED,
		Details: map[string]any{"retryAfterSeconds": retryAfterSeconds},
	}
}

// Internal builds a 500 INTERNAL error. It deliberately carries no details:
// the underlying failure belongs in a server-side log (Write does this for
// any 5xx), never in the response body.
func Internal() *Error {
	return &Error{Status: http.StatusInternalServerError, Code: gen.INTERNAL}
}
