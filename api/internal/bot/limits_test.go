package bot_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Sulton-Ali/savdo/api/internal/ai"
	"github.com/Sulton-Ali/savdo/api/internal/bot"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// scriptedAnswer is one no-tool-call Response scripted result — the
// simplest possible ai.Fake turn: the model answers straight away.
func scriptedAnswer(text string) ai.FakeResult {
	return ai.FakeResult{Response: ai.Response{
		Text: text, StopReason: "end_turn",
		Usage: ai.Usage{Provider: "anthropic", Model: "claude-sonnet-5", InputTokens: 10, OutputTokens: 10, CostEstimate: "0.000120"},
	}}
}

// rateLimitedTextUz and fallbackTextUz mirror texts.go's own uz map
// entries verbatim — MINOR 7's own fix: an external _test package
// cannot call the unexported localeTexts/fallbackText this package's
// own gate replies are built from, so these are the literal expected
// strings, not a substring/non-empty check.
const rateLimitedTextUz = "Bir soatda savollar soni chegarasiga yetdingiz. Iltimos, birozdan so'ng qayta yozing."

func fallbackTextUz(shopName string) string {
	return `Men faqat "` + shopName + `" do'konining mahsulotlari, narxlari, mavjudligi, ish vaqti, manzili va aloqalari bo'yicha yordam bera olaman.`
}

// assertPersistedUserAndAssistant pins Sonnet/Opus MAJOR 1: a gated
// (rate-limited/over-budget) turn must still leave both the customer's
// own question and the static reply in the transcript (O-25's own "the
// rate-limited message is stored"; D-114's whole retention purpose) —
// never only the assistant row with no question behind it.
func assertPersistedUserAndAssistant(t *testing.T, env *testEnv, chatID int64, wantQuestion string) {
	t.Helper()
	convID := env.conversationID(t, chatID)
	rows, err := env.q.ListBotMessages(context.Background(), db.ListBotMessagesParams{ShopID: env.shop.ID, ConversationID: convID, Limit: 1000})
	if err != nil {
		t.Fatalf("ListBotMessages: %v", err)
	}
	var sawUser, sawAssistant bool
	for _, r := range rows {
		if r.Role == db.BotMessageRoleUser && r.Content == wantQuestion {
			sawUser = true
		}
		if r.Role == db.BotMessageRoleAssistant {
			sawAssistant = true
		}
	}
	if !sawUser {
		t.Fatalf("want the user's own question %q persisted even though this turn was gated", wantQuestion)
	}
	if !sawAssistant {
		t.Fatalf("want a static assistant reply persisted alongside the question")
	}
}

// TestHandleUpdate_perChatRateLimit pins O-25: the 21st free-text turn in
// a rolling hour gets the static rate-limited reply instead of ever
// reaching the model — proven here by scripting zero ai.Fake results, so
// a Chat call at all would panic the test.
func TestHandleUpdate_perChatRateLimit(t *testing.T) {
	env := newTestEnv(t, ai.NewFake())
	ctx := context.Background()
	const chatID = int64(1)

	// First turn creates the conversation naturally so insertLLMMessage
	// below has an id to attach to — but it must not itself call Chat
	// (the fake has zero scripted results): send /start instead of free
	// text.
	env.svc.HandleUpdate(ctx, textUpdate(chatID, 100, "alice", "uz", "/start"))
	convID := env.conversationID(t, chatID)

	// Seed 20 prior LLM turns directly (O-25's own limit) — cheaper than
	// scripting 20 real round-trips, and this test cares about the
	// gate, not the tool loop.
	for i := 0; i < 20; i++ {
		env.insertLLMMessage(t, convID, 10, 10)
	}

	const question = "Do you have shoes?"
	env.svc.HandleUpdate(ctx, textUpdate(chatID, 100, "alice", "uz", question))

	got := lastReply(t, env.sender.allReplyTexts())
	if got != rateLimitedTextUz {
		t.Fatalf("reply = %q, want the exact O-24 rate-limited text %q", got, rateLimitedTextUz)
	}
	assertPersistedUserAndAssistant(t, env, chatID, question)
}

// TestHandleUpdate_perShopDailyBudget pins O-25's other gate: once the
// shop's daily token spend is at or over ai_daily_token_budget, a
// free-text turn answers statically (with the shop's contact line) and
// never reaches the model — again proven by an empty ai.Fake.
func TestHandleUpdate_perShopDailyBudget(t *testing.T) {
	env := newTestEnv(t, ai.NewFake())
	ctx := context.Background()
	const chatID = int64(2)

	env.setBudget(t, 100)
	env.svc.HandleUpdate(ctx, textUpdate(chatID, 200, "bob", "uz", "/start"))
	convID := env.conversationID(t, chatID)
	env.insertLLMMessage(t, convID, 60, 60) // 120 >= 100

	const question = "Do you have shoes?"
	env.svc.HandleUpdate(ctx, textUpdate(chatID, 200, "bob", "uz", question))

	got := lastReply(t, env.sender.allReplyTexts())
	want := fallbackTextUz(env.shop.Name) // no contacts seeded here -> no trailing contact line
	if got != want {
		t.Fatalf("reply = %q, want the exact O-24 fallback text %q", got, want)
	}
	assertPersistedUserAndAssistant(t, env, chatID, question)
}

