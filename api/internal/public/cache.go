package public

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// cacheEntry is one cached response body: the exact bytes
// gen.Visit*Response wrote (recorder.body), its status and Content-Type,
// a strong ETag over those bytes, and the shop it belongs to (so
// Invalidate can clear only that shop's entries).
type cacheEntry struct {
	shopID      uuid.UUID
	status      int
	contentType string
	body        []byte
	etag        string
	expiresAt   time.Time
}

// cacheGet returns key's entry when present and not yet expired — a
// lazily-evicted read: an expired entry is reported as a miss (the next
// successful write via cacheSet overwrites it), no separate sweep
// goroutine needed at this scale (a handful of distinct public
// path/query/locale combinations).
func (s *Service) cacheGet(key string) (cacheEntry, bool) {
	s.respMu.RLock()
	entry, ok := s.resp[key]
	s.respMu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return cacheEntry{}, false
	}
	return entry, true
}

func (s *Service) cacheSet(key string, entry cacheEntry) {
	s.respMu.Lock()
	if s.resp == nil {
		s.resp = make(map[string]cacheEntry)
	}
	s.resp[key] = entry
	s.respMu.Unlock()
}

// cacheKey is O-20's cache key: shop slug, path, sorted query (Values.
// Encode already sorts by key) and the resolved locale — a different
// slug, path, query or locale is a different cache entry; the slug (not
// just the shop id) is included so a Phase 8 hostname change is visible
// in the key shape even though this task still resolves by
// PUBLIC_SHOP_SLUG alone.
func cacheKey(shopSlug, path, rawQuery, locale string) string {
	return shopSlug + "|" + path + "|" + rawQuery + "|" + locale
}

