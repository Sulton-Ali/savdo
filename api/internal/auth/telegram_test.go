package auth

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/config"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

// testBotToken is the fixed (fake) bot token every hardcoded HMAC vector
// below was computed against, independently, in Python
// (hashlib.hmac/hashlib.sha256 — not this package's own code), so these
// tests actually exercise VerifyLoginWidget's algorithm rather than
// checking it against itself.
const testBotToken = "test-bot-token-12345"

func strPtr(s string) *string { return &s }

// TestWidgetSignature checks widgetSignature (the HMAC half of
// VerifyLoginWidget, split out precisely so it can be tested on its own —
// telegram.go's own doc comment) against vectors computed independently,
// in Python (hashlib.hmac/hashlib.sha256 — not this package's own code),
// for the exact fixed testBotToken/payload combinations below. This is
// what actually proves the Go implementation matches Telegram's
// documented algorithm, rather than checking Go against Go.
func TestWidgetSignature(t *testing.T) {
	tests := []struct {
		name    string
		payload gen.TelegramAuthRequest
		want    string
	}{
		{
			name: "full payload (id, first_name, username, auth_date)",
			payload: gen.TelegramAuthRequest{
				Id: "123456789", FirstName: strPtr("Test"), Username: strPtr("testuser"),
				AuthDate: 1700000000,
			},
			want: "dbc9021fcd3f4006d0c731742aacb043acaf75bc67aeac5e3f98a625c55418ec",
		},
		{
			name:    "minimal payload (id, auth_date only)",
			payload: gen.TelegramAuthRequest{Id: "987654321", AuthDate: 1700000000},
			want:    "6c1222917c2a356b34df76042a055595a53619ba85e2b8ab35a7f17dec1fdfd4",
		},
		{
			name:    "different id, same auth_date as the stale vector below",
			payload: gen.TelegramAuthRequest{Id: "123456789", AuthDate: 1000000000},
			want:    "4a3170e4f8075756ad3f08f2a93462defda492699faea983f2121fd673323017",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := widgetSignature(tt.payload, testBotToken); got != tt.want {
				t.Fatalf("widgetSignature() = %q, want %q (independently computed in Python)", got, tt.want)
			}
		})
	}
}