// TestHandleUpdate_underBudgetCallsModel is the budget gate's converse:
// under budget, a free-text turn does reach the model.
func TestHandleUpdate_underBudgetCallsModel(t *testing.T) {
	fake := ai.NewFake(scriptedAnswer("We have shoes in stock."))
	env := newTestEnv(t, fake)
	env.setBudget(t, 1_000_000)

	env.svc.HandleUpdate(context.Background(), textUpdate(3, 300, "carl", "uz", "Do you have shoes?"))

	if len(fake.Requests) != 1 {
		t.Fatalf("Chat called %d times, want 1", len(fake.Requests))
	}
	got := lastReply(t, env.sender.allReplyTexts())
	if got != "We have shoes in stock." {
		t.Fatalf("reply = %q, want the scripted answer verbatim", got)
	}
}

// TestHandleUpdate_dailyBudgetResetsNextDay pins O-25's own "until
// midnight": once the shop's local day rolls over, SumBotTokensSince's
// window moves past yesterday's spend and a free-text turn reaches the
// model again — the budget gate's own day-boundary behavior, not just
// "is under/over right now".
func TestHandleUpdate_dailyBudgetResetsNextDay(t *testing.T) {
	env := newTestEnv(t, ai.NewFake())
	const chatID = int64(4)

	env.setBudget(t, 100)
	env.svc.HandleUpdate(context.Background(), textUpdate(chatID, 400, "dana", "uz", "/start"))
	convID := env.conversationID(t, chatID)
	env.insertLLMMessage(t, convID, 60, 60) // 120 >= 100, over budget today

	env.svc.HandleUpdate(context.Background(), textUpdate(chatID, 400, "dana", "uz", "Do you have shoes?"))
	if lastReply(t, env.sender.allReplyTexts()) == "" {
		t.Fatalf("want a static over-budget reply before the clock advances")
	}

	env.clock.advance(25 * time.Hour) // past local midnight, whatever the shop's timezone
	fake := ai.NewFake(scriptedAnswer("Yes, in stock."))
	env.rebuildService(fake)

	env.svc.HandleUpdate(context.Background(), textUpdate(chatID, 400, "dana", "uz", "Do you have shoes?"))

	if len(fake.Requests) != 1 {
		t.Fatalf("Chat called %d times after the day rolled over, want exactly 1 (yesterday's spend must not count today)", len(fake.Requests))
	}
}

// buildService is the common shape every D-120 test below needs: a
// fresh *bot.Service over env's existing (real, testcontainers) database
// and fixtures, with its own aiClient and cfg — env.svc/env.cfg
// themselves are left untouched so newTestEnv's own default (env budget
// 200000) never leaks into a scenario deliberately testing a different
// value.
func buildService(env *testEnv, aiClient ai.Client, cfg bot.Config) *bot.Service {
	return bot.NewService(env.pool, env.q, aiClient, env.pub, env.content, env.sender, nil, cfg, env.clock.now, testLogger())
}

// TestHandleUpdate_dailyBudget_envAppliesWhenShopNull pins D-120/R2: a
// shop with no ai_daily_token_budget row set (NULL, never touched by
// env.setBudget here) is capped by Config.DailyTokenBudget (AI_DAILY_
// TOKEN_BUDGET) instead of O-25's old "NULL means unlimited" reading —
// once usage is at or over that env-level cap, the bot answers
// statically without ever reaching the model, proven by a zero-result
// ai.Fake that panics if Chat is ever called.
func TestHandleUpdate_dailyBudget_envAppliesWhenShopNull(t *testing.T) {
	env := newTestEnv(t, nil)
	ctx := context.Background()
	const chatID = int64(11)
	cfg := bot.Config{ShopID: env.shop.ID, SiteURL: "https://savdo.test", PriceInputPerMTok: "2.00", PriceOutputPerMTok: "10.00", DailyTokenBudget: 100}

	env.svc = buildService(env, ai.NewFake(), cfg) // /start never calls Chat
	env.svc.HandleUpdate(ctx, textUpdate(chatID, 1100, "eve", "uz", "/start"))
	convID := env.conversationID(t, chatID)
	env.insertLLMMessage(t, convID, 60, 60) // 120 >= env cap of 100; shop's own column stays NULL

	env.svc.HandleUpdate(ctx, textUpdate(chatID, 1100, "eve", "uz", "Do you have shoes?"))
	if lastReply(t, env.sender.allReplyTexts()) == "" {
		t.Fatalf("want a static over-(env-)budget reply")
	}
}

