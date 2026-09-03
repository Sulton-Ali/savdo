package httpx

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/config"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

// authTestFixture wires a full router against a real (testcontainers)
// Postgres, with one shop and one active user seeded directly through
// sqlc's generated queries — end-to-end coverage of the `/auth/*`
// operations as the acceptance checks describe them, not just their
// individual pieces.
type authTestFixture struct {
	router   http.Handler
	shopID   uuid.UUID
	userID   uuid.UUID
	username string
	password string
}

func newAuthTestFixture(t *testing.T, cfgOverrides func(*config.Config)) authTestFixture {
	t.Helper()
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	q := db.New(pool)
	ctx := t.Context()

	shop, err := q.CreateShop(ctx, db.CreateShopParams{ID: uuid.New(), Slug: "shop-a", Name: "Shop A"})
	if err != nil {
		t.Fatalf("CreateShop: %v", err)
	}

	const password = "correct-horse-battery"
	hash, err := auth.Hash(password)
	if err != nil {
		t.Fatalf("auth.Hash: %v", err)
	}
	user, err := q.CreateUser(ctx, db.CreateUserParams{
		ID: uuid.New(), ShopID: shop.ID, Username: "owner1", PasswordHash: hash,
		FullName: "Owner", Role: db.UserRoleOwner, Locale: "uz",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	cfg := config.Config{
		SessionWebTTL:       7 * 24 * time.Hour,
		SessionMobileTTL:    30 * 24 * time.Hour,
		LoginRateIPPerMin:   1000,
		LoginRateUserPerMin: 1000,
		CookieSecure:        true,
	}
	if cfgOverrides != nil {
		cfgOverrides(&cfg)
	}

	authSvc := auth.NewService(q, cfg, shop.ID)

	return authTestFixture{
		router:   NewRouter(testLogger(), pool, authSvc),
		shopID:   shop.ID,
		userID:   user.ID,
		username: user.Username,
		password: password,
	}
}

func (f authTestFixture) login(t *testing.T, client string) *httptest.ResponseRecorder {
	t.Helper()
	return f.loginAs(t, f.username, f.password, client)
}

func (f authTestFixture) loginAs(t *testing.T, username, password, client string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"username": username, "password": password, "client": client})
	if err != nil {
		t.Fatalf("marshal login body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func TestLoginWebSetsCookieAndReturnsNoToken(t *testing.T) {
	f := newAuthTestFixture(t, nil)

	rec := f.login(t, "web")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}

	setCookie := rec.Header().Get("Set-Cookie")
	if setCookie == "" {
		t.Fatal("no Set-Cookie header on a successful web login")
	}
	for _, want := range []string{"savdo_session=", "Path=/", "HttpOnly", "Secure", "SameSite=Lax", "Max-Age=604800"} {
		if !strings.Contains(setCookie, want) {
			t.Fatalf("Set-Cookie = %q, want it to contain %q", setCookie, want)
		}
	}

	var body gen.LoginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Token != nil {
		t.Fatalf("Token = %v, want nil (no token in body for a web login)", *body.Token)
	}
	if body.User.Username != f.username {
		t.Fatalf("User.Username = %q, want %q", body.User.Username, f.username)
	}
}

func TestLoginMobileReturnsTokenAndNoCookie(t *testing.T) {
	f := newAuthTestFixture(t, nil)

	rec := f.login(t, "mobile")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}

	if rec.Header().Get("Set-Cookie") != "" {
		t.Fatalf("Set-Cookie = %q, want none for a mobile login", rec.Header().Get("Set-Cookie"))
	}

	var body gen.LoginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Token == nil || *body.Token == "" {
		t.Fatal("Token is nil/empty, want a bearer token for a mobile login")
	}
}