func TestVerifyLoginWidget(t *testing.T) {
	valid := gen.TelegramAuthRequest{
		Id: "123456789", FirstName: strPtr("Test"), Username: strPtr("testuser"),
		AuthDate: 1700000000, Hash: "dbc9021fcd3f4006d0c731742aacb043acaf75bc67aeac5e3f98a625c55418ec",
	}
	minimal := gen.TelegramAuthRequest{
		Id: "987654321", AuthDate: 1700000000,
		Hash: "6c1222917c2a356b34df76042a055595a53619ba85e2b8ab35a7f17dec1fdfd4",
	}

	tests := []struct {
		name        string
		payload     gen.TelegramAuthRequest
		wantErr     bool
		wantID      int64
		wantUserame string
	}{
		{
			// This vector's authDate (1700000000, ~Nov 2023) is long past
			// telegramAuthMaxAge by the time this test runs — a real
			// widget payload's authDate is always close to "now", so
			// exercising the accept path uses a freshly signed one
			// (signPayload) instead; this fixed, independently computed
			// vector's job (TestWidgetSignature above) is proving the HMAC
			// math itself, not staleness.
			name:        "freshly signed valid payload is accepted",
			payload:     signPayload(gen.TelegramAuthRequest{Id: "123456789", Username: strPtr("testuser"), AuthDate: int(time.Now().Unix())}, testBotToken),
			wantID:      123456789,
			wantUserame: "testuser",
		},
		{
			name: "tampered field (username changed after signing)",
			payload: gen.TelegramAuthRequest{
				Id: valid.Id, FirstName: valid.FirstName, Username: strPtr("attacker"),
				AuthDate: valid.AuthDate, Hash: valid.Hash,
			},
			wantErr: true,
		},
		{
			name:    "tampered id",
			payload: gen.TelegramAuthRequest{Id: "000000000", AuthDate: minimal.AuthDate, Hash: minimal.Hash},
			wantErr: true,
		},
		{
			name:    "missing/empty hash",
			payload: gen.TelegramAuthRequest{Id: minimal.Id, AuthDate: minimal.AuthDate, Hash: ""},
			wantErr: true,
		},
		{
			name: "stale auth_date (correctly signed, but far past telegramAuthMaxAge)",
			payload: gen.TelegramAuthRequest{
				Id: "123456789", AuthDate: 1000000000,
				Hash: "4a3170e4f8075756ad3f08f2a93462defda492699faea983f2121fd673323017",
			},
			wantErr: true,
		},
		{
			// D-117: the widget window is 5 minutes. A payload 6 minutes old
			// must already be rejected.
			name: "auth_date 6 minutes old is rejected",
			payload: func() gen.TelegramAuthRequest {
				past := int(time.Now().Add(-6 * time.Minute).Unix())
				return signPayload(gen.TelegramAuthRequest{Id: "123456789", AuthDate: past}, testBotToken)
			}(),
			wantErr: true,
		},
		{
			// D-117: a payload 4 minutes old is still within the 5-minute
			// window and must be accepted.
			name: "auth_date 4 minutes old is accepted",
			payload: func() gen.TelegramAuthRequest {
				past := int(time.Now().Add(-4 * time.Minute).Unix())
				return signPayload(gen.TelegramAuthRequest{Id: "123456789", Username: strPtr("testuser"), AuthDate: past}, testBotToken)
			}(),
			wantID:      123456789,
			wantUserame: "testuser",
		},
		{
			name:    "wrong bot token",
			payload: signPayload(gen.TelegramAuthRequest{Id: "987654321", AuthDate: int(time.Now().Unix())}, "a-completely-different-bot-token"),
			wantErr: true,
		},
		{
			// Review finding 8's trivial half: a correctly-signed payload
			// timestamped meaningfully in the future is rejected, not
			// just a stale one.
			name: "auth_date far in the future is rejected",
			payload: func() gen.TelegramAuthRequest {
				future := int(time.Now().Add(telegramAuthMaxSkew * 10).Unix())
				return signPayload(gen.TelegramAuthRequest{Id: "123456789", AuthDate: future}, testBotToken)
			}(),
			wantErr: true,
		},
		{
			// Small clock drift between this server and Telegram's must
			// still be tolerated.
			name: "auth_date a few seconds in the future is within tolerated skew",
			payload: func() gen.TelegramAuthRequest {
				future := int(time.Now().Add(5 * time.Second).Unix())
				return signPayload(gen.TelegramAuthRequest{Id: "123456789", Username: strPtr("testuser"), AuthDate: future}, testBotToken)
			}(),
			wantID:      123456789,
			wantUserame: "testuser",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, username, err := VerifyLoginWidget(tt.payload, testBotToken)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("VerifyLoginWidget() error = nil, want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("VerifyLoginWidget() error = %v, want nil", err)
			}
			if id != tt.wantID {
				t.Fatalf("id = %d, want %d", id, tt.wantID)
			}
			if username != tt.wantUserame {
				t.Fatalf("username = %q, want %q", username, tt.wantUserame)
			}
		})
	}
}

// TestVerifyLoginWidgetRejectsEmptyBotToken is Review CRITICAL 1's own
// regression test: an empty botToken must never be treated as "sign with
// an empty key" — secret_key would be SHA-256(""), a public constant
// anyone can compute, so an attacker who forges a payload against that
// same empty string must still be rejected. This calls VerifyLoginWidget
// with an explicit "" second argument (unlike TestVerifyLoginWidget's own
// table, which always checks against the fixed testBotToken) — the case
// that actually exercises the fix.
func TestVerifyLoginWidgetRejectsEmptyBotToken(t *testing.T) {
	payload := signPayload(gen.TelegramAuthRequest{
		Id: "123456789", Username: strPtr("testuser"), AuthDate: int(time.Now().Unix()),
	}, "")

	_, _, err := VerifyLoginWidget(payload, "")
	if err == nil {
		t.Fatal("VerifyLoginWidget(payload, \"\") error = nil, want an error (empty bot token must never validate anything)")
	}
}

func testTelegramConfig() config.Config {
	cfg := testConfig()
	cfg.TelegramBotToken = testBotToken
	cfg.BotUsername = "savdo_test_bot"
	return cfg
}

// signPayload signs p with widgetSignature (telegram.go) — used only to
// build a payload for tests whose point is something other than the HMAC
// check itself (TestWidgetSignature/TestVerifyLoginWidget above already
// cover that against vectors computed independently in Python).
func signPayload(p gen.TelegramAuthRequest, botToken string) gen.TelegramAuthRequest {
	p.Hash = widgetSignature(p, botToken)
	return p
}

