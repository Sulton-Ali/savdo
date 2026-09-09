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
	"github.com/jackc/pgx/v5/pgtype"

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

// slowBotSender sleeps delay before every SendMessage/SendPhoto call —
// item 8's own stand-in for "a slow fake service still running" (this
// package cannot build a slow ai.Client, see newBotTestFixtureWithSender's
// own doc comment).
type slowBotSender struct{ delay time.Duration }

func (s slowBotSender) SendMessage(_ context.Context, _ int64, _ string) error {
	time.Sleep(s.delay)
	return nil
}
func (s slowBotSender) SendPhoto(_ context.Context, _ int64, _, _ string) error {
	time.Sleep(s.delay)
	return nil
}

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
	return newBotTestFixtureWithSender(t, noopBotSender{})
}

// newBotTestFixtureWithSender is newBotTestFixture with an injectable
// bot.Sender — item 8's own "handler returns fast while a slow update
// is still processing" test needs a sender slow enough to observe, and
// this package cannot import internal/ai to build a slow ai.Client
// instead (scripts/guards.sh's own boundary rule, hard rule 12: "only
// internal/bot and cmd/* wiring") — a slash-command-only scenario with a
// slow *Sender* proves the same thing this handler actually promises
// (HandleBotWebhook itself never blocks on Service.HandleUpdate), without
// needing a real or fake LLM call at all.
func newBotTestFixtureWithSender(t *testing.T, sender bot.Sender) botTestFixture {
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
	botSvc := bot.NewService(pool, q, nil, pubHandler, contentSvc, sender, nil,
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
// trip). No username: item 11's own "nil when Telegram gives none" case.
func telegramUpdateJSON(updateID int, chatID, userID int64, text string) []byte {
	return telegramUpdateJSONFull(updateID, chatID, userID, "", "private", text)
}

// telegramUpdateJSONFull is telegramUpdateJSON with a username and an
// explicit chat type — item 11 (username persisted) and Sonnet/Opus
// MAJOR 4 (group chats reject /start link_<code>) both need one of
// those telegramUpdateJSON itself does not carry.
func telegramUpdateJSONFull(updateID int, chatID, userID int64, username, chatType, text string) []byte {
	fromUsername := ""
	if username != "" {
		fromUsername = fmt.Sprintf(`, "username": %q`, username)
	}
	body := fmt.Sprintf(`{
		"update_id": %d,
		"message": {
			"message_id": 1, "date": 1700000000,
			"chat": {"id": %d, "type": %q},
			"from": {"id": %d, "is_bot": false, "first_name": "Test", "language_code": "uz"%s},
			"text": %q
		}
	}`, updateID, chatID, chatType, userID, fromUsername, text)
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
// Service.Dispatch (a real bot_conversations/bot_messages row appears)
// — not just an auth check with no effect. HandleBotWebhook itself only
// acknowledges 200 and hands off to Dispatch's own background goroutine
// (item 8), so the conversation row can still be missing for a few
// milliseconds after ServeHTTP returns — pollUntil below waits for it
// instead of asserting immediately.
func TestHandleBotWebhook_correctSecretProcessesUpdate(t *testing.T) {
	f := newBotTestFixture(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/bot/webhook/"+f.secret, bytes.NewReader(telegramUpdateJSON(1, 555, 600, "/start")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}

	conv := pollForConversation(t, f.q, f.shopID, 555)
	if conv.TelegramUserID != 600 {
		t.Fatalf("TelegramUserID = %d, want 600", conv.TelegramUserID)
	}
}

// pollForConversation waits (up to 2s, 10ms between attempts — generous
// for HandleUpdate's own real work here, a single /start command against
// a real but tiny Postgres, no LLM call) for chatID's conversation row to
// appear, the way an assertion right after ServeHTTP returns no longer
// can once HandleBotWebhook hands off to Service.Dispatch's own
// background goroutine instead of running synchronously (item 8).
func pollForConversation(t *testing.T, q *db.Queries, shopID uuid.UUID, chatID int64) db.BotConversation {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		conv, err := q.GetBotConversationByChat(context.Background(), db.GetBotConversationByChatParams{ShopID: shopID, TelegramChatID: chatID})
		if err == nil {
			return conv
		}
		lastErr = err
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("GetBotConversationByChat: %v, want a conversation row created by the real (async) update within the poll window", lastErr)
	return db.BotConversation{}
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

// TestListBotConversationMessages_costEstimateSixDecimals pins item 6:
// bot_messages.cost_estimate is NUMERIC(10,6) (0022_bot_messages.sql,
// O-28), not money's own NUMERIC(14,2) — the contract field must render
// all six fractional digits ("0.000140"), never rounded down to money's
// 2dp ("0.00"), or a real (small, sub-cent) LLM cost reads as free.
func TestListBotConversationMessages_costEstimateSixDecimals(t *testing.T) {
	f := newBotTestFixture(t)
	ctx := context.Background()

	conv, err := f.q.CreateBotConversation(ctx, db.CreateBotConversationParams{
		ID: uuid.New(), ShopID: f.shopID, TelegramChatID: 900, TelegramUserID: 901, Mode: db.BotModeCustomer,
	})
	if err != nil {
		t.Fatalf("CreateBotConversation: %v", err)
	}

	var cost pgtype.Numeric
	if err := cost.Scan("0.000140"); err != nil {
		t.Fatalf("cost.Scan: %v", err)
	}
	provider, model := "anthropic", "claude-sonnet-5"
	var inTok, outTok, lat int32 = 30, 8, 500
	if _, err := f.q.InsertBotMessage(ctx, db.InsertBotMessageParams{
		ID: uuid.New(), ConversationID: conv.ID, ShopID: f.shopID, Role: db.BotMessageRoleAssistant, Content: "Yes, in stock.",
		Provider: &provider, Model: &model, InputTokens: &inTok, OutputTokens: &outTok, LatencyMs: &lat, CostEstimate: cost,
	}); err != nil {
		t.Fatalf("InsertBotMessage: %v", err)
	}

	ownerCookie := f.login(t, f.ownerUsername, f.ownerPassword)
	rec := f.get(t, "/v1/bot/conversations/"+conv.ID.String()+"/messages", ownerCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var list gen.BotMessageList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("Items = %+v, want exactly the one seeded message", list.Items)
	}
	got, err := list.Items[0].CostEstimate.Get()
	if err != nil {
		t.Fatalf("CostEstimate.Get: %v (want a value, not null)", err)
	}
	if got != "0.000140" {
		t.Fatalf("CostEstimate = %q, want \"0.000140\" (6dp), not money's 2dp rounding", got)
	}
}

// TestHandleBotWebhook_telegramUsernamePersistedAndShownInAdminList
// pins item 11: bot_conversations.telegram_username is set from the
// triggering update's message.from.username, and the admin's own GET
// /bot/conversations surfaces it as a real string (not always null, the
// pre-fix behavior).
func TestHandleBotWebhook_telegramUsernamePersistedAndShownInAdminList(t *testing.T) {
	f := newBotTestFixture(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/bot/webhook/"+f.secret, bytes.NewReader(telegramUpdateJSONFull(2, 777, 778, "alice_tg", "private", "/start")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	pollForConversation(t, f.q, f.shopID, 777)

	ownerCookie := f.login(t, f.ownerUsername, f.ownerPassword)
	listRec := f.get(t, "/v1/bot/conversations", ownerCookie)
	if listRec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", listRec.Code, listRec.Body.String())
	}
	var list gen.BotConversationList
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var found *gen.BotConversation
	for i := range list.Items {
		if list.Items[i].TelegramChatId == "777" {
			found = &list.Items[i]
		}
	}
	if found == nil {
		t.Fatalf("Items = %+v, want the conversation for chat 777", list.Items)
	}
	username, err := found.TelegramUsername.Get()
	if err != nil {
		t.Fatalf("TelegramUsername.Get: %v (want \"alice_tg\", not null)", err)
	}
	if username != "alice_tg" {
		t.Fatalf("TelegramUsername = %q, want %q", username, "alice_tg")
	}
}

// TestHandleBotWebhook_returnsFastWhileUpdateStillProcessing pins item
// 8: HandleBotWebhook itself only validates the secret and decodes the
// body, then hands off to Service.Dispatch and acknowledges 200 —
// ServeHTTP must return in low milliseconds even though the update it
// just accepted is still running (here, blocked inside a slow Sender)
// well past that.
func TestHandleBotWebhook_returnsFastWhileUpdateStillProcessing(t *testing.T) {
	const sendDelay = 300 * time.Millisecond
	f := newBotTestFixtureWithSender(t, slowBotSender{delay: sendDelay})

	req := httptest.NewRequest(http.MethodPost, "/v1/bot/webhook/"+f.secret, bytes.NewReader(telegramUpdateJSON(3, 999, 1000, "/start")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	start := time.Now()
	f.router.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	if elapsed >= sendDelay {
		t.Fatalf("ServeHTTP took %s, want well under the slow sender's own %s delay (the update must run in the background, not inline)", elapsed, sendDelay)
	}

	// The update did eventually run (proof this is really async, not
	// just "the sender's own error was swallowed").
	pollForConversation(t, f.q, f.shopID, 999)
}
