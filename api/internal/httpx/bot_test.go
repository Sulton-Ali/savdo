package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/bot"
	"github.com/Sulton-Ali/savdo/api/internal/catalog"
	"github.com/Sulton-Ali/savdo/api/internal/config"
	"github.com/Sulton-Ali/savdo/api/internal/content"
	"github.com/Sulton-Ali/savdo/api/internal/crm"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
	"github.com/Sulton-Ali/savdo/api/internal/media"
	"github.com/Sulton-Ali/savdo/api/internal/public"
	"github.com/Sulton-Ali/savdo/api/internal/sales"
	"github.com/Sulton-Ali/savdo/api/internal/shop"
	"github.com/Sulton-Ali/savdo/api/internal/stock"
)

// noopBotSender is a bot.Sender stand-in for the router-level webhook
// tests below — they only need HandleUpdate to run to completion and
// write real bot_messages rows, never a real Telegram call (D-112).
type noopBotSender struct{}

func (noopBotSender) SendMessage(context.Context, int64, string) error       { return nil }
func (noopBotSender) SendPhoto(context.Context, int64, string, string) error { return nil }

// botTestFixture wires a full router (real auth middleware, real
// bot.Handler) against a real Postgres, with one shop, an owner and a
// cashier — end-to-end coverage of the webhook's secret check and the
// two admin `/bot/conversations*` operations' permission gate.
type botTestFixture struct {
	router          http.Handler
	q               *db.Queries
	shopID          uuid.UUID
	ownerUsername   string
	ownerPassword   string
	cashierUsername string
	cashierPassword string
	secret          string
}

func newBotTestFixture(t *testing.T) botTestFixture {
	t.Helper()
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	q := db.New(pool)
	ctx := context.Background()

	shopRow, err := q.CreateShop(ctx, db.CreateShopParams{ID: uuid.New(), Slug: "bot-httpx", Name: "Bot HTTPX Shop"})
	if err != nil {
		t.Fatalf("CreateShop: %v", err)
	}

	const ownerPassword = "correct-horse-battery"
	ownerHash, err := auth.Hash(ownerPassword)
	if err != nil {
		t.Fatalf("auth.Hash: %v", err)
	}
	owner, err := q.CreateUser(ctx, db.CreateUserParams{
		ID: uuid.New(), ShopID: shopRow.ID, Username: "owner1", PasswordHash: ownerHash,
		FullName: "Owner", Role: db.UserRoleOwner, Locale: "uz",
	})
	if err != nil {
		t.Fatalf("CreateUser(owner): %v", err)
	}

	const cashierPassword = "correct-horse-battery"
	cashierHash, err := auth.Hash(cashierPassword)
	if err != nil {
		t.Fatalf("auth.Hash: %v", err)
	}
	cashier, err := q.CreateUser(ctx, db.CreateUserParams{
		ID: uuid.New(), ShopID: shopRow.ID, Username: "cashier1", PasswordHash: cashierHash,
		FullName: "Cashier", Role: db.UserRoleCashier, Locale: "uz",
	})
	if err != nil {
		t.Fatalf("CreateUser(cashier): %v", err)
	}

	cfg := config.Config{
		SessionWebTTL:       7 * 24 * time.Hour,
		SessionMobileTTL:    30 * 24 * time.Hour,
		LoginRateIPPerMin:   1000,
		LoginRateUserPerMin: 1000,
		CookieSecure:        true,
	}
	authSvc := auth.NewService(q, cfg, shopRow.ID)
	shopSvc := shop.NewService(pool, q)
	mediaSvc := media.NewService(q, nil, "/media", 10<<20, 2, 10)
	catalogSvc := catalog.NewService(pool, q, "uz", "/media")
	stockSvc := stock.NewService(pool, q)
	crmSvc := crm.NewService(q)
	salesSvc := sales.NewService(q)
	contentSvc := content.NewService(q)
	publicSvc := public.NewService(q, contentSvc, shopRow.Slug, "/media")
	pubHandler := public.NewHandler(publicSvc)

	const secret = "test-webhook-secret-xyz"
	// aiClient is nil: every test in this file only ever sends a slash
	// command through the webhook (never free text), and a command
	// reply never calls the model (commands.go's own doc comment) — the
	// tool-loop/ai.Client boundary is internal/bot's own test suite's
	// job (internal/bot/limits_test.go, boundary_test.go), not this
	// router-level one. Avoids this package needing to import
	// internal/ai at all (scripts/guards.sh's own boundary rule: "only
	// internal/bot and cmd/* wiring").
	botSvc := bot.NewService(pool, q, nil, pubHandler, contentSvc, noopBotSender{}, nil,
		bot.Config{ShopID: shopRow.ID, SiteURL: "https://savdo.test"}, nil, testLogger())

	router := NewRouter(testLogger(), pool, authSvc, shopSvc, mediaSvc, nil, catalogSvc, stockSvc, crmSvc,
		testReportsService(), salesSvc, contentSvc, publicSvc, botSvc, secret)

	return botTestFixture{
		router: router, q: q, shopID: shopRow.ID,
		ownerUsername: owner.Username, ownerPassword: ownerPassword,
		cashierUsername: cashier.Username, cashierPassword: cashierPassword,
		secret: secret,
	}
}

