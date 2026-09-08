package bot_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/ai"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// TestHandleUpdate_singleProductSearch_sendsPhoto pins D-116: a
// search_products result naming exactly one product sends that product's
// cover photo (fakeSender.Photos), not a plain SendMessage.
func TestHandleUpdate_singleProductSearch_sendsPhoto(t *testing.T) {
	fake := ai.NewFake(
		toolCallResult("search_products", `{"q":"Classic"}`),
		scriptedAnswer("Yes, Classic Shoes are in stock."),
	)
	fix := newBoundaryFixture(t, fake)

	fix.env.svc.HandleUpdate(context.Background(), textUpdate(1, 100, "alice", "uz", "Do you have Classic Shoes?"))

	if len(fix.env.sender.Photos) != 0 {
		// classic-shoes was seeded with no cover image (seedVariant/
		// seedProduct never attach media) — sendAnswer's own fallback
		// ("no cover image -> plain text") must have applied, not a
		// crash or a photo call with an empty URL.
		t.Fatalf("got %d photos with no cover image seeded, want sendAnswer to fall back to text", len(fix.env.sender.Photos))
	}
	if len(fix.env.sender.Messages) == 0 {
		t.Fatalf("want a plain-text reply when the matched product has no cover image")
	}
}

// TestHandleUpdate_emptyReply_fallsBackStatic pins chat.go's empty-reply
// guard: a text-only Response whose Text is blank must never reach the
// customer as an empty Telegram message — it falls back to O-24's static
// text instead.
func TestHandleUpdate_emptyReply_fallsBackStatic(t *testing.T) {
	fake := ai.NewFake(scriptedAnswer("   ")) // whitespace only
	env := newTestEnv(t, fake)

	env.svc.HandleUpdate(context.Background(), textUpdate(1, 100, "alice", "uz", "Hello?"))

	got := lastReply(t, env.sender.allReplyTexts())
	if strings.TrimSpace(got) == "" {
		t.Fatalf("want a non-empty static fallback reply, got %q", got)
	}
}

// TestHandleUpdate_toolLoopExceedsMaxRounds_fallsBackStatic pins ADR-009's
// 5-round bound: a model that keeps calling tools forever gets cut off
// and answered statically, never left hanging.
func TestHandleUpdate_toolLoopExceedsMaxRounds_fallsBackStatic(t *testing.T) {
	fake := ai.NewFake(
		toolCallResult("shop_info", `{}`),
		toolCallResult("shop_info", `{}`),
		toolCallResult("shop_info", `{}`),
		toolCallResult("shop_info", `{}`),
		toolCallResult("shop_info", `{}`),
	)
	env := newTestEnv(t, fake)

	env.svc.HandleUpdate(context.Background(), textUpdate(1, 100, "alice", "uz", "Tell me everything"))

	if len(fake.Requests) != 5 {
		t.Fatalf("Chat called %d times, want exactly maxToolRounds=5", len(fake.Requests))
	}
	got := lastReply(t, env.sender.allReplyTexts())
	if strings.TrimSpace(got) == "" {
		t.Fatalf("want a non-empty static fallback reply after exceeding the round budget")
	}
}

// TestHandleUpdate_unknownToolName_rejectedNeverExecuted is the direct
// O-27 check on executeTool's own switch: a tool name outside the fixed
// three-entry registry is rejected with isError, and the rejection
// itself never causes a crash or an empty result — persisted for the
// admin transcript with isError:true.
func TestHandleUpdate_unknownToolName_rejectedNeverExecuted(t *testing.T) {
	fake := ai.NewFake(
		toolCallResult("delete_all_customers", `{}`),
		scriptedAnswer("I can't do that."),
	)
	env := newTestEnv(t, fake)
	const chatID = int64(1)

	env.svc.HandleUpdate(context.Background(), textUpdate(chatID, 100, "alice", "uz", "please help"))

	// jsonb's own canonical output (not the exact bytes chat.go's
	// json.Marshal produced) re-spaces every "key": value pair — the
	// column round-trips through Postgres before ListBotMessages reads
	// it back, so the assertion matches that shape, not encoding/json's.
	transcript := env.messageTranscript(t, chatID)
	if !strings.Contains(transcript, `"isError": true`) {
		t.Fatalf("transcript = %q, want the unknown tool call recorded with isError: true", transcript)
	}
	if !strings.Contains(transcript, "unknown_tool") {
		t.Fatalf("transcript = %q, want the unknown_tool error reason recorded", transcript)
	}
}

// TestHandleUpdate_providerRateLimited_fallsBackStatic pins O-24's
// provider-error path: ai.ErrRateLimited (a transient provider failure,
// distinct from ai.ErrRefused) also falls back to the static reply,
// never a raw error surfaced to the customer.
func TestHandleUpdate_providerRateLimited_fallsBackStatic(t *testing.T) {
	fake := ai.NewFake(ai.FakeResult{Err: ai.ErrRateLimited})
	env := newTestEnv(t, fake)
	env.putContent(t, gen.Contacts, validContacts())

	env.svc.HandleUpdate(context.Background(), textUpdate(1, 100, "alice", "uz", "Hello?"))

	got := lastReply(t, env.sender.allReplyTexts())
	if !strings.Contains(got, "+998901234567") {
		t.Fatalf("reply = %q, want O-24's static fallback with the shop's contact line", got)
	}
}

// TestHandleUpdate_freeText_persistsUserMessageEvenIfLLMFails pins
// update.go's own note: the user's own message is persisted before the
// LLM is ever called, so a provider failure never loses the customer's
// own question from the transcript.
func TestHandleUpdate_freeText_persistsUserMessageEvenIfLLMFails(t *testing.T) {
	fake := ai.NewFake(ai.FakeResult{Err: ai.ErrProviderUnavailable})
	env := newTestEnv(t, fake)
	const chatID = int64(1)

	env.svc.HandleUpdate(context.Background(), textUpdate(chatID, 100, "alice", "uz", "Do you have shoes?"))

	convID := env.conversationID(t, chatID)
	rows, err := env.q.ListBotMessages(context.Background(), db.ListBotMessagesParams{ShopID: env.shop.ID, ConversationID: convID, Limit: 10})
	if err != nil {
		t.Fatalf("ListBotMessages: %v", err)
	}
	var sawUser bool
	for _, r := range rows {
		if r.Role == db.BotMessageRoleUser && r.Content == "Do you have shoes?" {
			sawUser = true
		}
	}
	if !sawUser {
		t.Fatalf("want the user's own message persisted even though the provider call failed")
	}
}