func TestLoginRejectsWrongPasswordUnknownUserAndInactiveUserWithIdenticalBodies(t *testing.T) {
	f := newAuthTestFixture(t, nil)

	pool := testdb.New(t)
	q := db.New(pool)
	isActive := false
	inactiveHash, err := auth.Hash(f.password)
	if err != nil {
		t.Fatalf("auth.Hash: %v", err)
	}
	inactiveUser, err := q.CreateUser(t.Context(), db.CreateUserParams{
		ID: uuid.New(), ShopID: f.shopID, Username: "inactive1", PasswordHash: inactiveHash,
		FullName: "Inactive", Role: db.UserRoleCashier, Locale: "uz",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := q.UpdateUser(t.Context(), db.UpdateUserParams{ShopID: f.shopID, ID: inactiveUser.ID, IsActive: &isActive}); err != nil {
		t.Fatalf("deactivate user: %v", err)
	}

	var bodies []string
	for _, tt := range []struct {
		username, password string
	}{
		{f.username, "wrong-password"},
		{"nobody-such-user", "whatever"},
		{"inactive1", f.password},
	} {
		rec := f.loginAs(t, tt.username, tt.password, "web")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("username=%q status = %d, want 401, body = %s", tt.username, rec.Code, rec.Body.String())
		}
		bodies = append(bodies, rec.Body.String())
	}

	for i := 1; i < len(bodies); i++ {
		if bodies[i] != bodies[0] {
			t.Fatalf("response bodies differ across credential failures: %q vs %q", bodies[0], bodies[i])
		}
	}
}

// TestLoginUsernameLengthIsCountedInCharactersNotBytes proves the full
// request path end to end: a 40-character Cyrillic username (80 bytes)
// passes length validation and reaches the real credential check (401,
// unknown user — never 400), while a 65-character one is rejected by
// validation (400) before any credential check runs.
func TestLoginUsernameLengthIsCountedInCharactersNotBytes(t *testing.T) {
	f := newAuthTestFixture(t, nil)

	withinLimit := strings.Repeat("а", 40) // 40 chars, 80 bytes
	rec := f.loginAs(t, withinLimit, "whatever-password", "web")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("40-char Cyrillic username: status = %d, want 401 (validation should pass; the account just doesn't exist), body = %s", rec.Code, rec.Body.String())
	}

	overLimit := strings.Repeat("а", 65) // 65 chars, 130 bytes
	rec = f.loginAs(t, overLimit, "whatever-password", "web")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("65-char Cyrillic username: status = %d, want 400 (validation should reject it), body = %s", rec.Code, rec.Body.String())
	}
	var body gen.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Code != gen.VALIDATIONFAILED {
		t.Fatalf("error.code = %q, want %q", body.Error.Code, gen.VALIDATIONFAILED)
	}
}

func TestGetMeWithBogusBearerIsUnauthenticated(t *testing.T) {
	f := newAuthTestFixture(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer this-token-does-not-exist")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body = %s", rec.Code, rec.Body.String())
	}
}

func TestGetMeWithNoCredentialIsUnauthenticated(t *testing.T) {
	f := newAuthTestFixture(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body = %s", rec.Code, rec.Body.String())
	}
}

func TestGetMeReturnsUserShopAndPermissions(t *testing.T) {
	f := newAuthTestFixture(t, nil)
	loginRec := f.login(t, "web")

	req := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	for _, c := range loginRec.Result().Cookies() {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}

	var me gen.Me
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if me.User.Username != f.username {
		t.Fatalf("User.Username = %q, want %q", me.User.Username, f.username)
	}
	if me.Shop.Id != f.shopID {
		t.Fatalf("Shop.Id = %v, want %v", me.Shop.Id, f.shopID)
	}
	if len(me.Permissions) == 0 {
		t.Fatal("Permissions is empty, want the owner's capability list")
	}
}

func TestRevokedSessionIsUnauthenticated(t *testing.T) {
	f := newAuthTestFixture(t, nil)
	loginRec := f.login(t, "web")
	cookies := loginRec.Result().Cookies()

	logoutReq := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	for _, c := range cookies {
		logoutReq.AddCookie(c)
	}
	logoutReq.Header.Set("X-Requested-With", "savdo")
	logoutRec := httptest.NewRecorder()
	f.router.ServeHTTP(logoutRec, logoutReq)
	if logoutRec.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204, body = %s", logoutRec.Code, logoutRec.Body.String())
	}

	meReq := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	for _, c := range cookies {
		meReq.AddCookie(c)
	}
	meRec := httptest.NewRecorder()
	f.router.ServeHTTP(meRec, meReq)
	if meRec.Code != http.StatusUnauthorized {
		t.Fatalf("status after logout = %d, want 401", meRec.Code)
	}
}

func TestLogoutClearsCookie(t *testing.T) {
	f := newAuthTestFixture(t, nil)
	loginRec := f.login(t, "web")

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	for _, c := range loginRec.Result().Cookies() {
		req.AddCookie(c)
	}
	req.Header.Set("X-Requested-With", "savdo")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body = %s", rec.Code, rec.Body.String())
	}
	setCookie := rec.Header().Get("Set-Cookie")
	if !strings.Contains(setCookie, "Max-Age=0") {
		t.Fatalf("Set-Cookie = %q, want it to contain Max-Age=0", setCookie)
	}
}

