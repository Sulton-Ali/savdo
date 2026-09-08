package public

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
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
// cacheSet call sweeps it away, see evictLocked), no separate sweep
// goroutine needed at this scale.
func (s *Service) cacheGet(key string) (cacheEntry, bool) {
	s.respMu.RLock()
	entry, ok := s.resp[key]
	s.respMu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return cacheEntry{}, false
	}
	return entry, true
}

// maxCacheEntries, maxCacheBytes and maxCacheEntryBytes bound
// Service.resp for the life of the process (T3 review round 2, CRITICAL
// 1): cacheKey is already built only from cacheParamsKey's bounded,
// typed fields, not the raw query string, so an unrelated/junk query
// parameter can never mint a new entry at all — but a caller can still
// send many distinct *legitimate-shaped* `q` values (each capped by
// maxSearchLength, but the count of them is not bounded), so cacheSet
// enforces a hard ceiling on both entry count and total cached bytes on
// top of that. maxCacheEntryBytes additionally refuses to cache any
// single response above a sane size at all — a pathological page is
// simply never cached, every request for it just runs the real handler,
// which affects performance, never correctness.
const (
	maxCacheEntries    = 2000
	maxCacheBytes      = 32 << 20 // 32 MiB
	maxCacheEntryBytes = 2 << 20  // 2 MiB
)

// cacheSet inserts entry under key, first making room per the bounds
// above. respBytes is Service's own running total of len(body) across
// every live entry (cacheEntry's other fields are small/fixed-size and
// not worth tracking) — kept in sync here and in evictLocked/Invalidate,
// the three places that ever add or remove a map entry.
func (s *Service) cacheSet(key string, entry cacheEntry) {
	if len(entry.body) > maxCacheEntryBytes {
		return
	}
	s.respMu.Lock()
	defer s.respMu.Unlock()
	if s.resp == nil {
		s.resp = make(map[string]cacheEntry)
	}
	if old, ok := s.resp[key]; ok {
		s.respBytes -= len(old.body)
		delete(s.resp, key)
	}
	s.evictLocked(len(entry.body))
	s.resp[key] = entry
	s.respBytes += len(entry.body)
}

// evictLocked makes room for a newBytes-sized insert; respMu must
// already be held for writing. It first drops every already-expired
// entry — a hit for those was impossible anyway (cacheGet treats them as
// a miss), so this only reclaims memory — then, if the entry-count or
// byte-total cap would still be exceeded, evicts entries with the
// nearest expiresAt first: since every entry shares the same cacheTTL
// measured from its own insertion, "nearest expiresAt" and "oldest
// insertion" are the same ordering, so this is a plain oldest-first
// eviction without a separate recency-tracking structure. The O(n) scan
// per eviction is fine at this scale: it only runs when the map is
// already at or near a 2 000-entry cap, not on every insert.
func (s *Service) evictLocked(newBytes int) {
	now := time.Now()
	for k, e := range s.resp {
		if now.After(e.expiresAt) {
			s.respBytes -= len(e.body)
			delete(s.resp, k)
		}
	}
	for len(s.resp) >= maxCacheEntries || s.respBytes+newBytes > maxCacheBytes {
		if len(s.resp) == 0 {
			return
		}
		var oldestKey string
		var oldestExpiry time.Time
		first := true
		for k, e := range s.resp {
			if first || e.expiresAt.Before(oldestExpiry) {
				oldestKey, oldestExpiry = k, e.expiresAt
				first = false
			}
		}
		s.respBytes -= len(s.resp[oldestKey].body)
		delete(s.resp, oldestKey)
	}
}

// cacheKey is O-20's cache key: shop slug, path, a bounded params string
// (cacheParamsKey) and the resolved locale — a different slug, path,
// params or locale is a different cache entry; the slug (not just the
// shop id) is included so a Phase 8 hostname change is visible in the
// key shape even though this task still resolves by PUBLIC_SHOP_SLUG
// alone.
func cacheKey(shopSlug, path, params, locale string) string {
	return shopSlug + "|" + path + "|" + params + "|" + locale
}

