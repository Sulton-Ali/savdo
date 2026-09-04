package catalog

import (
	"context"
	"net/http"
	"strings"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// acceptLanguageCtxKey is the context key AcceptLanguageMiddleware stashes
// the raw `Accept-Language` header value under.
type acceptLanguageCtxKey struct{}

// AcceptLanguageMiddleware is a gen.StrictMiddlewareFunc that stashes the
// raw `Accept-Language` request header on the context. A
// gen.StrictServerInterface method never sees the raw *http.Request (only
// (ctx, request) — see gen.StrictHandlerFunc), and `Accept-Language` is
// not modelled as a per-operation parameter in contracts/openapi.yaml (it
// is a `components.parameters` entry no path references), so this is the
// only way resolveLocale can reach it — the same requestInfo-stashing
// pattern auth.Service.Middleware uses for User-Agent/IP. Wired into
// internal/httpx.NewRouter alongside auth.Service.Middleware.
func AcceptLanguageMiddleware(f gen.StrictHandlerFunc, _ string) gen.StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
		ctx = context.WithValue(ctx, acceptLanguageCtxKey{}, r.Header.Get("Accept-Language"))
		return f(ctx, w, r, request)
	}
}

// AcceptLanguageFromContext returns the raw `Accept-Language` header value
// AcceptLanguageMiddleware stashed on ctx ("" if the middleware never ran
// or the client sent none). Exported (T4) so another module whose own
// response resolves a translated field by the caller's locale — e.g.
// stock.PurchaseItem.productName — can read the same header this package
// already stashes, without a second, duplicate middleware: prefer
// ResolveLocale below, which also applies the uz/ru/en fallback; this is
// the lower-level piece ResolveLocale itself is built from.
func AcceptLanguageFromContext(ctx context.Context) string {
	v, _ := ctx.Value(acceptLanguageCtxKey{}).(string)
	return v
}

// supportedLocales is the uz/ru/en vocabulary docs/05-API.md §
// Conventions and ADR-012 define. Order does not matter here — request
// order is what resolveLocale/ResolveLocale honours.
var supportedLocales = map[string]bool{"uz": true, "ru": true, "en": true}

// ResolveLocale parses ctx's `Accept-Language` header (stashed by
// AcceptLanguageMiddleware) for the first supported locale (uz/ru/en),
// trying each comma-separated entry in the order the client listed them
// and ignoring any `;q=` weight — a full RFC 4647 weighted match is more
// than three fixed locales need. Falls back to defaultLocale when the
// header is absent or names nothing supported (docs/05-API.md §
// Conventions). Exported (T4) alongside AcceptLanguageFromContext for the
// same reason — (*Service).resolveLocale below is now a thin wrapper
// passing the catalog shop's own defaultLocale, kept for its existing
// unexported call sites in this package; a caller from another module
// passes its own shop row's default_locale explicitly, the same value
// stock.defaultLocaleFor already reads.
func ResolveLocale(ctx context.Context, defaultLocale string) string {
	header := AcceptLanguageFromContext(ctx)
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

// resolveLocale is ResolveLocale bound to this Service's own defaultLocale
// — every existing call site in this package keeps using the short,
// receiver form.
func (s *Service) resolveLocale(ctx context.Context) string {
	return ResolveLocale(ctx, s.defaultLocale)
}

// translationFallback reports whether the resolved row's locale differs
// from the one the caller asked for — true both when a different
// supported locale's translation was used (requested -> 'uz' -> any
// fallback, in the SQL) and when localeUsed is "" (no translation exists
// at all yet), since "" never equals a real requested locale
// (docs/05-API.md § Conventions).
func translationFallback(localeUsed, requested string) bool {
	return localeUsed != requested
}

// effectiveLocale is what the `locale` response field reports: the
// resolved localeUsed when a translation exists, or the requested locale
// itself when none does yet (localeUsed == "") — never an empty string,
// which is not a member of gen.Locale's uz/ru/en vocabulary.
func effectiveLocale(localeUsed, requested string) gen.Locale {
	if localeUsed == "" {
		return gen.Locale(requested)
	}
	return gen.Locale(localeUsed)
}