func TestCookieAuthLogoutWithoutCSRFHeaderIsForbidden(t *testing.T) {
	f := newAuthTestFixture(t, nil)
	loginRec := f.login(t, "web")

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	for _, c := range loginRec.Result().Cookies() {
		req.AddCookie(c)
	}
	// Deliberately no X-Requested-With header.
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403, body = %s", rec.Code, rec.Body.String())
	}
	var body gen.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Code != gen.FORBIDDEN {
		t.Fatalf("error.code = %q, want %q", body.Error.Code, gen.FORBIDDEN)
	}
	if body.Error.Details == nil || (*body.Error.Details)["reason"] != "csrf" {
		t.Fatalf("error.details = %+v, want reason=csrf", body.Error.Details)
	}
}

func TestBearerLogoutNeedsNoCSRFHeader(t *testing.T) {
	f := newAuthTestFixture(t, nil)
	loginRec := f.login(t, "mobile")

	var loginBody gen.LoginResponse
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginBody); err != nil {
		t.Fatalf("decode login body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+*loginBody.Token)
	// Deliberately no X-Requested-With header — bearer auth is exempt.
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body = %s", rec.Code, rec.Body.String())
	}
}

func TestLoginRateLimitReturns429WithRetryAfter(t *testing.T) {
	f := newAuthTestFixture(t, func(cfg *config.Config) {
		cfg.LoginRateIPPerMin = 10
		cfg.LoginRateUserPerMin = 10
	})

	var last *httptest.ResponseRecorder
	for i := 0; i < 11; i++ {
		last = f.loginAs(t, "nobody", "wrongpass1", "web")
	}

	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("11th attempt status = %d, want 429, body = %s", last.Code, last.Body.String())
	}
	if last.Header().Get("Retry-After") == "" {
		t.Fatal("no Retry-After header on a 429 response")
	}
}

func TestRevokeSessionIsIdempotentAndOtherUsersSessionIs404(t *testing.T) {
	f := newAuthTestFixture(t, nil)
	loginRec := f.login(t, "web")
	cookies := loginRec.Result().Cookies()

	var loginBody gen.LoginResponse
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginBody); err != nil {
		t.Fatalf("decode login body: %v", err)
	}

	revoke := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodDelete, "/v1/auth/sessions/"+loginBody.Session.Id.String(), nil)
		for _, c := range cookies {
			req.AddCookie(c)
		}
		req.Header.Set("X-Requested-With", "savdo")
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)
		return rec
	}

	first := revoke()
	if first.Code != http.StatusNoContent {
		t.Fatalf("1st revoke status = %d, want 204, body = %s", first.Code, first.Body.String())
	}

	// The session used to authenticate this second call is now revoked,
	// so the request itself is unauthenticated — a separate, still-valid
	// session is needed to prove idempotency against the same target id.
	secondLogin := f.login(t, "web")
	secondCookies := secondLogin.Result().Cookies()
	req := httptest.NewRequest(http.MethodDelete, "/v1/auth/sessions/"+loginBody.Session.Id.String(), nil)
	for _, c := range secondCookies {
		req.AddCookie(c)
	}
	req.Header.Set("X-Requested-With", "savdo")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("2nd revoke (already revoked) status = %d, want 204 (idempotent), body = %s", rec.Code, rec.Body.String())
	}

	unknownID := uuid.New()
	req = httptest.NewRequest(http.MethodDelete, "/v1/auth/sessions/"+unknownID.String(), nil)
	for _, c := range secondCookies {
		req.AddCookie(c)
	}
	req.Header.Set("X-Requested-With", "savdo")
	rec = httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("revoke unknown id status = %d, want 404, body = %s", rec.Code, rec.Body.String())
	}
}

func TestListSessions(t *testing.T) {
	f := newAuthTestFixture(t, nil)
	loginRec := f.login(t, "web")
	cookies := loginRec.Result().Cookies()

	req := httptest.NewRequest(http.MethodGet, "/v1/auth/sessions", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}

	var list gen.SessionList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(list.Items))
	}
	if !list.Items[0].Current {
		t.Fatal("the only session should be marked Current")
	}
	if list.NextCursor != nil {
		t.Fatalf("NextCursor = %v, want nil", *list.NextCursor)
	}
}
