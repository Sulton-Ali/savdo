package bot

import (
	"context"
	"net/http"
	"strings"

	"github.com/Sulton-Ali/savdo/api/internal/catalog"
)

// supportedLocales mirrors catalog's own uz/ru/en vocabulary (ADR-012).
// Not exported there, so duplicated here — a two-entry map, the same
// tradeoff internal/public's own tests accept (public/setup_test.go's
// ctxWithAcceptLanguage doc comment).
var supportedLocales = map[string]bool{"uz": true, "ru": true, "en": true}

// resolveCommandLocale is D-113's fallback chain for slash commands (and,
// this package's own choice, for the language hint free text gets before
// the model has a chance to detect the message's own language itself):
// the Telegram user's own language_code, then the shop's default_locale.
// languageCode may carry a region suffix ("en-US") or be empty (some
// Telegram clients send none); only the primary subtag is checked.
//
// Known mismatch (item 15, no behavior change): this locale also
// resolves which language executeTool's tool results come back in
// (withLocale below), but the *model itself* answers in whatever
// language it detects the customer's own message to be written in
// (chat.go's systemPrompt: "Detect the customer's own language... and
// answer in that language instead"), which can differ from the
// Telegram UI language_code this function reads. A customer whose
// Telegram client is set to Russian but who writes in English gets tool
// data resolved in Russian (category/content names, if translated)
// while the model's own prose answers in English. Not fixed here: there
// is no trivial fix — resolving tool data in the *model's* detected
// language would mean detecting it twice (once here, before the first
// Chat call that could tell us, and once by the model itself), or
// changing the tool loop's shape so a tool call can carry its own
// locale, either of which is more than this task's scope.
func resolveCommandLocale(languageCode, shopDefaultLocale string) string {
	tag, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(languageCode)), "-")
	if supportedLocales[tag] {
		return tag
	}
	if supportedLocales[shopDefaultLocale] {
		return shopDefaultLocale
	}
	return "uz"
}

// withLocale calls fn with ctx carrying the `Accept-Language` value
// catalog.ResolveLocale expects — internal/public.Handler's operations
// resolve their locale that way (catalog.AcceptLanguageMiddleware, wired
// ahead of them in internal/httpx.NewRouter for a real HTTP request), and
// this package has no HTTP request of its own to carry one: locale here
// is D-113's own per-message decision (resolveCommandLocale), not a
// client header. Mirrors internal/public's own test helper doing the
// same thing for the same reason (public/setup_test.go's
// ctxWithAcceptLanguage) — the only exported way to reach
// catalog.ResolveLocale's stashed value without standing up a full HTTP
// request.
func withLocale[T any](ctx context.Context, locale string, fn func(context.Context) (T, error)) (T, error) {
	var out T
	var callErr error
	wrapped := catalog.AcceptLanguageMiddleware(func(ctx context.Context, _ http.ResponseWriter, _ *http.Request, _ any) (any, error) {
		out, callErr = fn(ctx)
		return out, callErr
	}, "bot")
	req := &http.Request{Header: http.Header{"Accept-Language": {locale}}}
	_, _ = wrapped(ctx, nil, req, nil)
	return out, callErr
}