func TestServiceAuthenticateTelegram(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	svc := NewService(pool, q, testTelegramConfig(), shop.ID)

	linkedActive := seedUser(ctx, t, q, shop.ID, "linked-active", "correct-horse-battery", db.UserRoleOwner)
	if _, err := q.LinkTelegramAccount(ctx, db.LinkTelegramAccountParams{
		ID: uuid.New(), UserID: linkedActive.ID, ShopID: shop.ID, TelegramUserID: 111,
	}); err != nil {
		t.Fatalf("LinkTelegramAccount: %v", err)
	}

	linkedInactive := seedUser(ctx, t, q, shop.ID, "linked-inactive", "correct-horse-battery", db.UserRoleCashier)
	if _, err := q.LinkTelegramAccount(ctx, db.LinkTelegramAccountParams{
		ID: uuid.New(), UserID: linkedInactive.ID, ShopID: shop.ID, TelegramUserID: 222,
	}); err != nil {
		t.Fatalf("LinkTelegramAccount: %v", err)
	}
	isActive := false
	if _, err := q.UpdateUser(ctx, db.UpdateUserParams{ShopID: shop.ID, ID: linkedInactive.ID, IsActive: &isActive}); err != nil {
		t.Fatalf("deactivate user: %v", err)
	}

	t.Run("linked active user succeeds", func(t *testing.T) {
		payload := signPayload(gen.TelegramAuthRequest{Id: "111", AuthDate: int(time.Now().Unix())}, testBotToken)
		result, err := svc.AuthenticateTelegram(ctx, payload, "test-agent", nil)
		if err != nil {
			t.Fatalf("AuthenticateTelegram() error = %v", err)
		}
		if result.User.ID != linkedActive.ID {
			t.Fatalf("User.ID = %v, want %v", result.User.ID, linkedActive.ID)
		}
		if result.Session.Client != db.SessionClientWeb {
			t.Fatalf("Session.Client = %v, want web", result.Session.Client)
		}
	})

	t.Run("unlinked telegram id is unauthenticated", func(t *testing.T) {
		payload := signPayload(gen.TelegramAuthRequest{Id: "999", AuthDate: int(time.Now().Unix())}, testBotToken)
		_, err := svc.AuthenticateTelegram(ctx, payload, "", nil)
		if err == nil || errStatus(t, err) != 401 {
			t.Fatalf("AuthenticateTelegram() error = %v, want 401", err)
		}
	})

	t.Run("linked but inactive user is unauthenticated", func(t *testing.T) {
		payload := signPayload(gen.TelegramAuthRequest{Id: "222", AuthDate: int(time.Now().Unix())}, testBotToken)
		_, err := svc.AuthenticateTelegram(ctx, payload, "", nil)
		if err == nil || errStatus(t, err) != 401 {
			t.Fatalf("AuthenticateTelegram() error = %v, want 401", err)
		}
	})

	t.Run("bad HMAC is unauthenticated", func(t *testing.T) {
		payload := gen.TelegramAuthRequest{Id: "111", AuthDate: int(time.Now().Unix()), Hash: "0000"}
		_, err := svc.AuthenticateTelegram(ctx, payload, "", nil)
		if err == nil || errStatus(t, err) != 401 {
			t.Fatalf("AuthenticateTelegram() error = %v, want 401", err)
		}
	})
}