func (f botTestFixture) login(t *testing.T, username, password string) *http.Cookie {
	t.Helper()
	body, err := json.Marshal(map[string]string{"username": username, "password": password, "client": "web"})
	if err != nil {
		t.Fatalf("marshal login body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login(%q): status = %d, body = %s", username, rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName {
			return c
		}
	}
	t.Fatalf("login(%q): no session cookie in response", username)
	return nil
}

func (f botTestFixture) get(t *testing.T, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

// telegramUpdateJSON builds a minimal Telegram Update JSON body for one
// text message — the exact shape HandleBotWebhook decodes into
// models.Update (handler.go's own json.Marshal/json.Unmarshal round
// trip).
func telegramUpdateJSON(updateID int, chatID, userID int64, text string) []byte {
	body := fmt.Sprintf(`{
		"update_id": %d,
		"message": {
			"message_id": 1, "date": 1700000000,
			"chat": {"id": %d, "type": "private"},
			"from": {"id": %d, "is_bot": false, "first_name": "Test", "language_code": "uz"},
			"text": %q
		}
	}`, updateID, chatID, userID, text)
	return []byte(body)
}

// TestHandleBotWebhook_wrongSecretIs404 pins the constant-time secret
// check: any non-matching secret 404s, indistinguishable from a route
// Telegram never registered (bot.Handler.HandleBotWebhook's own doc
// comment) — never a 403.
func TestHandleBotWebhook_wrongSecretIs404(t *testing.T) {
	f := newBotTestFixture(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/bot/webhook/not-the-secret", bytes.NewReader(telegramUpdateJSON(1, 1, 100, "/start")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestHandleBotWebhook_correctSecretProcessesUpdate proves a matching
// secret both 200s and actually runs the update through
// Service.HandleUpdate (a real bot_conversations/bot_messages row
// appears) — not just an auth check with no effect.
func TestHandleBotWebhook_correctSecretProcessesUpdate(t *testing.T) {
	f := newBotTestFixture(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/bot/webhook/"+f.secret, bytes.NewReader(telegramUpdateJSON(1, 555, 600, "/start")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}

	conv, err := f.q.GetBotConversationByChat(context.Background(), db.GetBotConversationByChatParams{ShopID: f.shopID, TelegramChatID: 555})
	if err != nil {
		t.Fatalf("GetBotConversationByChat: %v, want a conversation row created by the real update", err)
	}
	if conv.TelegramUserID != 600 {
		t.Fatalf("TelegramUserID = %d, want 600", conv.TelegramUserID)
	}
}

// TestHandleBotWebhook_emptySecretConfiguredAlwaysIs404 pins the safe
// default: an unconfigured (empty) BOT_WEBHOOK_SECRET makes the route
// 404 for every request, even one that happens to send an empty secret
// itself.
func TestHandleBotWebhook_emptySecretConfiguredAlwaysIs404(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	q := db.New(pool)
	ctx := context.Background()
	shopRow, err := q.CreateShop(ctx, db.CreateShopParams{ID: uuid.New(), Slug: "bot-httpx-empty", Name: "Bot HTTPX Empty Secret Shop"})
	if err != nil {
		t.Fatalf("CreateShop: %v", err)
	}
	botSvc := bot.NewService(pool, q, nil, nil, nil, noopBotSender{}, nil, bot.Config{ShopID: shopRow.ID}, nil, testLogger())
	router := NewRouter(testLogger(), pool, testAuthService(), testShopService(), testMediaService(), nil,
		testCatalogService(), testStockService(), testCrmService(), testReportsService(), testSalesService(), testContentService(), testPublicService(),
		botSvc, "") // BOT_WEBHOOK_SECRET unset

	req := httptest.NewRequest(http.MethodPost, "/v1/bot/webhook/", bytes.NewReader(telegramUpdateJSON(1, 1, 100, "/start")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (empty secret path segment)", rec.Code)
	}
}

// seedConversationWithMessage writes one real bot_conversations +
// bot_messages pair directly (not through the webhook, to keep the admin
// handler tests independent of Service.HandleUpdate's own behavior).
func seedConversationWithMessage(t *testing.T, q *db.Queries, shopID uuid.UUID, chatID int64) db.BotConversation {
	t.Helper()
	ctx := context.Background()
	conv, err := q.CreateBotConversation(ctx, db.CreateBotConversationParams{
		ID: uuid.New(), ShopID: shopID, TelegramChatID: chatID, TelegramUserID: chatID + 1000, Mode: db.BotModeCustomer,
	})
	if err != nil {
		t.Fatalf("CreateBotConversation: %v", err)
	}
	msg, err := q.InsertBotMessage(ctx, db.InsertBotMessageParams{
		ID: uuid.New(), ConversationID: conv.ID, ShopID: shopID, Role: db.BotMessageRoleUser, Content: "Hello",
	})
	if err != nil {
		t.Fatalf("InsertBotMessage: %v", err)
	}
	createdAt := msg.CreatedAt
	conv, err = q.TouchBotConversation(ctx, db.TouchBotConversationParams{LastMessageAt: &createdAt, ShopID: shopID, ID: conv.ID})
	if err != nil {
		t.Fatalf("TouchBotConversation: %v", err)
	}
	return conv
}

// TestListBotConversations_ownerSeesRealData_cashierForbidden pins
// auth.PermBotRead's own matrix (owner/manager, never cashier) end to
// end through the real router.
func TestListBotConversations_ownerSeesRealData_cashierForbidden(t *testing.T) {
	f := newBotTestFixture(t)
	conv := seedConversationWithMessage(t, f.q, f.shopID, 42)

	ownerCookie := f.login(t, f.ownerUsername, f.ownerPassword)
	rec := f.get(t, "/v1/bot/conversations", ownerCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner ListBotConversations: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var list gen.BotConversationList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].Id != conv.ID {
		t.Fatalf("Items = %+v, want exactly the seeded conversation", list.Items)
	}

	cashierCookie := f.login(t, f.cashierUsername, f.cashierPassword)
	forbiddenRec := f.get(t, "/v1/bot/conversations", cashierCookie)
	if forbiddenRec.Code != http.StatusForbidden {
		t.Fatalf("cashier ListBotConversations: status = %d, want 403", forbiddenRec.Code)
	}
}

// TestListBotConversationMessages_ownerSeesRealData_wrongShopIs404 pins
// hard rule 1 (shop_id from the auth context, never the request): a
// conversation id from a different shop must 404, not leak another
// shop's transcript.
func TestListBotConversationMessages_ownerSeesRealData_wrongShopIs404(t *testing.T) {
	f := newBotTestFixture(t)
	conv := seedConversationWithMessage(t, f.q, f.shopID, 43)
	ownerCookie := f.login(t, f.ownerUsername, f.ownerPassword)

	rec := f.get(t, "/v1/bot/conversations/"+conv.ID.String()+"/messages", ownerCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var list gen.BotMessageList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].Content != "Hello" {
		t.Fatalf("Items = %+v, want the one seeded message", list.Items)
	}

	otherShop, err := f.q.CreateShop(context.Background(), db.CreateShopParams{ID: uuid.New(), Slug: "bot-httpx-other", Name: "Other Shop"})
	if err != nil {
		t.Fatalf("CreateShop(other): %v", err)
	}
	otherConv := seedConversationWithMessage(t, f.q, otherShop.ID, 44)

	crossRec := f.get(t, "/v1/bot/conversations/"+otherConv.ID.String()+"/messages", ownerCookie)
	if crossRec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for another shop's conversation id", crossRec.Code)
	}
}
