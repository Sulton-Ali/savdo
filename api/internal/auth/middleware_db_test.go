package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

// loginAndGetToken logs a seeded user in and returns the raw token, for
// tests that need a real, DB-backed session to authenticate a request
// with.
func loginAndGetToken(t *testing.T, svc *Service, username, password string, client db.SessionClient) string {
	t.Helper()
	result, err := svc.Login(context.Background(), username, password, client, "test-agent", nil)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	return result.Token
}

func TestMiddlewareAuthenticatesAValidCookieSession(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	q := db.New(pool)
	ctx := context.Background()

	shop := seedShop(ctx, t, q, "shop-a")
	user := seedUser(ctx, t, q, shop.ID, "owner1", "correct-horse-battery", db.UserRoleOwner)
	svc := NewService(pool, q, testConfig(), shop.ID)

	token := loginAndGetToken(t, svc, "owner1", "correct-horse-battery", db.SessionClientWeb)

	var gotCtx context.Context
	next := func(ctx context.Context, _ http.ResponseWriter, _ *http.Request, _ any) (any, error) {
		gotCtx = ctx
		return "ok", nil
	}

	r := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: token, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	rec := httptest.NewRecorder()

	_, err := svc.Middleware(next, "GetMe")(ctx, rec, r, nil)
	if err != nil {
		t.Fatalf("Middleware() error = %v, want nil for a valid session", err)
	}

	authCtx, ok := FromContext(gotCtx)
	if !ok {
		t.Fatal("no auth.Context attached to the inner handler's context")
	}
	if authCtx.UserID != user.ID {
		t.Fatalf("UserID = %v, want %v", authCtx.UserID, user.ID)
	}
	if authCtx.ShopID != shop.ID {
		t.Fatalf("ShopID = %v, want %v", authCtx.ShopID, shop.ID)
	}
	if authCtx.Role != db.UserRoleOwner {
		t.Fatalf("Role = %v, want owner", authCtx.Role)
	}
	if authCtx.Client != db.SessionClientWeb {
		t.Fatalf("Client = %v, want web (extracted from the cookie)", authCtx.Client)
	}
}

func TestMiddlewareRejectsARevokedSession(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	q := db.New(pool)
	ctx := context.Background()

	shop := seedShop(ctx, t, q, "shop-a")
	seedUser(ctx, t, q, shop.ID, "owner1", "correct-horse-battery", db.UserRoleOwner)
	svc := NewService(pool, q, testConfig(), shop.ID)

	result, err := svc.Login(ctx, "owner1", "correct-horse-battery", db.SessionClientWeb, "", nil)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if err := svc.Logout(ctx, shop.ID, result.Session.ID, result.User.ID); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}

	next := func(_ context.Context, _ http.ResponseWriter, _ *http.Request, _ any) (any, error) {
		t.Fatal("inner handler must not run for a revoked session")
		return nil, nil
	}

	r := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: result.Token, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	rec := httptest.NewRecorder()

	_, err = svc.Middleware(next, "GetMe")(ctx, rec, r, nil)
	if err == nil {
		t.Fatal("Middleware() error = nil, want Unauthenticated for a revoked session")
	}
	if got := errStatus(t, err); got != 401 {
		t.Fatalf("status = %d, want 401", got)
	}
}

func TestMiddlewareRejectsSessionOfInactiveUser(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	q := db.New(pool)
	ctx := context.Background()

	shop := seedShop(ctx, t, q, "shop-a")
	user := seedUser(ctx, t, q, shop.ID, "owner1", "correct-horse-battery", db.UserRoleOwner)
	svc := NewService(pool, q, testConfig(), shop.ID)

	result, err := svc.Login(ctx, "owner1", "correct-horse-battery", db.SessionClientWeb, "", nil)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	isActive := false
	if _, err := q.UpdateUser(ctx, db.UpdateUserParams{ShopID: shop.ID, ID: user.ID, IsActive: &isActive}); err != nil {
		t.Fatalf("deactivate user: %v", err)
	}

	next := func(_ context.Context, _ http.ResponseWriter, _ *http.Request, _ any) (any, error) {
		t.Fatal("inner handler must not run for a session belonging to an inactive user")
		return nil, nil
	}

	r := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: result.Token, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	rec := httptest.NewRecorder()

	_, err = svc.Middleware(next, "GetMe")(ctx, rec, r, nil)
	if err == nil {
		t.Fatal("Middleware() error = nil, want Unauthenticated")
	}
	if got := errStatus(t, err); got != 401 {
		t.Fatalf("status = %d, want 401", got)
	}
}