// TestHandleUpdate_dailyBudget_shopColumnOverridesEnv pins D-120/R2's
// other half: once a shop sets its own ai_daily_token_budget, that value
// wins over Config.DailyTokenBudget, even when the env-level cap alone
// would already be exceeded.
func TestHandleUpdate_dailyBudget_shopColumnOverridesEnv(t *testing.T) {
	env := newTestEnv(t, nil)
	ctx := context.Background()
	const chatID = int64(12)
	cfg := bot.Config{ShopID: env.shop.ID, SiteURL: "https://savdo.test", PriceInputPerMTok: "2.00", PriceOutputPerMTok: "10.00", DailyTokenBudget: 1}

	env.svc = buildService(env, ai.NewFake(), cfg)
	env.setBudget(t, 1_000_000) // the shop's own column, far above the env's own 1
	env.svc.HandleUpdate(ctx, textUpdate(chatID, 1200, "frank", "uz", "/start"))
	convID := env.conversationID(t, chatID)
	env.insertLLMMessage(t, convID, 20, 20) // 40 >> env cap (1), << shop cap (1,000,000)

	fake := ai.NewFake(scriptedAnswer("We have shoes in stock."))
	env.svc = buildService(env, fake, cfg)
	env.svc.HandleUpdate(ctx, textUpdate(chatID, 1200, "frank", "uz", "Do you have shoes?"))

	if len(fake.Requests) != 1 {
		t.Fatalf("Chat called %d times, want 1 (the shop's own budget must win over the env cap)", len(fake.Requests))
	}
}

// TestHandleUpdate_dailyBudget_neitherConfigured_failsClosed pins
// D-120/R2's own fail-closed rule: shop.ai_daily_token_budget NULL *and*
// Config.DailyTokenBudget 0 (AI_DAILY_TOKEN_BUDGET explicitly empty/0)
// means no cap is configured at all — the bot must never read that as
// "unlimited"; it answers statically without ever calling the model.
func TestHandleUpdate_dailyBudget_neitherConfigured_failsClosed(t *testing.T) {
	env := newTestEnv(t, nil)
	ctx := context.Background()
	const chatID = int64(13)
	cfg := bot.Config{ShopID: env.shop.ID, SiteURL: "https://savdo.test", PriceInputPerMTok: "2.00", PriceOutputPerMTok: "10.00", DailyTokenBudget: 0}

	env.svc = buildService(env, ai.NewFake(), cfg) // zero scripted results: panics if Chat is ever called
	env.svc.HandleUpdate(ctx, textUpdate(chatID, 1300, "gina", "uz", "Do you have shoes?"))

	if lastReply(t, env.sender.allReplyTexts()) == "" {
		t.Fatalf("want a static fail-closed reply (no budget configured at all)")
	}
}

// budgetQueryErrorDBTX wraps a real db.DBTX, injecting a failure into
// only SumBotTokensSince's own query (matched by its own distinctive
// "total_tokens" alias, bot.sql.go's own SQL text) — every other query
// (GetShop, the conversation lookup, the rate-limit count, ...) still
// runs for real, so MAJOR 3's test can isolate exactly the query
// shopOverBudget itself makes.
type budgetQueryErrorDBTX struct {
	real interface {
		Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error)
		Query(context.Context, string, ...interface{}) (pgx.Rows, error)
		QueryRow(context.Context, string, ...interface{}) pgx.Row
	}
}

func (d budgetQueryErrorDBTX) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	return d.real.Exec(ctx, sql, args...)
}
func (d budgetQueryErrorDBTX) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	return d.real.Query(ctx, sql, args...)
}
func (d budgetQueryErrorDBTX) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	if strings.Contains(sql, "total_tokens") {
		return errQueryRow{err: errors.New("simulated: budget query unavailable")}
	}
	return d.real.QueryRow(ctx, sql, args...)
}

type errQueryRow struct{ err error }

func (r errQueryRow) Scan(...interface{}) error { return r.err }

// dbNewWithErrorInjection wraps pool in budgetQueryErrorDBTX and builds
// a *db.Queries over it — db.New takes db.DBTX as a plain interface
// (internal/db/db.go), so this needs no mock of internal/bot itself.
func dbNewWithErrorInjection(pool interface {
	Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error)
	Query(context.Context, string, ...interface{}) (pgx.Rows, error)
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}) *db.Queries {
	return db.New(budgetQueryErrorDBTX{real: pool})
}

// TestHandleUpdate_budgetCheckQueryFails_failsClosed pins MAJOR 3: a
// failed budget check (SumBotTokensSince itself erroring, not just "over
// budget") must fail closed the same way a failed rate-limit check
// already does — a static reply, never "log it and call the LLM
// anyway".
func TestHandleUpdate_budgetCheckQueryFails_failsClosed(t *testing.T) {
	env := newTestEnv(t, nil)
	ctx := context.Background()
	const chatID = int64(14)

	q := dbNewWithErrorInjection(env.pool)
	env.svc = bot.NewService(env.pool, q, ai.NewFake(), env.pub, env.content, env.sender, nil, env.cfg, env.clock.now, testLogger())

	const question = "Do you have shoes?"
	env.svc.HandleUpdate(ctx, textUpdate(chatID, 1400, "hank", "uz", question))

	if lastReply(t, env.sender.allReplyTexts()) == "" {
		t.Fatalf("want a static fail-closed reply when the budget query itself errors")
	}
	// MAJOR 1: the question is persisted before the budget check ever
	// runs, so a budget-query failure must not lose it either.
	assertPersistedUserAndAssistant(t, env, chatID, question)
}
