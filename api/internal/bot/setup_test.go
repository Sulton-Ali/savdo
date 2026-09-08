// Package bot_test (black-box, same convention internal/public's own
// _test package uses) exercises Service.HandleUpdate end to end against
// a real (testcontainers) Postgres and a scripted ai.Fake — no mocks for
// anything this package itself owns, only for the two boundaries D-112
// names: the Telegram transport (fakeSender below) and the LLM
// (ai.Fake, internal/ai/fake.go, built for exactly this).
package bot_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/ai"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/bot"
	"github.com/Sulton-Ali/savdo/api/internal/content"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
	"github.com/Sulton-Ali/savdo/api/internal/public"
	"github.com/Sulton-Ali/savdo/api/internal/stock"
)

// fakeSender records every SendMessage/SendPhoto call instead of talking
// to Telegram (D-112: "tests use a fake Telegram transport").
type fakeSender struct {
	mu       sync.Mutex
	Messages []sentMessage
	Photos   []sentPhoto
	SendErr  error // when set, every SendMessage/SendPhoto call fails with it
}

type sentMessage struct {
	ChatID int64
	Text   string
}

type sentPhoto struct {
	ChatID  int64
	URL     string
	Caption string
}

func (f *fakeSender) SendMessage(_ context.Context, chatID int64, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.SendErr != nil {
		return f.SendErr
	}
	f.Messages = append(f.Messages, sentMessage{ChatID: chatID, Text: text})
	return nil
}

func (f *fakeSender) SendPhoto(_ context.Context, chatID int64, url, caption string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.SendErr != nil {
		return f.SendErr
	}
	f.Photos = append(f.Photos, sentPhoto{ChatID: chatID, URL: url, Caption: caption})
	return nil
}

// last returns every reply's own text — a SendMessage's Text or a
// SendPhoto's Caption, in send order — for tests that only care what a
// customer would have read, not which of the two calls produced it.
func (f *fakeSender) allReplyTexts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	type ordered struct {
		at   int
		text string
	}
	var out []ordered
	for i, m := range f.Messages {
		out = append(out, ordered{i, m.Text})
	}
	base := len(f.Messages)
	for i, p := range f.Photos {
		out = append(out, ordered{base + i, p.Caption})
	}
	texts := make([]string, len(out))
	for i, o := range out {
		texts[i] = o.text
	}
	return texts
}

// clock is a mutable, test-controlled now() for O-25's rolling-hour and
// daily-budget windows.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock(t time.Time) *clock { return &clock{t: t} }

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// testEnv is one test's whole wiring: a real Postgres-backed bot.Service
// (aiClient scripted per test), plus the pieces a test needs to seed data
// and inspect what the bot did.
type testEnv struct {
	pool    *pgxpool.Pool
	q       *db.Queries
	content *content.Service
	pub     *public.Handler
	shop    db.Shop
	owner   db.User // content_blocks.updated_by's FK target for putContent
	sender  *fakeSender
	clock   *clock
	cfg     bot.Config
	svc     *bot.Service
}

// newTestEnv builds a Service backed by a real (testcontainers) Postgres,
// truncated for isolation, and aiClient (an *ai.Fake for most tests, or a
// stand-in that panics on Chat for command-only tests).
func newTestEnv(t *testing.T, aiClient ai.Client) *testEnv {
	t.Helper()
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	q := db.New(pool)
	ctx := context.Background()

	shopRow, err := q.CreateShop(ctx, db.CreateShopParams{ID: uuid.New(), Slug: "bot-test-" + uuid.NewString(), Name: "Bot Test Shop"})
	if err != nil {
		t.Fatalf("seed shop: %v", err)
	}

	// content_blocks.updated_by is a real FK (docs/04-DATA-MODEL.md § 6);
	// putContent below needs an existing users row to attribute a write
	// to, the same reason internal/content's own tests seed one.
	hash, err := auth.Hash("correct-horse-battery")
	if err != nil {
		t.Fatalf("auth.Hash: %v", err)
	}
	owner, err := q.CreateUser(ctx, db.CreateUserParams{
		ID: uuid.New(), ShopID: shopRow.ID, Username: "owner", PasswordHash: hash,
		FullName: "Bot Test Owner", Role: db.UserRoleOwner, Locale: "uz",
	})
	if err != nil {
		t.Fatalf("seed owner: %v", err)
	}

	contentSvc := content.NewService(q)
	publicSvc := public.NewService(q, contentSvc, shopRow.Slug, "/media")
	pubHandler := public.NewHandler(publicSvc)

	sender := &fakeSender{}
	// clk starts at the real wall clock, not a fixed historical instant:
	// O-25's rate-limit/budget windows are compared against real
	// bot_messages.created_at values (DB-assigned `now()`, InsertBotMessage
	// has no created_at parameter — see insertLLMMessage below), so the
	// service's own clock has to track real time too. clock.advance lets a
	// test move it forward within the same window without needing to wait.
	clk := newClock(time.Now())
	cfg := bot.Config{ShopID: shopRow.ID, SiteURL: "https://savdo.test"}

	svc := bot.NewService(pool, q, aiClient, pubHandler, contentSvc, sender, nil, cfg, clk.now, testLogger())

	return &testEnv{
		pool: pool, q: q, content: contentSvc, pub: pubHandler, shop: shopRow, owner: owner,
		sender: sender, clock: clk, cfg: cfg, svc: svc,
	}
}

