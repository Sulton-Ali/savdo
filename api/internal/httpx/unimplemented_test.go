package httpx

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/config"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

// TestUnimplementedOperationsRequireAuthentication proves auth.Service's
// Middleware (wired in by NewRouter) now gates every operation that isn't
// allow-listed — including these still-unimplemented ones — before a
// request can ever reach the notImplementedResponse stub: calling any of
// them with no session answers 401 UNAUTHENTICATED, not the stub's 500.
func TestUnimplementedOperationsRequireAuthentication(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"getShop", http.MethodGet, "/v1/shop"},
		{"listLocations", http.MethodGet, "/v1/locations"},
		{"listStaff", http.MethodGet, "/v1/staff"},
	}

	router := NewRouter(testLogger(), nil, testAuthService())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d (Middleware should reject before the stub runs)", rec.Code, http.StatusUnauthorized)
			}

			var body gen.Error
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.Error.Code != gen.UNAUTHENTICATED {
				t.Fatalf("error.code = %q, want %q", body.Error.Code, gen.UNAUTHENTICATED)
			}
		})
	}
}

// TestUnimplementedOperationsAnswerNotImplementedOnceAuthenticated proves
// that, past Middleware, a still-unimplemented operation answers the
// shared Error envelope (ADR-013) with code INTERNAL and
// details.reason=not_implemented rather than a generic 500 or a panic —
// the same proof TestUnimplementedOperationsRequireAuthentication's
// predecessor made, now exercised with a real session since these
// operations are no longer allow-listed.
func TestUnimplementedOperationsAnswerNotImplementedOnceAuthenticated(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	q := db.New(pool)
	ctx := t.Context()

	shop, err := q.CreateShop(ctx, db.CreateShopParams{ID: uuid.New(), Slug: "shop-a", Name: "Shop A"})
	if err != nil {
		t.Fatalf("CreateShop: %v", err)
	}
	hash, err := auth.Hash("correct-horse-battery")
	if err != nil {
		t.Fatalf("auth.Hash: %v", err)
	}
	if _, err := q.CreateUser(ctx, db.CreateUserParams{
		ID: uuid.New(), ShopID: shop.ID, Username: "owner1", PasswordHash: hash,
		FullName: "Owner", Role: db.UserRoleOwner, Locale: "uz",
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	authSvc := auth.NewService(q, config.Config{
		SessionWebTTL:       time.Hour,
		SessionMobileTTL:    time.Hour,
		LoginRateIPPerMin:   1000,
		LoginRateUserPerMin: 1000,
	}, shop.ID)
	router := NewRouter(testLogger(), pool, authSvc)

	loginBody, err := json.Marshal(map[string]string{
		"username": "owner1", "password": "correct-horse-battery", "client": "web",
	})
	if err != nil {
		t.Fatalf("marshal login body: %v", err)
	}
	loginReq := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200, body = %s", loginRec.Code, loginRec.Body.String())
	}
	cookies := loginRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login response set no cookies")
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/shop", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}

	var body gen.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Code != gen.INTERNAL {
		t.Fatalf("error.code = %q, want %q", body.Error.Code, gen.INTERNAL)
	}
	if body.Error.Details == nil || (*body.Error.Details)["reason"] != "not_implemented" {
		t.Fatalf("error.details = %+v, want reason=not_implemented", body.Error.Details)
	}
}
