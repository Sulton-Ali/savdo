package httpx

import "net/http"

// maxRequestBodyBytes bounds every request body this API reads, except the
// one route mediaRequestBodyBytes overrides below (1 MiB). Nothing else in
// the contract needs more than a few KiB per request, and an unbounded
// body is a denial-of-service vector (a client can otherwise send
// gigabytes into a JSON decoder, or use one oversized field to make an
// in-memory rate-limiter key or an argon2 input arbitrarily large).
const maxRequestBodyBytes = 1 << 20 // 1 MiB

// mediaUploadPath and mediaRequestBodyBytes are POST /media's own, larger
// limit (docs/06-ROADMAP.md Phase 2 T3 spec: "12 MiB for that route, 1 MiB
// elsewhere"). 12 MiB, not Config.MediaMaxBytes (10 MiB default): the
// multipart envelope around the file part (boundaries, the other form
// fields, header overhead) needs headroom over the file-content cap
// media.Service enforces on top of this — the two limits are deliberately
// different and enforced at different layers.
const (
	mediaUploadPath       = "/v1/media"
	mediaRequestBodyBytes = 12 << 20 // 12 MiB
)

// maxBytesBody wraps every request's body in http.MaxBytesReader before it
// reaches any handler, at the limit for its route. A read past the limit
// fails with *http.MaxBytesError, which the generated strict server's
// decode step surfaces to writeRequestError (router.go) as a wrapped
// error — mapped there to 400 VALIDATION_FAILED with details.reason
// "body_too_large", not the 500 an unhandled read error would otherwise
// risk becoming.
func maxBytesBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := int64(maxRequestBodyBytes)
		if r.Method == http.MethodPost && r.URL.Path == mediaUploadPath {
			limit = mediaRequestBodyBytes
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}
