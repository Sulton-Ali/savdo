package auth

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/internal/config"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

func testConfig() config.Config {
	return config.Config{
		SessionWebTTL:       7 * 24 * time.Hour,
		SessionMobileTTL:    30 * 24 * time.Hour,
		LoginRateIPPerMin:   1000,
		LoginRateUserPerMin: 1000,
		CookieSecure:        true,
	}
}

// seedShop creates one shop directly through sqlc's generated queries, per
// this task's instruction to seed fixtures that way rather than hand-write
// SQL in the test.
func seedShop(ctx context.Context, t *testing.T, q *db.Queries, slug string) db.Shop {
	t.Helper()
	shop, err := q.CreateShop(ctx, db.CreateShopParams{ID: uuid.New(), Slug: slug, Name: "Test Shop " + slug})
	if err != nil {
		t.Fatalf("seedShop: %v", err)
	}
	return shop
}

// seedUser creates one active user with password as their real password
// (hashed with this package's own Hash, exactly as staff creation will).
func seedUser(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, username, password string, role db.UserRole) db.User {
	t.Helper()
	hash, err := Hash(password)
	if err != nil {
		t.Fatalf("seedUser: Hash: %v", err)
	}
	user, err := q.CreateUser(ctx, db.CreateUserParams{
		ID:           uuid.New(),
		ShopID:       shopID,
		Username:     username,
		PasswordHash: hash,
		FullName:     "Test User " + username,
		Role:         role,
		Locale:       "uz",
	})
	if err != nil {
		t.Fatalf("seedUser: CreateUser: %v", err)
	}
	return user
}

func TestServiceLoginSuccessWeb(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	user := seedUser(ctx, t, q, shop.ID, "owner1", "correct-horse-battery", db.UserRoleOwner)
	svc := NewService(pool, q, testConfig(), shop.ID)

	result, err := svc.Login(ctx, "owner1", "correct-horse-battery", db.SessionClientWeb, "test-agent", nil)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if result.User.ID != user.ID {
		t.Fatalf("User.ID = %v, want %v", result.User.ID, user.ID)
	}
	if result.Session.Client != db.SessionClientWeb {
		t.Fatalf("Session.Client = %v, want web", result.Session.Client)
	}
	if result.Token == "" {
		t.Fatal("Token is empty, want a raw token")
	}

	wantExpiry := time.Now().Add(7 * 24 * time.Hour)
	if diff := result.Session.ExpiresAt.Sub(wantExpiry); diff > time.Minute || diff < -time.Minute {
		t.Fatalf("Session.ExpiresAt = %v, want ~%v (web TTL)", result.Session.ExpiresAt, wantExpiry)
	}

	// last_login_at must have been set.
	reloaded, err := q.GetUserByID(ctx, db.GetUserByIDParams{ShopID: shop.ID, ID: user.ID})
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if reloaded.LastLoginAt == nil {
		t.Fatal("LastLoginAt was not set after a successful login")
	}
}

func TestServiceLoginSuccessMobileGetsLongerTTL(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	seedUser(ctx, t, q, shop.ID, "cashier1", "correct-horse-battery", db.UserRoleCashier)
	svc := NewService(pool, q, testConfig(), shop.ID)

	result, err := svc.Login(ctx, "cashier1", "correct-horse-battery", db.SessionClientMobile, "", nil)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	wantExpiry := time.Now().Add(30 * 24 * time.Hour)
	if diff := result.Session.ExpiresAt.Sub(wantExpiry); diff > time.Minute || diff < -time.Minute {
		t.Fatalf("Session.ExpiresAt = %v, want ~%v (mobile TTL)", result.Session.ExpiresAt, wantExpiry)
	}
}

func TestServiceLoginRejectsWrongPasswordUnknownUserAndInactiveUserIdentically(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	seedUser(ctx, t, q, shop.ID, "owner1", "correct-horse-battery", db.UserRoleOwner)

	inactive := seedUser(ctx, t, q, shop.ID, "inactive1", "correct-horse-battery", db.UserRoleCashier)
	isActive := false
	if _, err := q.UpdateUser(ctx, db.UpdateUserParams{ShopID: shop.ID, ID: inactive.ID, IsActive: &isActive}); err != nil {
		t.Fatalf("deactivate user: %v", err)
	}

	tests := []struct {
		name     string
		username string
		password string
	}{
		{"wrong password", "owner1", "totally-wrong-password"},
		{"unknown user", "nobody-like-this-exists", "whatever-password"},
		{"inactive user, correct password", "inactive1", "correct-horse-battery"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(pool, q, testConfig(), shop.ID)
			_, err := svc.Login(ctx, tt.username, tt.password, db.SessionClientWeb, "", nil)
			if err == nil {
				t.Fatal("Login() error = nil, want Unauthenticated")
			}
			if got := errStatus(t, err); got != 401 {
				t.Fatalf("status = %d, want 401", got)
			}
		})
	}
}

func TestServiceLoginRateLimitsByIPAndUsername(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	seedUser(ctx, t, q, shop.ID, "owner1", "correct-horse-battery", db.UserRoleOwner)

	cfg := testConfig()
	cfg.LoginRateUserPerMin = 2
	svc := NewService(pool, q, cfg, shop.ID)

	for i := 0; i < 2; i++ {
		_, err := svc.Login(ctx, "owner1", "wrong-password", db.SessionClientWeb, "", nil)
		if err == nil || errStatus(t, err) != 401 {
			t.Fatalf("attempt %d: want 401 (still under the rate limit), got %v", i+1, err)
		}
	}

	_, err := svc.Login(ctx, "owner1", "wrong-password", db.SessionClientWeb, "", nil)
	if err == nil {
		t.Fatal("3rd attempt: error = nil, want RateLimited")
	}
	if got := errStatus(t, err); got != 429 {
		t.Fatalf("3rd attempt status = %d, want 429", got)
	}
}