// cacheParamsKey renders the part of the cache key that can vary a
// cached operation's response body, from request — the STRICT server's
// own already-decoded, typed request object, never the raw query string
// (T3 review round 2, CRITICAL 1). Only ListPublicProducts has
// body-affecting query parameters (category, featured, q, limit,
// cursor); GetPublicShop and ListPublicCategories take none, and
// GetPublicProductBySlug's only input is its path parameter, already
// folded into the key via r.URL.Path in CacheMiddleware — so this
// returns "" for every request type but ListPublicProductsRequestObject.
// Because the key comes from the typed request, an unrelated/junk query
// parameter (`?zzz=1`, cache-busting noise) never reaches it at all —
// oapi-codegen already dropped it before this object was built — and `q`
// and `limit` are bounded exactly the way the handler itself bounds them
// before running the query (searchParam's maxSearchLength cap,
// clampLimit's [1, maxLimit]).
func cacheParamsKey(request any) string {
	req, ok := request.(gen.ListPublicProductsRequestObject)
	if !ok {
		return ""
	}
	var category, featured, q, cursor string
	if req.Params.Category != nil {
		category = *req.Params.Category
	}
	if req.Params.Featured != nil {
		featured = strconv.FormatBool(*req.Params.Featured)
	}
	if s := searchParam(req.Params.Q); s != nil {
		q = *s
	}
	if req.Params.Cursor != nil {
		cursor = *req.Params.Cursor
	}
	limit := strconv.Itoa(int(clampLimit(req.Params.Limit)))
	return category + "\x00" + featured + "\x00" + q + "\x00" + limit + "\x00" + cursor
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

// ifNoneMatch reports whether raw (the request's If-None-Match header)
// matches etag under RFC 9110 § 13.1.2's rules for a conditional GET: a
// comma-separated list of entity tags (optionally space-padded around
// the commas), each optionally weak (`W/"..."`) — GET only ever needs
// the weak comparison function, so a weak and a strong tag with the same
// quoted value compare equal here, the `W/` prefix is simply stripped
// before comparing — or the single token `*`, which always matches
// (there is a current representation: this entry exists). An empty raw
// never matches (no header at all is not a conditional request).
func ifNoneMatch(raw, etag string) bool {
	if raw == "" {
		return false
	}
	for _, tag := range strings.Split(raw, ",") {
		tag = strings.TrimSpace(tag)
		if tag == "*" {
			return true
		}
		if strings.TrimPrefix(tag, "W/") == etag {
			return true
		}
	}
	return false
}

// writeCached sends entry to w: the caching headers always, then either a
// bodyless 304 (r's If-None-Match matches entry's ETag, ifNoneMatch) or
// entry's full status and body — the shared tail both a cache hit and a
// freshly-rendered response (CacheMiddleware) go through, so the two
// paths can never disagree about what a matching If-None-Match does.
func writeCached(w http.ResponseWriter, r *http.Request, entry cacheEntry) {
	w.Header().Set("Content-Type", entry.contentType)
	w.Header().Set("Cache-Control", "public, max-age=60")
	w.Header().Set("ETag", entry.etag)
	// Vary: Accept-Language — the cache key already partitions by resolved
	// locale (cacheKey), but a shared HTTP cache in front of this API only
	// knows that from a Vary header: without it, a downstream/browser
	// cache could serve one locale's cached bytes to a request that asked
	// for another. Set on both the 200 and the 304 path, the same "every
	// response this handler writes carries the same caching headers"
	// posture Cache-Control/ETag already take here.
	w.Header().Set("Vary", "Accept-Language")
	if ifNoneMatch(r.Header.Get("If-None-Match"), entry.etag) {
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
// A garbage or absent header (no comma-separated part matches a
// supported locale) simply falls through to defaultLocale, never an
// error — the same tolerance catalog.ResolveLocale itself has.
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
// (shop, path, params, locale) key runs the inner handler once, caches
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
		key := cacheKey(shop.Slug, r.URL.Path, cacheParamsKey(request), locale)

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
			// Not a cacheable 200 (e.g. 404 for an unknown slug, or a 400
			// for a malformed cursor) — let the normal strictHandler path
			// Visit it against the real w. Never reaches cacheSet, so a
			// caller sending many distinct invalid requests (a tampered
			// cursor, garbage params) cannot grow the cache at all.
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