func TestTelegramLinkLifecycle(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	svc := NewService(pool, q, testTelegramConfig(), shop.ID)
	user := seedUser(ctx, t, q, shop.ID, "owner1", "correct-horse-battery", db.UserRoleOwner)

	linked, username, err := svc.GetTelegramLink(ctx, shop.ID, user.ID)
	if err != nil {
		t.Fatalf("GetTelegramLink() (before) error = %v", err)
	}
	if linked || username != nil {
		t.Fatalf("GetTelegramLink() (before) = (%v, %v), want (false, nil)", linked, username)
	}

	result, err := svc.CreateTelegramLink(ctx, shop.ID, user.ID)
	if err != nil {
		t.Fatalf("CreateTelegramLink() error = %v", err)
	}
	if result.Code == "" {
		t.Fatal("CreateTelegramLink() Code is empty")
	}

	if err := svc.CompleteLink(ctx, result.Code, 555, "tg_username"); err != nil {
		t.Fatalf("CompleteLink() error = %v", err)
	}

	linked, username, err = svc.GetTelegramLink(ctx, shop.ID, user.ID)
	if err != nil {
		t.Fatalf("GetTelegramLink() (after) error = %v", err)
	}
	if !linked {
		t.Fatal("GetTelegramLink() (after) linked = false, want true")
	}
	if username == nil || *username != "tg_username" {
		t.Fatalf("GetTelegramLink() (after) username = %v, want tg_username", username)
	}

	// The code is single use: completing it again must fail.
	if err := svc.CompleteLink(ctx, result.Code, 555, "tg_username"); err == nil {
		t.Fatal("CompleteLink() (reused code) error = nil, want ErrLinkCodeInvalid")
	}

	if err := svc.DeleteTelegramLink(ctx, shop.ID, user.ID); err != nil {
		t.Fatalf("DeleteTelegramLink() error = %v", err)
	}
	linked, _, err = svc.GetTelegramLink(ctx, shop.ID, user.ID)
	if err != nil {
		t.Fatalf("GetTelegramLink() (after unlink) error = %v", err)
	}
	if linked {
		t.Fatal("GetTelegramLink() (after unlink) linked = true, want false")
	}

	// Unlinking again is idempotent.
	if err := svc.DeleteTelegramLink(ctx, shop.ID, user.ID); err != nil {
		t.Fatalf("DeleteTelegramLink() (2nd) error = %v", err)
	}
}

func TestCompleteLinkInvalidCode(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	svc := NewService(pool, q, testTelegramConfig(), shop.ID)

	if err := svc.CompleteLink(ctx, "not-a-real-code", 1, ""); err == nil {
		t.Fatal("CompleteLink() (garbage code) error = nil, want ErrLinkCodeInvalid")
	}
}

func TestCompleteLinkWrongVerifierLocksAfterMaxAttempts(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	svc := NewService(pool, q, testTelegramConfig(), shop.ID)
	user := seedUser(ctx, t, q, shop.ID, "owner1", "correct-horse-battery", db.UserRoleOwner)

	result, err := svc.CreateTelegramLink(ctx, shop.ID, user.ID)
	if err != nil {
		t.Fatalf("CreateTelegramLink() error = %v", err)
	}

	// Same selector (first userSelectorLen chars), wrong verifier tail.
	wrongCode := result.Code[:userSelectorLen] + "WRONGVERIFIERXXXXXXXXXXXXXXXXXX"

	for i := 0; i < otpMaxAttempts; i++ {
		if err := svc.CompleteLink(ctx, wrongCode, 1, ""); err == nil {
			t.Fatalf("attempt %d: CompleteLink() error = nil, want ErrLinkCodeInvalid", i+1)
		}
	}

	// Even the correct code must now fail: attempts exhausted, the row is
	// locked (otp.go/CompleteLink's otpMaxAttempts check).
	if err := svc.CompleteLink(ctx, result.Code, 1, ""); err == nil {
		t.Fatal("CompleteLink() (correct code, after max attempts) error = nil, want ErrLinkCodeInvalid")
	}
}

func TestCompleteLinkTelegramAccountAlreadyLinkedToAnotherUser(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	svc := NewService(pool, q, testTelegramConfig(), shop.ID)

	alreadyLinked := seedUser(ctx, t, q, shop.ID, "already-linked", "correct-horse-battery", db.UserRoleOwner)
	if _, err := q.LinkTelegramAccount(ctx, db.LinkTelegramAccountParams{
		ID: uuid.New(), UserID: alreadyLinked.ID, ShopID: shop.ID, TelegramUserID: 42,
	}); err != nil {
		t.Fatalf("LinkTelegramAccount: %v", err)
	}

	other := seedUser(ctx, t, q, shop.ID, "other", "correct-horse-battery", db.UserRoleCashier)
	result, err := svc.CreateTelegramLink(ctx, shop.ID, other.ID)
	if err != nil {
		t.Fatalf("CreateTelegramLink() error = %v", err)
	}

	err = svc.CompleteLink(ctx, result.Code, 42, "")
	if err != ErrTelegramAlreadyLinked {
		t.Fatalf("CompleteLink() error = %v, want ErrTelegramAlreadyLinked", err)
	}
}