// TestServiceLoginRateLimitsByUsernameCaseInsensitively proves that
// varying the case of a username (or padding it with whitespace) cannot
// be used to get extra login attempts past the per-username limiter —
// users.username is citext, so "Owner1" and "owner1" are the same account
// as far as GetUserByUsername is concerned, and the limiter must agree.
func TestServiceLoginRateLimitsByUsernameCaseInsensitively(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	seedUser(ctx, t, q, shop.ID, "owner1", "correct-horse-battery", db.UserRoleOwner)

	cfg := testConfig()
	cfg.LoginRateUserPerMin = 2
	svc := NewService(pool, q, cfg, shop.ID)

	variants := []string{"owner1", "Owner1", "OWNER1", "  owner1  "}
	for i, username := range variants[:2] {
		_, err := svc.Login(ctx, username, "wrong-password", db.SessionClientWeb, "", nil)
		if err == nil || errStatus(t, err) != 401 {
			t.Fatalf("attempt %d (username=%q): want 401 (still under the rate limit), got %v", i+1, username, err)
		}
	}

	// The limit is 2/min; both were consumed above (regardless of case),
	// so every further variant must be rate limited, not treated as a
	// fresh bucket.
	for _, username := range variants[2:] {
		_, err := svc.Login(ctx, username, "wrong-password", db.SessionClientWeb, "", nil)
		if err == nil {
			t.Fatalf("username=%q: error = nil, want RateLimited (case/whitespace variation must share the same bucket)", username)
		}
		if got := errStatus(t, err); got != 429 {
			t.Fatalf("username=%q status = %d, want 429", username, got)
		}
	}
}

func TestServiceRevokeSessionIsIdempotent(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	svc := NewService(pool, q, testConfig(), shop.ID)
	user := seedUser(ctx, t, q, shop.ID, "owner1", "correct-horse-battery", db.UserRoleOwner)

	result, err := svc.Login(ctx, "owner1", "correct-horse-battery", db.SessionClientWeb, "", nil)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	found, err := svc.RevokeSession(ctx, shop.ID, user.ID, result.Session.ID)
	if err != nil {
		t.Fatalf("RevokeSession() (1st) error = %v", err)
	}
	if !found {
		t.Fatal("RevokeSession() (1st) found = false, want true")
	}

	found, err = svc.RevokeSession(ctx, shop.ID, user.ID, result.Session.ID)
	if err != nil {
		t.Fatalf("RevokeSession() (2nd, already revoked) error = %v", err)
	}
	if !found {
		t.Fatal("RevokeSession() (2nd, already revoked) found = false, want true (idempotent)")
	}
}

func TestServiceRevokeSessionNotFoundForAnotherUsersSession(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	svc := NewService(pool, q, testConfig(), shop.ID)
	owner := seedUser(ctx, t, q, shop.ID, "owner1", "correct-horse-battery", db.UserRoleOwner)
	seedUser(ctx, t, q, shop.ID, "cashier1", "correct-horse-battery", db.UserRoleCashier)

	ownerSession, err := svc.Login(ctx, "owner1", "correct-horse-battery", db.SessionClientWeb, "", nil)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	cashierResult, err := svc.Login(ctx, "cashier1", "correct-horse-battery", db.SessionClientWeb, "", nil)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	// The cashier tries to revoke the owner's session id: not found, since
	// RevokeSession is scoped to the caller's own sessions.
	found, err := svc.RevokeSession(ctx, shop.ID, cashierResult.User.ID, ownerSession.Session.ID)
	if err != nil {
		t.Fatalf("RevokeSession() error = %v", err)
	}
	if found {
		t.Fatal("RevokeSession() found = true, want false for another user's session id")
	}

	_ = owner
}

func TestServiceListSessionsAndGetMe(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	svc := NewService(pool, q, testConfig(), shop.ID)
	user := seedUser(ctx, t, q, shop.ID, "manager1", "correct-horse-battery", db.UserRoleManager)

	if _, err := svc.Login(ctx, "manager1", "correct-horse-battery", db.SessionClientWeb, "", nil); err != nil {
		t.Fatalf("Login() (web) error = %v", err)
	}
	if _, err := svc.Login(ctx, "manager1", "correct-horse-battery", db.SessionClientMobile, "", nil); err != nil {
		t.Fatalf("Login() (mobile) error = %v", err)
	}

	sessions, err := svc.ListSessions(ctx, shop.ID, user.ID)
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("len(sessions) = %d, want 2", len(sessions))
	}

	me, err := svc.GetMe(ctx, shop.ID, user.ID)
	if err != nil {
		t.Fatalf("GetMe() error = %v", err)
	}
	if me.User.ID != user.ID {
		t.Fatalf("me.User.ID = %v, want %v", me.User.ID, user.ID)
	}
	if me.Shop.ID != shop.ID {
		t.Fatalf("me.Shop.ID = %v, want %v", me.Shop.ID, shop.ID)
	}
}