func TestMiddlewareCSRFOnCookieAuthenticatedMutatingRequest(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	q := db.New(pool)
	ctx := context.Background()

	shop := seedShop(ctx, t, q, "shop-a")
	seedUser(ctx, t, q, shop.ID, "owner1", "correct-horse-battery", db.UserRoleOwner)
	svc := NewService(pool, q, testConfig(), shop.ID)
	token := loginAndGetToken(t, svc, "owner1", "correct-horse-battery", db.SessionClientWeb)

	next := func(_ context.Context, _ http.ResponseWriter, _ *http.Request, _ any) (any, error) {
		return "ok", nil
	}

	t.Run("missing header is forbidden", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
		r.AddCookie(&http.Cookie{Name: CookieName, Value: token, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
		rec := httptest.NewRecorder()

		_, err := svc.Middleware(next, "Logout")(ctx, rec, r, nil)
		if err == nil {
			t.Fatal("error = nil, want Forbidden")
		}
		if got := errStatus(t, err); got != 403 {
			t.Fatalf("status = %d, want 403", got)
		}
	})

	t.Run("correct header passes", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
		r.AddCookie(&http.Cookie{Name: CookieName, Value: token, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
		r.Header.Set(csrfHeader, csrfHeaderValue)
		rec := httptest.NewRecorder()

		resp, err := svc.Middleware(next, "Logout")(ctx, rec, r, nil)
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if resp != "ok" {
			t.Fatalf("resp = %v, want ok", resp)
		}
	})
}

func TestMiddlewareSlidesExpiryWhenSessionIsStale(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	q := db.New(pool)
	ctx := context.Background()

	shop := seedShop(ctx, t, q, "shop-a")
	seedUser(ctx, t, q, shop.ID, "owner1", "correct-horse-battery", db.UserRoleOwner)
	svc := NewService(pool, q, testConfig(), shop.ID)

	result, err := svc.Login(ctx, "owner1", "correct-horse-battery", db.SessionClientWeb, "", nil)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	// Force last_seen_at into the past, beyond touchThreshold, so
	// Middleware's sliding-expiry check fires on the next request. This
	// goes straight at the pool rather than through a sqlc query because
	// no generated query exists (or should exist) for backdating
	// last_seen_at — it is test-only setup, not application behavior.
	stale := time.Now().Add(-5 * time.Minute)
	if _, err := pool.Exec(ctx, `UPDATE sessions SET last_seen_at = $1 WHERE id = $2`, stale, result.Session.ID); err != nil {
		t.Fatalf("backdate last_seen_at: %v", err)
	}

	next := func(_ context.Context, _ http.ResponseWriter, _ *http.Request, _ any) (any, error) {
		return "ok", nil
	}

	r := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: result.Token, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	rec := httptest.NewRecorder()

	if _, err := svc.Middleware(next, "GetMe")(ctx, rec, r, nil); err != nil {
		t.Fatalf("Middleware() error = %v", err)
	}

	sessions, err := svc.ListSessions(ctx, shop.ID, result.User.ID)
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("len(sessions) = %d, want 1", len(sessions))
	}

	got := sessions[0]
	if !got.ExpiresAt.After(result.Session.ExpiresAt) {
		t.Fatalf("ExpiresAt = %v, want it extended beyond the original %v", got.ExpiresAt, result.Session.ExpiresAt)
	}
	if !got.LastSeenAt.After(stale) {
		t.Fatalf("LastSeenAt = %v, want it advanced past the backdated %v", got.LastSeenAt, stale)
	}
}

// TestMiddlewareCSRFRejectionDoesNotTouchSession proves the CSRF check
// runs before the sliding-expiry touch: a cookie-authenticated mutating
// request that is missing the CSRF header must be rejected without ever
// extending the session it rode in on. Extending it anyway would let a
// blocked, unauthorized-in-spirit request keep an otherwise-idle session
// alive indefinitely.
func TestMiddlewareCSRFRejectionDoesNotTouchSession(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	q := db.New(pool)
	ctx := context.Background()

	shop := seedShop(ctx, t, q, "shop-a")
	seedUser(ctx, t, q, shop.ID, "owner1", "correct-horse-battery", db.UserRoleOwner)
	svc := NewService(pool, q, testConfig(), shop.ID)

	result, err := svc.Login(ctx, "owner1", "correct-horse-battery", db.SessionClientWeb, "", nil)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	stale := time.Now().Add(-5 * time.Minute)
	if _, err := pool.Exec(ctx, `UPDATE sessions SET last_seen_at = $1 WHERE id = $2`, stale, result.Session.ID); err != nil {
		t.Fatalf("backdate last_seen_at: %v", err)
	}

	// Capture the value actually stored (Postgres truncates to
	// microsecond precision, so this is not bit-identical to the Go
	// `stale` value above) as the "before" baseline, rather than
	// comparing against `stale` itself.
	before, err := svc.ListSessions(ctx, shop.ID, result.User.ID)
	if err != nil || len(before) != 1 {
		t.Fatalf("read back baseline session state: sessions=%v err=%v", before, err)
	}

	next := func(_ context.Context, _ http.ResponseWriter, _ *http.Request, _ any) (any, error) {
		t.Fatal("inner handler must not run when the CSRF check fails")
		return nil, nil
	}

	r := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: result.Token, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	// Deliberately no X-Requested-With header.
	rec := httptest.NewRecorder()

	_, err = svc.Middleware(next, "Logout")(ctx, rec, r, nil)
	if err == nil {
		t.Fatal("Middleware() error = nil, want Forbidden (CSRF)")
	}
	if got := errStatus(t, err); got != 403 {
		t.Fatalf("status = %d, want 403", got)
	}

	after, err := svc.ListSessions(ctx, shop.ID, result.User.ID)
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(after) != 1 {
		t.Fatalf("len(sessions) = %d, want 1", len(after))
	}
	if !after[0].LastSeenAt.Equal(before[0].LastSeenAt) {
		t.Fatalf("LastSeenAt = %v, want it unchanged at %v (a CSRF-blocked request must not touch the session)", after[0].LastSeenAt, before[0].LastSeenAt)
	}
	if !after[0].ExpiresAt.Equal(before[0].ExpiresAt) {
		t.Fatalf("ExpiresAt = %v, want it unchanged at %v", after[0].ExpiresAt, before[0].ExpiresAt)
	}
}

// TestMiddlewareRejectsAnExpiredSessionWithoutTouchingIt proves an expired
// session (expires_at in the past) is rejected with 401 — the same
// GetSessionByTokenHash query that authenticates a live session
// (`... AND s.expires_at > now()`) simply doesn't find an expired one, so
// Middleware treats it identically to an unknown token — and, because it
// is never found, is never reached by the sliding-expiry touch either:
// an expired session must not be silently revived by someone still
// presenting its old token.
func TestMiddlewareRejectsAnExpiredSessionWithoutTouchingIt(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	q := db.New(pool)
	ctx := context.Background()

	shop := seedShop(ctx, t, q, "shop-a")
	seedUser(ctx, t, q, shop.ID, "owner1", "correct-horse-battery", db.UserRoleOwner)
	svc := NewService(pool, q, testConfig(), shop.ID)

	result, err := svc.Login(ctx, "owner1", "correct-horse-battery", db.SessionClientWeb, "", nil)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	expiredAt := time.Now().Add(-time.Hour)
	lastSeenBefore := time.Now().Add(-2 * time.Hour)
	if _, err := pool.Exec(ctx,
		`UPDATE sessions SET expires_at = $1, last_seen_at = $2 WHERE id = $3`,
		expiredAt, lastSeenBefore, result.Session.ID,
	); err != nil {
		t.Fatalf("expire the session: %v", err)
	}

	next := func(_ context.Context, _ http.ResponseWriter, _ *http.Request, _ any) (any, error) {
		t.Fatal("inner handler must not run for an expired session")
		return nil, nil
	}

	r := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: result.Token, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	rec := httptest.NewRecorder()

	_, err = svc.Middleware(next, "GetMe")(ctx, rec, r, nil)
	if err == nil {
		t.Fatal("Middleware() error = nil, want Unauthenticated for an expired session")
	}
	if got := errStatus(t, err); got != 401 {
		t.Fatalf("status = %d, want 401", got)
	}

	// Read the row directly (ListUserSessions has no expiry filter,
	// unlike GetSessionByTokenHash) to prove it was left exactly as
	// backdated — no sliding-expiry touch reached it.
	rows, err := q.ListUserSessions(ctx, db.ListUserSessionsParams{ShopID: shop.ID, UserID: result.User.ID})
	if err != nil || len(rows) != 1 {
		t.Fatalf("ListUserSessions: rows=%v err=%v", rows, err)
	}
	if !rows[0].ExpiresAt.Equal(expiredAt.Truncate(time.Microsecond)) {
		t.Fatalf("ExpiresAt = %v, want it unchanged at the expired %v (must not be extended)", rows[0].ExpiresAt, expiredAt)
	}
	if !rows[0].LastSeenAt.Equal(lastSeenBefore.Truncate(time.Microsecond)) {
		t.Fatalf("LastSeenAt = %v, want it unchanged at %v (must not be touched)", rows[0].LastSeenAt, lastSeenBefore)
	}
}