// TestAuthenticateTelegramRejectsAccountLinkedInAnotherShop is Review
// finding 6's cross-shop test: GetTelegramAccountByTelegramUserID
// (db/queries/auth_telegram.sql's own doc comment) is deliberately the
// one lookup in this whole flow that runs before a shop_id is known —
// AuthenticateTelegram's own account.ShopID != s.shopID guard
// (telegram.go) is what actually enforces the tenant boundary
// afterwards. This proves shop A's Service never authenticates as shop
// B's user just because the same Telegram id happens to be linked there.
func TestAuthenticateTelegramRejectsAccountLinkedInAnotherShop(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shopA := seedShop(ctx, t, q, "shop-a")
	shopB := seedShop(ctx, t, q, "shop-b")

	shopBUser := seedUser(ctx, t, q, shopB.ID, "shop-b-owner", "correct-horse-battery", db.UserRoleOwner)
	if _, err := q.LinkTelegramAccount(ctx, db.LinkTelegramAccountParams{
		ID: uuid.New(), UserID: shopBUser.ID, ShopID: shopB.ID, TelegramUserID: 777,
	}); err != nil {
		t.Fatalf("LinkTelegramAccount: %v", err)
	}

	svcA := NewService(pool, q, testTelegramConfig(), shopA.ID)
	payload := signPayload(gen.TelegramAuthRequest{Id: "777", AuthDate: int(time.Now().Unix())}, testBotToken)

	_, err := svcA.AuthenticateTelegram(ctx, payload, "", nil)
	if err == nil || errStatus(t, err) != 401 {
		t.Fatalf("AuthenticateTelegram() (shop A, account linked in shop B) error = %v, want 401", err)
	}

	// The same telegram id authenticates fine against shop B's own
	// Service — proving the rejection above is really about the shop
	// boundary, not a broken payload/link.
	svcB := NewService(pool, q, testTelegramConfig(), shopB.ID)
	result, err := svcB.AuthenticateTelegram(ctx, payload, "", nil)
	if err != nil {
		t.Fatalf("AuthenticateTelegram() (shop B, account linked in shop B) error = %v, want nil", err)
	}
	if result.User.ID != shopBUser.ID {
		t.Fatalf("User.ID = %v, want %v", result.User.ID, shopBUser.ID)
	}
}

// TestAuthenticateTelegramRateLimitedByIP mirrors
// TestRequestOtpRateLimitsByIPAndUsername/TestServiceLoginRateLimitsByIPAndUsername
// (Review S2): AuthenticateTelegram shares Login's own ipLimiter
// (telegram.go), so it must be rate limited the same way.
func TestAuthenticateTelegramRateLimitedByIP(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	cfg := testTelegramConfig()
	cfg.LoginRateIPPerMin = 2
	svc := NewService(pool, q, cfg, shop.ID)

	// Every payload here fails the HMAC check (no telegram id is linked),
	// but that happens after the rate limiter — same ordering Login's own
	// rate-limit test relies on.
	badPayload := gen.TelegramAuthRequest{Id: "1", AuthDate: int(time.Now().Unix()), Hash: "0000"}

	for i := 0; i < 2; i++ {
		_, err := svc.AuthenticateTelegram(ctx, badPayload, "", nil)
		if err == nil || errStatus(t, err) != 401 {
			t.Fatalf("attempt %d: want 401 (still under the rate limit), got %v", i+1, err)
		}
	}

	_, err := svc.AuthenticateTelegram(ctx, badPayload, "", nil)
	if err == nil {
		t.Fatal("3rd attempt: error = nil, want RateLimited")
	}
	if got := errStatus(t, err); got != 429 {
		t.Fatalf("3rd attempt status = %d, want 429", got)
	}
}