// rebuildService replaces svc's ai.Client, keeping every other
// dependency (same pool/queries/shop/sender/clock) — TestHandleUpdate_
// dataBoundary scripts a fresh ai.Fake per scenario and needs a Service
// backed by it, without paying for a new container or re-seeding the
// fixture's catalog/content data for every one of O-27's scenarios.
func (e *testEnv) rebuildService(aiClient ai.Client) {
	e.svc = bot.NewService(e.pool, e.q, aiClient, e.pub, e.content, e.sender, nil, e.cfg, e.clock.now, testLogger())
}

// setBudget sets the shop's daily token budget (O-25) directly through
// UpdateShop (the same query internal/shop.Service.Update uses) — a test
// concern, not something this package's own Service ever writes.
func (e *testEnv) setBudget(t *testing.T, budget int32) {
	t.Helper()
	if _, err := e.q.UpdateShop(context.Background(), db.UpdateShopParams{ID: e.shop.ID, AiDailyTokenBudget: &budget}); err != nil {
		t.Fatalf("setBudget(%d): %v", budget, err)
	}
}

// putContent seeds one content block (Hours/Contacts/Social/...) — the
// same public-read boundary shop_info/handleHours/handleAddress read
// through.
func (e *testEnv) putContent(t *testing.T, key gen.ContentKey, data map[string]interface{}) {
	t.Helper()
	if _, err := e.content.Upsert(context.Background(), e.shop.ID, key, gen.LocaleUz, data, e.owner.ID); err != nil {
		t.Fatalf("putContent(%q): %v", key, err)
	}
}

// conversationID looks up the bot_conversations row HandleUpdate creates
// on a chat's first message — tests that need to seed extra bot_messages
// rows directly (insertLLMMessage below) call this after one real
// HandleUpdate turn to get its id.
func (e *testEnv) conversationID(t *testing.T, chatID int64) uuid.UUID {
	t.Helper()
	conv, err := e.q.GetBotConversationByChat(context.Background(), db.GetBotConversationByChatParams{ShopID: e.shop.ID, TelegramChatID: chatID})
	if err != nil {
		t.Fatalf("GetBotConversationByChat(%d): %v", chatID, err)
	}
	return conv.ID
}

// insertLLMMessage writes one assistant bot_messages row with a non-
// static provider directly (bypassing Service entirely) — O-25's
// CountLLMMessagesSince/SumBotTokensSince tests need more "prior LLM
// turns" than scripting that many real ai.Fake round-trips would be
// worth. created_at is the database's own now() (InsertBotMessage has no
// such parameter, 0022_bot_messages.sql's own append-only design) — this
// package's tests keep testEnv.clock close to real wall time for exactly
// this reason (newTestEnv's own doc comment).
func (e *testEnv) insertLLMMessage(t *testing.T, convID uuid.UUID, inputTokens, outputTokens int32) {
	t.Helper()
	provider, model := "anthropic", "claude-sonnet-5"
	if _, err := e.q.InsertBotMessage(context.Background(), db.InsertBotMessageParams{
		ID: uuid.New(), ConversationID: convID, ShopID: e.shop.ID, Role: db.BotMessageRoleAssistant, Content: "prior reply",
		Provider: &provider, Model: &model, InputTokens: &inputTokens, OutputTokens: &outputTokens,
	}); err != nil {
		t.Fatalf("insertLLMMessage: %v", err)
	}
}

// validHours is a well-formed hours block (O-19: exactly 7 days).
func validHours() map[string]interface{} {
	days := make([]interface{}, 0, 7)
	for _, d := range []string{"mon", "tue", "wed", "thu", "fri", "sat"} {
		days = append(days, map[string]interface{}{"day": d, "closed": false, "open": "09:00", "close": "18:00"})
	}
	days = append(days, map[string]interface{}{"day": "sun", "closed": true})
	return map[string]interface{}{"days": days}
}

func validContacts() map[string]interface{} {
	return map[string]interface{}{"phone": "+998901234567", "address": "Tashkent, Amir Temur 1"}
}

