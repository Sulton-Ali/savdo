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
			name: "stale auth_date (correctly signed, but > 24h old)",
			payload: gen.TelegramAuthRequest{
				Id: "123456789", AuthDate: 1000000000,
				Hash: "4a3170e4f8075756ad3f08f2a93462defda492699faea983f2121fd673323017",
			},
			wantErr: true,
		},
		{
			name:    "wrong bot token",
			payload: signPayload(gen.TelegramAuthRequest{Id: "987654321", AuthDate: int(time.Now().Unix())}, "a-completely-different-bot-token"),
			wantErr: true,
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
