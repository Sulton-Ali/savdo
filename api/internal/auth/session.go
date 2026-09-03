package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/Sulton-Ali/savdo/api/internal/config"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// CookieName is the session cookie's name (docs/05-API.md § Conventions).
const CookieName = "savdo_session"

// csrfHeader and csrfHeaderValue are the CSRF check docs/05-API.md
// requires on every cookie-authenticated mutating request.
const (
	csrfHeader      = "X-Requested-With"
	csrfHeaderValue = "savdo"
)

// tokenBytes is the raw entropy of a session token before encoding: 32
// random bytes (docs/03-ARCHITECTURE.md § Auth flow).
const tokenBytes = 32

// touchThreshold is how stale last_seen_at must be before Middleware
// bothers to extend a session (D-29's sliding expiry), so an active user
// making many requests per minute doesn't turn every one of them into a
// write.
const touchThreshold = 60 * time.Second

// userAgentMaxLen truncates a stored User-Agent (docs/03-ARCHITECTURE.md §
// Auth spec).
const userAgentMaxLen = 512

// newToken generates a fresh session token: 32 random bytes (crypto/rand),
// returned both as the raw base64url string the client is given and as the
// SHA-256 hash that is all `sessions.token_hash` ever stores (ADR-005 — no
// plaintext token ever reaches the database).
func newToken() (raw string, hash []byte, err error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, err
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	return raw, hashToken(raw), nil
}

// hashToken is the one place a raw token becomes the value looked up in
// (and compared against) `sessions.token_hash`.
func hashToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

// ttlFor returns the sliding-session lifetime for client (D-29: 7 days
// web, 30 days mobile), from config rather than a hardcoded literal so
// tests and non-default deployments can tune it.
func ttlFor(client db.SessionClient, cfg config.Config) time.Duration {
	if client == db.SessionClientMobile {
		return cfg.SessionMobileTTL
	}
	return cfg.SessionWebTTL
}

// needsTouch reports whether a session last seen at lastSeenAt should have
// its expiry extended now (D-29 sliding expiry), throttled to once per
// touchThreshold so a busy session doesn't write on every request.
func needsTouch(lastSeenAt, now time.Time) bool {
	return now.Sub(lastSeenAt) > touchThreshold
}

// truncateUserAgent bounds a stored User-Agent header to userAgentMaxLen
// bytes. Go strings index by byte, not rune, so this can split a multi-byte
// UTF-8 sequence; that is an acceptable cosmetic wrinkle for a value that
// is never re-parsed, only displayed.
func truncateUserAgent(ua string) string {
	if len(ua) <= userAgentMaxLen {
		return ua
	}
	return ua[:userAgentMaxLen]
}

// sessionCookie builds the Set-Cookie for a successful `client: web` login:
// HttpOnly, SameSite=Lax always; Secure when cfg says so (docs/05-API.md §
// Conventions; ADR-005). Go's net/http encodes a positive MaxAge as
// `Max-Age=<seconds>` — see https://pkg.go.dev/net/http#Cookie.
func sessionCookie(token string, ttl time.Duration, secure bool) *http.Cookie {
	// gosec's G124 only recognizes a literal `true` for Secure, so it
	// flags this literal as "missing" it even though Secure is always
	// set below — from cfg.CookieSecure, which defaults true in prod and
	// false in dev (config.Load), never a hardcoded constant, since a
	// hardcoded true would make the cookie unusable over the API's own
	// plain HTTP dev server. See
	// https://github.com/securego/gosec/blob/master/RULES.md#g124.
	return &http.Cookie{ //nolint:gosec // G124 false positive: Secure is set, just not to a literal true.
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(ttl.Seconds()),
	}
}

// clearSessionCookie builds the Set-Cookie logout uses to remove the
// session cookie client-side. A negative MaxAge tells net/http to emit
// `Max-Age=0`, which every browser treats as "delete this cookie now" —
// see https://pkg.go.dev/net/http#Cookie.
func clearSessionCookie(secure bool) *http.Cookie {
	return &http.Cookie{ //nolint:gosec // G124 false positive, same as sessionCookie above.
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}
}
