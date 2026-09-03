package httpx

import "net/http"

// maxRequestBodyBytes bounds every request body this API ever reads (1
// MiB). Nothing in Phase 1's contract needs more than a few KiB per
// request — media upload (Phase 2) will need its own, larger, multipart
// limit at that route specifically, but the default for everything else
// should be small: an unbounded body is a denial-of-service vector (a
// client can otherwise send gigabytes into a JSON decoder, or use one
// oversized field to make an in-memory rate-limiter key or an argon2
// input arbitrarily large).
const maxRequestBodyBytes = 1 << 20 // 1 MiB

// maxBytesBody wraps every request's body in http.MaxBytesReader before it
// reaches any handler. A read past the limit fails with *http.MaxBytesError,
// which the generated strict server's JSON-decode step surfaces to
// writeRequestError (router.go) as a wrapped error — mapped there to 400
// VALIDATION_FAILED with details.reason "body_too_large", not the 500 an
// unhandled read error would otherwise risk becoming.
func maxBytesBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		next.ServeHTTP(w, r)
	})
}
