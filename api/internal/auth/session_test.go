package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Sulton-Ali/savdo/api/internal/config"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

func TestNewTokenIsUniqueAndHashesConsistently(t *testing.T) {
	raw1, hash1, err := newToken()
	if err != nil {
		t.Fatalf("newToken() error = %v", err)
	}
	raw2, hash2, err := newToken()
	if err != nil {
		t.Fatalf("newToken() error = %v", err)
	}

	if raw1 == raw2 {
		t.Fatal("two newToken() calls produced the same raw token")
	}
	if string(hash1) == string(hash2) {
		t.Fatal("two newToken() calls produced the same hash")
	}

	// hashToken must be deterministic: the same raw token always hashes to
	// the same value, since that is how GetSessionByTokenHash finds it
	// again on a later request.
	if string(hashToken(raw1)) != string(hash1) {
		t.Fatal("hashToken(raw) does not reproduce the hash newToken returned for the same raw value")
	}
}

func TestTTLForPicksByClient(t *testing.T) {
	cfg := config.Config{SessionWebTTL: 7 * 24 * time.Hour, SessionMobileTTL: 30 * 24 * time.Hour}

	if got := ttlFor(db.SessionClientWeb, cfg); got != cfg.SessionWebTTL {
		t.Fatalf("ttlFor(web) = %v, want %v", got, cfg.SessionWebTTL)
	}
	if got := ttlFor(db.SessionClientMobile, cfg); got != cfg.SessionMobileTTL {
		t.Fatalf("ttlFor(mobile) = %v, want %v", got, cfg.SessionMobileTTL)
	}
}

func TestNeedsTouch(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name       string
		lastSeenAt time.Time
		want       bool
	}{
		{"just seen", now.Add(-1 * time.Second), false},
		{"exactly at the threshold", now.Add(-touchThreshold), false},
		{"stale", now.Add(-90 * time.Second), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := needsTouch(tt.lastSeenAt, now); got != tt.want {
				t.Fatalf("needsTouch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTruncateUserAgent(t *testing.T) {
	short := "Mozilla/5.0"
	if got := truncateUserAgent(short); got != short {
		t.Fatalf("truncateUserAgent(short) = %q, want unchanged", got)
	}

	long := strings.Repeat("a", userAgentMaxLen+100)
	got := truncateUserAgent(long)
	if len(got) != userAgentMaxLen {
		t.Fatalf("truncateUserAgent(long) length = %d, want %d", len(got), userAgentMaxLen)
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	c := sessionCookie("the-token", 7*24*time.Hour, true)

	if c.Name != CookieName {
		t.Fatalf("Name = %q, want %q", c.Name, CookieName)
	}
	if c.Value != "the-token" {
		t.Fatalf("Value = %q, want the-token", c.Value)
	}
	if c.Path != "/" {
		t.Fatalf("Path = %q, want /", c.Path)
	}
	if !c.HttpOnly {
		t.Fatal("HttpOnly = false, want true")
	}
	if !c.Secure {
		t.Fatal("Secure = false, want true (cfg.CookieSecure was true)")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("SameSite = %v, want Lax", c.SameSite)
	}
	if c.MaxAge != int((7 * 24 * time.Hour).Seconds()) {
		t.Fatalf("MaxAge = %d, want %d", c.MaxAge, int((7 * 24 * time.Hour).Seconds()))
	}

	// Render through net/http to confirm the actual Set-Cookie header
	// text, not just the struct fields.
	rec := httptest.NewRecorder()
	http.SetCookie(rec, c)
	header := rec.Header().Get("Set-Cookie")
	for _, want := range []string{"savdo_session=the-token", "Path=/", "HttpOnly", "Secure", "SameSite=Lax", "Max-Age=604800"} {
		if !strings.Contains(header, want) {
			t.Fatalf("Set-Cookie header = %q, want it to contain %q", header, want)
		}
	}
}

func TestSessionCookieNotSecureWhenConfigSaysSo(t *testing.T) {
	c := sessionCookie("tok", time.Hour, false)
	if c.Secure {
		t.Fatal("Secure = true, want false when cfg.CookieSecure is false")
	}
}

func TestClearSessionCookieProducesMaxAgeZero(t *testing.T) {
	c := clearSessionCookie(true)

	rec := httptest.NewRecorder()
	http.SetCookie(rec, c)
	header := rec.Header().Get("Set-Cookie")

	if !strings.Contains(header, "Max-Age=0") {
		t.Fatalf("Set-Cookie header = %q, want it to contain Max-Age=0", header)
	}
	if c.Value != "" {
		t.Fatalf("Value = %q, want empty", c.Value)
	}
}
