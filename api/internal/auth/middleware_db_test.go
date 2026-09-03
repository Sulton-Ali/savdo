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
	svc := NewService(q, testConfig(), shop.ID)

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
	svc := NewService(q, testConfig(), shop.ID)

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
	svc := NewService(q, testConfig(), shop.ID)

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
	svc := NewService(q, testConfig(), shop.ID)
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
	svc := NewService(q, testConfig(), shop.ID)

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