// seedUnit/seedProduct/seedVariant/stockIn mirror internal/public's own
// setup_test.go helpers (same seeding shape the tool loop's tests need
// real catalog data for) — duplicated rather than imported, since
// internal/public's are unexported to its own _test package.
func seedUnit(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID) db.Unit {
	t.Helper()
	u, err := q.UpsertUnit(ctx, db.UpsertUnitParams{ID: uuid.New(), ShopID: shopID, Code: "pcs", Precision: 0})
	if err != nil {
		t.Fatalf("seedUnit: %v", err)
	}
	return u
}

func seedLocation(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, name string) db.Location {
	t.Helper()
	l, err := q.CreateLocation(ctx, db.CreateLocationParams{ID: uuid.New(), ShopID: shopID, Name: name, Kind: db.LocationKindStore, IsDefault: false, IsActive: true})
	if err != nil {
		t.Fatalf("seedLocation(%q): %v", name, err)
	}
	return l
}

type productSpec struct {
	CategoryID         *uuid.UUID
	Slug, Name         string
	BasePrice          string
	CostPrice          string // "" = none (ADR-010: never seed this to prove it leaks — always set it here and assert it never appears)
	PromoPrice         string // "" = no promo
	PromoFrom, PromoTo *time.Time
	IsActive           bool
}

func seedProduct(ctx context.Context, t *testing.T, q *db.Queries, shopID, unitID uuid.UUID, spec productSpec) db.Product {
	t.Helper()
	params := db.CreateProductParams{
		ID: uuid.New(), ShopID: shopID, CategoryID: spec.CategoryID, UnitID: unitID, Slug: spec.Slug,
		BasePrice: numeric(t, spec.BasePrice), IsActive: spec.IsActive,
		PromoFrom: spec.PromoFrom, PromoTo: spec.PromoTo,
	}
	if spec.CostPrice != "" {
		params.CostPrice = numeric(t, spec.CostPrice)
	}
	if spec.PromoPrice != "" {
		params.PromoPrice = numeric(t, spec.PromoPrice)
	}
	p, err := q.CreateProduct(ctx, params)
	if err != nil {
		t.Fatalf("seedProduct(%q): %v", spec.Slug, err)
	}
	if err := q.UpsertProductTranslation(ctx, db.UpsertProductTranslationParams{ProductID: p.ID, Locale: "uz", Name: spec.Name}); err != nil {
		t.Fatalf("seedProduct(%q) translation: %v", spec.Slug, err)
	}
	return p
}

func seedVariant(ctx context.Context, t *testing.T, q *db.Queries, shopID, productID uuid.UUID) db.ProductVariant {
	t.Helper()
	v, err := q.CreateVariant(ctx, db.CreateVariantParams{ID: uuid.New(), ShopID: shopID, ProductID: productID, Attributes: json.RawMessage(`{}`), IsActive: true})
	if err != nil {
		t.Fatalf("seedVariant: %v", err)
	}
	return v
}

func stockIn(ctx context.Context, t *testing.T, pool *pgxpool.Pool, q *db.Queries, shopID, variantID, locationID uuid.UUID, qty string) {
	t.Helper()
	svc := stock.NewService(pool, q)
	reason := db.AdjustmentReasonCountCorrection
	if _, err := svc.MoveInTx(ctx, stock.MoveParams{
		ShopID: shopID, VariantID: variantID, LocationID: locationID, Kind: db.StockMovementKindAdjustment,
		Qty: decimalOf(t, qty), AdjustmentReason: &reason,
	}); err != nil {
		t.Fatalf("stockIn(%s, %s): %v", variantID, qty, err)
	}
}

func numeric(t *testing.T, s string) pgtype.Numeric {
	t.Helper()
	var n pgtype.Numeric
	if err := n.Scan(s); err != nil {
		t.Fatalf("numeric(%q): %v", s, err)
	}
	return n
}

func decimalOf(t *testing.T, s string) decimal.Decimal {
	t.Helper()
	d, err := decimal.NewFromString(s)
	if err != nil {
		t.Fatalf("decimalOf(%q): %v", s, err)
	}
	return d
}

// textUpdate builds a minimal *models.Update carrying one text message —
// the only shape HandleUpdate's own doc comment says this package acts
// on ("customer mode only... anything that is not a text message is
// ignored").
func textUpdate(chatID, telegramUserID int64, username, langCode, text string) *models.Update {
	return &models.Update{
		Message: &models.Message{
			Chat: models.Chat{ID: chatID},
			From: &models.User{ID: telegramUserID, Username: username, LanguageCode: langCode},
			Text: text,
		},
	}
}
