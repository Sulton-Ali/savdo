package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/config"
)

func TestCSRFDecisionTable(t *testing.T) {
	tests := []struct {
		name     string
		source   tokenSource
		method   string
		required bool
	}{
		{"cookie + POST requires CSRF", sourceCookie, http.MethodPost, true},
		{"cookie + PATCH requires CSRF", sourceCookie, http.MethodPatch, true},
		{"cookie + DELETE requires CSRF", sourceCookie, http.MethodDelete, true},
		{"cookie + GET is safe", sourceCookie, http.MethodGet, false},
		{"cookie + HEAD is safe", sourceCookie, http.MethodHead, false},
		{"cookie + OPTIONS is safe", sourceCookie, http.MethodOptions, false},
		{"bearer + POST never requires CSRF", sourceBearer, http.MethodPost, false},
		{"bearer + DELETE never requires CSRF", sourceBearer, http.MethodDelete, false},
		{"bearer + GET never requires CSRF", sourceBearer, http.MethodGet, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := csrfRequired(tt.source, tt.method); got != tt.required {
				t.Fatalf("csrfRequired(%v, %s) = %v, want %v", tt.source, tt.method, got, tt.required)
			}
		})
	}
}

func TestExtractTokenPrefersBearerOverCookie(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	r.Header.Set("Authorization", "Bearer the-bearer-token")
	r.AddCookie(&http.Cookie{Name: CookieName, Value: "the-cookie-token", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})

	token, source, ok := extractToken(r)
	if !ok {
		t.Fatal("extractToken() ok = false, want true")
	}
	if token != "the-bearer-token" {
		t.Fatalf("token = %q, want the-bearer-token", token)
	}
	if source != sourceBearer {
		t.Fatalf("source = %v, want sourceBearer", source)
	}
}

func TestExtractTokenFallsBackToCookie(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: "the-cookie-token", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})

	token, source, ok := extractToken(r)
	if !ok {
		t.Fatal("extractToken() ok = false, want true")
	}
	if token != "the-cookie-token" {
		t.Fatalf("token = %q, want the-cookie-token", token)
	}
	if source != sourceCookie {
		t.Fatalf("source = %v, want sourceCookie", source)
	}
}

func TestExtractTokenNoneFound(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)

	if _, _, ok := extractToken(r); ok {
		t.Fatal("extractToken() ok = true, want false when no credential is present")
	}
}

func TestExtractTokenIgnoresNonBearerAuthorization(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	r.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	r.AddCookie(&http.Cookie{Name: CookieName, Value: "the-cookie-token", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})

	token, source, ok := extractToken(r)
	if !ok || source != sourceCookie || token != "the-cookie-token" {
		t.Fatalf("extractToken() = (%q, %v, %v), want fallback to the cookie", token, source, ok)
	}
}

func TestClientIPUsesRemoteAddrInDev(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.5:54321"
	r.Header.Set("X-Forwarded-For", "198.51.100.9")

	ip := clientIP(r, false)
	if ip == nil || ip.String() != "203.0.113.5" {
		t.Fatalf("clientIP(dev) = %v, want 203.0.113.5 (X-Forwarded-For ignored outside prod)", ip)
	}
}

func TestClientIPUsesForwardedForLastHopInProd(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.5:54321"
	r.Header.Set("X-Forwarded-For", "198.51.100.9, 10.0.0.1")

	ip := clientIP(r, true)
	if ip == nil || ip.String() != "10.0.0.1" {
		t.Fatalf("clientIP(prod) = %v, want 10.0.0.1 (last hop — the trusted proxy's own observation)", ip)
	}
}

// TestClientIPIgnoresASpoofedFirstEntry proves a client cannot bypass the
// per-IP rate limit or forge sessions.ip by prepending an arbitrary
// address to its own X-Forwarded-For request header: only the last entry
// — appended by the trusted proxy (Caddy), never copied verbatim from a
// client-supplied value — is used.
func TestClientIPIgnoresASpoofedFirstEntry(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.5:54321"
	// The attacker sends its own forged entry, then Caddy appends the
	// address it actually saw on the connection.
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 198.51.100.42")

	ip := clientIP(r, true)
	if ip == nil || ip.String() != "198.51.100.42" {
		t.Fatalf("clientIP() = %v, want 198.51.100.42 (the proxy-appended hop); a spoofed first entry must be ignored", ip)
	}
}

func TestClientIPFallsBackWhenUnparseable(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "not-an-address"

	if ip := clientIP(r, false); ip != nil {
		t.Fatalf("clientIP() = %v, want nil for an unparseable address", ip)
	}
}

func TestMiddlewareAllowlistsHealthzReadyzAndLoginWithoutASession(t *testing.T) {
	svc := NewService(nil, nil, config.Config{}, [16]byte{})

	for _, op := range []string{"GetHealthz", "GetReadyz", "Login"} {
		t.Run(op, func(t *testing.T) {
			called := false
			next := func(_ context.Context, _ http.ResponseWriter, _ *http.Request, _ any) (any, error) {
				called = true
				return "ok", nil
			}

			r := httptest.NewRequest(http.MethodPost, "/v1/whatever", nil)
			rec := httptest.NewRecorder()

			resp, err := svc.Middleware(next, op)(context.Background(), rec, r, nil)
			if err != nil {
				t.Fatalf("Middleware() error = %v, want nil for an allow-listed operation with no session", err)
			}
			if !called {
				t.Fatal("the inner handler was never called for an allow-listed operation")
			}
			if resp != "ok" {
				t.Fatalf("resp = %v, want ok", resp)
			}
		})
	}
}

func TestMiddlewareRejectsAnythingElseWithNoCredential(t *testing.T) {
	svc := NewService(nil, nil, config.Config{}, [16]byte{})

	next := func(_ context.Context, _ http.ResponseWriter, _ *http.Request, _ any) (any, error) {
		t.Fatal("the inner handler must not run when there is no credential")
		return nil, nil
	}

	r := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	rec := httptest.NewRecorder()

	_, err := svc.Middleware(next, "GetMe")(context.Background(), rec, r, nil)
	if err == nil {
		t.Fatal("Middleware() error = nil, want Unauthenticated")
	}
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("Middleware() error = %v, want a 401 *apierr.Error", err)
	}
}
