package auth

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

func TestValidateTelegramAuthRequest(t *testing.T) {
	tests := []struct {
		name       string
		body       gen.TelegramAuthRequest
		wantFields []string
	}{
		{
			name: "valid",
			body: gen.TelegramAuthRequest{Id: "123", AuthDate: 1700000000, Hash: "abcd"},
		},
		{
			name:       "missing id",
			body:       gen.TelegramAuthRequest{AuthDate: 1700000000, Hash: "abcd"},
			wantFields: []string{"id"},
		},
		{
			name:       "missing hash",
			body:       gen.TelegramAuthRequest{Id: "123", AuthDate: 1700000000},
			wantFields: []string{"hash"},
		},
		{
			name:       "missing authDate",
			body:       gen.TelegramAuthRequest{Id: "123", Hash: "abcd"},
			wantFields: []string{"authDate"},
		},
		{
			name:       "id too long",
			body:       gen.TelegramAuthRequest{Id: strings.Repeat("1", maxTelegramFieldLength+1), AuthDate: 1700000000, Hash: "abcd"},
			wantFields: []string{"id"},
		},
		{
			name:       "username too long",
			body:       gen.TelegramAuthRequest{Id: "123", AuthDate: 1700000000, Hash: "abcd", Username: strPtr(strings.Repeat("a", maxTelegramFieldLength+1))},
			wantFields: []string{"username"},
		},
		{
			name:       "everything missing",
			body:       gen.TelegramAuthRequest{},
			wantFields: []string{"id", "hash", "authDate"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateTelegramAuthRequest(&tt.body)
			if len(got) != len(tt.wantFields) {
				t.Fatalf("validateTelegramAuthRequest() = %v, want fields %v", got, tt.wantFields)
			}
			for _, f := range tt.wantFields {
				if _, ok := got[f]; !ok {
					t.Fatalf("validateTelegramAuthRequest() = %v, want it to flag field %q", got, f)
				}
			}
		})
	}
}

// TestHandlerAuthenticateTelegramSetsCookieAndReturnsNoToken is Review
// finding 13's own test: it mirrors internal/httpx's own
// TestLoginWebSetsCookieAndReturnsNoToken (the "existing /auth/login
// handler test" the finding names) at this package's own handler level —
// calling *Handler.AuthenticateTelegram directly, the same way
// middleware_db_test.go's own tests call *Service.Middleware directly,
// rather than going through the full router (internal/httpx is outside
// this task's file scope). The Login Widget flow always starts a
// client: web session (telegram.go's own AuthenticateTelegram doc
// comment), so the session cookie/no-token-in-body shape it must produce
// is exactly Login's own web-client one.
func TestHandlerAuthenticateTelegramSetsCookieAndReturnsNoToken(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	svc := NewService(pool, q, testTelegramConfig(), shop.ID)
	h := NewHandler(svc)

	user := seedUser(ctx, t, q, shop.ID, "owner1", "correct-horse-battery", db.UserRoleOwner)
	if _, err := q.LinkTelegramAccount(ctx, db.LinkTelegramAccountParams{
		ID: uuid.New(), UserID: user.ID, ShopID: shop.ID, TelegramUserID: 111,
	}); err != nil {
		t.Fatalf("LinkTelegramAccount: %v", err)
	}

	payload := signPayload(gen.TelegramAuthRequest{Id: "111", AuthDate: int(time.Now().Unix())}, testBotToken)

	rec := httptest.NewRecorder()
	reqCtx := withRequestInfo(ctx, requestInfo{w: rec, userAgent: "test-agent"})

	resp, err := h.AuthenticateTelegram(reqCtx, gen.AuthenticateTelegramRequestObject{Body: &payload})
	if err != nil {
		t.Fatalf("AuthenticateTelegram() error = %v", err)
	}
	if err := resp.VisitAuthenticateTelegramResponse(rec); err != nil {
		t.Fatalf("VisitAuthenticateTelegramResponse() error = %v", err)
	}

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}

	setCookie := rec.Header().Get("Set-Cookie")
	if setCookie == "" {
		t.Fatal("no Set-Cookie header on a successful Telegram login")
	}
	for _, want := range []string{"savdo_session=", "Path=/", "HttpOnly", "Secure", "SameSite=Lax"} {
		if !strings.Contains(setCookie, want) {
			t.Fatalf("Set-Cookie = %q, want it to contain %q", setCookie, want)
		}
	}

	// The raw session token — whatever savdo_session= carries in the
	// cookie — must never also appear in the JSON body (hard rule 9: a
	// session token is only ever handed to the client once, via exactly
	// one channel).
	rawToken := ""
	for _, c := range rec.Result().Cookies() {
		if c.Name == CookieName {
			rawToken = c.Value
		}
	}
	if rawToken == "" {
		t.Fatal("could not parse the raw session token out of Set-Cookie")
	}
	bodyBytes := rec.Body.Bytes()
	if strings.Contains(string(bodyBytes), rawToken) {
		t.Fatalf("response body contains the raw session token: %s", bodyBytes)
	}

	var body gen.LoginResponse
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Token != nil {
		t.Fatalf("Token = %v, want nil (no token in body for a cookie-based Telegram login)", *body.Token)
	}
	if body.User.Username != "owner1" {
		t.Fatalf("User.Username = %q, want owner1", body.User.Username)
	}
}

// TestHandlerCreateTelegramLinkRejectsEmptyBotUsername is BOT_USERNAME's
// own dev-empty-is-allowed change (config.go) regression test at this
// handler's level: config.Load() requires BOT_USERNAME only in prod, so a
// dev deployment can reach this handler with cfg.BotUsername == "" — it
// must refuse to mint a code and hand back a deep link of
// `https://t.me/?start=...` (no bot named, so it goes nowhere) rather
// than silently produce a dead link.
func TestHandlerCreateTelegramLinkRejectsEmptyBotUsername(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	cfg := testTelegramConfig()
	cfg.BotUsername = ""
	svc := NewService(pool, q, cfg, shop.ID)
	h := NewHandler(svc)

	user := seedUser(ctx, t, q, shop.ID, "owner1", "correct-horse-battery", db.UserRoleOwner)
	authCtx := Context{ShopID: shop.ID, UserID: user.ID, Role: db.UserRoleOwner}
	reqCtx := WithContext(ctx, authCtx)

	_, err := h.CreateTelegramLink(reqCtx, gen.CreateTelegramLinkRequestObject{})
	if err == nil {
		t.Fatal("CreateTelegramLink() error = nil, want an error (empty BOT_USERNAME must never produce a deep link)")
	}
	if got := errStatus(t, err); got != 500 {
		t.Fatalf("CreateTelegramLink() status = %d, want 500", got)
	}

	// No code was minted for a dead-end link: GetTelegramLink still
	// reports unlinked and no otp_codes row for db.OtpPurposeLinkTelegram
	// exists for this user.
	linked, _, err := svc.GetTelegramLink(ctx, shop.ID, user.ID)
	if err != nil {
		t.Fatalf("GetTelegramLink() error = %v", err)
	}
	if linked {
		t.Fatal("GetTelegramLink() linked = true, want false")
	}

	var codeCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM otp_codes WHERE shop_id = $1 AND user_id = $2 AND purpose = $3`,
		shop.ID, user.ID, db.OtpPurposeLinkTelegram).Scan(&codeCount); err != nil {
		t.Fatalf("query otp_codes: %v", err)
	}
	if codeCount != 0 {
		t.Fatalf("otp_codes rows for db.OtpPurposeLinkTelegram = %d, want 0 (the handler must refuse before Service.CreateTelegramLink ever mints a code)", codeCount)
	}
}
