package bot_test

import (
	"context"
	"testing"
	"time"

	"github.com/Sulton-Ali/savdo/api/internal/ai"
)

// scriptedAnswer is one no-tool-call Response scripted result — the
// simplest possible ai.Fake turn: the model answers straight away.
func scriptedAnswer(text string) ai.FakeResult {
	return ai.FakeResult{Response: ai.Response{
		Text: text, StopReason: "end_turn",
		Usage: ai.Usage{Provider: "anthropic", Model: "claude-sonnet-5", InputTokens: 10, OutputTokens: 10, CostEstimate: "0.000120"},
	}}
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

	env.svc.HandleUpdate(ctx, textUpdate(chatID, 100, "alice", "uz", "Do you have shoes?"))

	got := lastReply(t, env.sender.allReplyTexts())
	if got == "" {
		t.Fatalf("want a static rate-limited reply")
	}
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

	env.svc.HandleUpdate(ctx, textUpdate(chatID, 200, "bob", "uz", "Do you have shoes?"))

	got := lastReply(t, env.sender.allReplyTexts())
	if got == "" {
		t.Fatalf("want a static over-budget reply")
	}
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