// TestCompleteLinkSecondAttemptWithSameCodeFailsAndDoesNotRelink is Review
// MAJOR 3's own regression test: MarkOTPUsed and LinkTelegramAccount now
// happen in one transaction, MarkOTPUsed first — a second CompleteLink
// call for an already-consumed code must fail before it can touch
// telegram_accounts at all, not just "fail eventually".
func TestCompleteLinkSecondAttemptWithSameCodeFailsAndDoesNotRelink(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	svc := NewService(pool, q, testTelegramConfig(), shop.ID)
	user := seedUser(ctx, t, q, shop.ID, "owner1", "correct-horse-battery", db.UserRoleOwner)

	result, err := svc.CreateTelegramLink(ctx, shop.ID, user.ID)
	if err != nil {
		t.Fatalf("CreateTelegramLink() error = %v", err)
	}

	if err := svc.CompleteLink(ctx, result.Code, 900, "first_username"); err != nil {
		t.Fatalf("CompleteLink() (1st) error = %v", err)
	}

	// A second call with the same code, attempting to relink to a
	// *different* Telegram id — must fail outright, and must not change
	// the link the first call already made.
	err = svc.CompleteLink(ctx, result.Code, 901, "attacker_username")
	if err != ErrLinkCodeInvalid {
		t.Fatalf("CompleteLink() (2nd, same code) error = %v, want ErrLinkCodeInvalid", err)
	}

	linked, username, err := svc.GetTelegramLink(ctx, shop.ID, user.ID)
	if err != nil {
		t.Fatalf("GetTelegramLink() error = %v", err)
	}
	if !linked {
		t.Fatal("GetTelegramLink() linked = false, want true")
	}
	if username == nil || *username != "first_username" {
		t.Fatalf("GetTelegramLink() username = %v, want first_username (the 2nd CompleteLink call must not have relinked)", username)
	}
}

// TestCompleteLinkRateLimitedByTelegramUserID mirrors
// TestAuthenticateTelegramRateLimitedByIP: CompleteLink shares Login's own
// ipLimiter (D-118), keyed by telegramUserID since it has no HTTP IP to
// key on (telegramLimiterKey) — every call here reuses the same
// telegramUserID so it lands in the same bucket.
func TestCompleteLinkRateLimitedByTelegramUserID(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	cfg := testTelegramConfig()
	cfg.LoginRateIPPerMin = 2
	svc := NewService(pool, q, cfg, shop.ID)

	// Every call here uses a garbage code, so it fails on parseSelectorToken
	// (ErrLinkCodeInvalid) — same ordering as the other rate-limit tests:
	// the limiter check runs first.
	const rateLimitedTelegramID = int64(4242)

	for i := 0; i < 2; i++ {
		err := svc.CompleteLink(ctx, "not-a-real-code", rateLimitedTelegramID, "")
		if err != ErrLinkCodeInvalid {
			t.Fatalf("attempt %d: error = %v, want ErrLinkCodeInvalid (still under the rate limit)", i+1, err)
		}
	}

	err := svc.CompleteLink(ctx, "not-a-real-code", rateLimitedTelegramID, "")
	if err == nil {
		t.Fatal("3rd attempt: error = nil, want RateLimited")
	}
	if got := errStatus(t, err); got != 429 {
		t.Fatalf("3rd attempt status = %d, want 429", got)
	}

	// A different Telegram id is a different bucket and is unaffected.
	if err := svc.CompleteLink(ctx, "not-a-real-code", rateLimitedTelegramID+1, ""); err != ErrLinkCodeInvalid {
		t.Fatalf("different telegram id: error = %v, want ErrLinkCodeInvalid (own bucket, not rate limited)", err)
	}
}

// TestCompleteLinkRejectsInactiveUser is Review MINOR 10's own regression
// test: a code requested while the user was active but completed after
// an owner deactivated the account in between must not complete the
// link.
func TestCompleteLinkRejectsInactiveUser(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	svc := NewService(pool, q, testTelegramConfig(), shop.ID)
	user := seedUser(ctx, t, q, shop.ID, "owner1", "correct-horse-battery", db.UserRoleOwner)

	result, err := svc.CreateTelegramLink(ctx, shop.ID, user.ID)
	if err != nil {
		t.Fatalf("CreateTelegramLink() error = %v", err)
	}

	isActive := false
	if _, err := q.UpdateUser(ctx, db.UpdateUserParams{ShopID: shop.ID, ID: user.ID, IsActive: &isActive}); err != nil {
		t.Fatalf("deactivate user: %v", err)
	}

	if err := svc.CompleteLink(ctx, result.Code, 42, ""); err != ErrLinkCodeInvalid {
		t.Fatalf("CompleteLink() (inactive user) error = %v, want ErrLinkCodeInvalid", err)
	}

	linked, _, err := svc.GetTelegramLink(ctx, shop.ID, user.ID)
	if err != nil {
		t.Fatalf("GetTelegramLink() error = %v", err)
	}
	if linked {
		t.Fatal("GetTelegramLink() linked = true, want false (an inactive user must not complete a link)")
	}
}