// strongETag is a strong ETag (RFC 9110 § 8.8.1: quoted, byte-exact) over
// body — sha256 hex, per this task's spec.
func strongETag(body []byte) string {
	sum := sha256.Sum256(body)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

// recorder is a minimal http.ResponseWriter that captures exactly what a
// gen.Visit*Response method writes, so CacheMiddleware can hash and cache
// those same bytes instead of re-deriving them (a second, independent
// json.Marshal could legally serialize differently — different key
// order, no trailing newline — and then the ETag would not describe the
// bytes actually sent). Deliberately not net/http/httptest.ResponseRecorder:
// that package is meant for tests, not a production request path.
type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func newRecorder() *recorder {
	return &recorder{header: make(http.Header), status: http.StatusOK}
}

func (r *recorder) Header() http.Header { return r.header }

func (r *recorder) Write(b []byte) (int, error) { return r.body.Write(b) }

func (r *recorder) WriteHeader(status int) { r.status = status }

// writeCached sends entry to w: the caching headers always, then either a
// bodyless 304 (r's If-None-Match matches entry's ETag) or entry's full
// status and body — the shared tail both a cache hit and a freshly-
// rendered response (CacheMiddleware) go through, so the two paths can
// never disagree about what a matching If-None-Match does.
func writeCached(w http.ResponseWriter, r *http.Request, entry cacheEntry) {
	w.Header().Set("Content-Type", entry.contentType)
	w.Header().Set("Cache-Control", "public, max-age=60")
	w.Header().Set("ETag", entry.etag)
	if inm := r.Header.Get("If-None-Match"); inm != "" && inm == entry.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(entry.status)
	_, _ = w.Write(entry.body)
}

// cachedOperations are the four operationIDs (the oapi-codegen strict
// server's Go method names — see auth.allowlistedOperations' own doc
// comment for why that casing, not the OpenAPI operationId, is what
// reaches this parameter) CacheMiddleware caches. Every other operation
// passes straight through untouched.
var cachedOperations = map[string]bool{
	"GetPublicShop":          true,
	"ListPublicCategories":   true,
	"ListPublicProducts":     true,
	"GetPublicProductBySlug": true,
}

// supportedLocales mirrors catalog.ResolveLocale's own vocabulary
// (ADR-012). Duplicated here (see resolveLocaleHeader) rather than
// imported, since this function operates on the raw header string, not a
// context catalog.AcceptLanguageMiddleware has necessarily stashed yet —
// CacheMiddleware must compute the same cache key regardless of where in
// the middleware chain it happens to sit relative to that one.
var supportedLocales = map[string]bool{"uz": true, "ru": true, "en": true}

// resolveLocaleHeader is catalog.ResolveLocale's parsing algorithm
// applied directly to a raw `Accept-Language` header value instead of a
// context — the same first-supported-tag, ignore-`;q=` logic, duplicated
// rather than exported+reused because CacheMiddleware needs it before
// catalog.AcceptLanguageMiddleware is guaranteed to have run (Go's
// StrictMiddlewareFunc composition runs the LAST entry in the slice
// first — see internal/httpx/router.go's NewRouter doc comment — and
// this package must not depend on its own position in that slice to
// compute a correct cache key). The actual handler methods (handler.go)
// still resolve locale the normal way, via catalog.ResolveLocale off ctx,
// since by the time a handler runs, AcceptLanguageMiddleware always has.
func resolveLocaleHeader(header, defaultLocale string) string {
	for _, part := range strings.Split(header, ",") {
		tag, _, _ := strings.Cut(strings.TrimSpace(part), ";")
		tag, _, _ = strings.Cut(tag, "-")
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		if supportedLocales[tag] {
			return tag
		}
	}
	return defaultLocale
}

// renderResponse Visits response (one of the four operations' 200
// JSONResponse types) into a fresh recorder and returns its bytes,
// status and Content-Type. ok is false for anything CacheMiddleware
// never caches: an error response (400/404/...), a nil response, or a
// Visit that itself errored (rendering falls back to the normal,
// uncached path in that case).
func renderResponse(response any) (body []byte, status int, contentType string, ok bool) {
	rec := newRecorder()
	var visitErr error
	switch v := response.(type) {
	case gen.GetPublicShop200JSONResponse:
		visitErr = v.VisitGetPublicShopResponse(rec)
	case gen.ListPublicCategories200JSONResponse:
		visitErr = v.VisitListPublicCategoriesResponse(rec)
	case gen.ListPublicProducts200JSONResponse:
		visitErr = v.VisitListPublicProductsResponse(rec)
	case gen.GetPublicProductBySlug200JSONResponse:
		visitErr = v.VisitGetPublicProductBySlugResponse(rec)
	default:
		return nil, 0, "", false
	}
	if visitErr != nil || rec.status != http.StatusOK {
		return nil, 0, "", false
	}
	return rec.body.Bytes(), rec.status, rec.header.Get("Content-Type"), true
}

// CacheMiddleware is a gen.StrictMiddlewareFunc (wired in
// internal/httpx.NewRouter alongside auth.Service.Middleware and
// catalog.AcceptLanguageMiddleware) that serves the four public
// operations out of Service's in-process cache (O-20): a fresh
// (shop, path, query, locale) key runs the inner handler once, caches
// its rendered bytes with a strong ETag for cacheTTL, and answers every
// request in that window — including one with a matching If-None-Match —
// from the cache, without calling the inner handler (and so without a
// database read) again. Every other operationID passes through
// unchanged.
func (s *Service) CacheMiddleware(f gen.StrictHandlerFunc, operationID string) gen.StrictHandlerFunc {
	if !cachedOperations[operationID] {
		return f
	}
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
		shop, err := s.resolveShop(ctx)
		if err != nil {
			return nil, err
		}
		locale := resolveLocaleHeader(r.Header.Get("Accept-Language"), shop.DefaultLocale)
		key := cacheKey(shop.Slug, r.URL.Path, r.URL.Query().Encode(), locale)

		if entry, ok := s.cacheGet(key); ok {
			writeCached(w, r, entry)
			return nil, nil
		}

		response, err := f(ctx, w, r, request)
		if err != nil {
			return response, err
		}

		body, status, contentType, ok := renderResponse(response)
		if !ok {
			// Not a cacheable 200 (e.g. 404 for an unknown slug) — let
			// the normal strictHandler path Visit it against the real w.
			return response, err
		}

		entry := cacheEntry{
			shopID: shop.ID, status: status, contentType: contentType, body: body,
			etag: strongETag(body), expiresAt: time.Now().Add(cacheTTL),
		}
		s.cacheSet(key, entry)
		writeCached(w, r, entry)
		return nil, nil
	}
}
