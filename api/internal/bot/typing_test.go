package bot_test

import (
	"context"
	"testing"
	"time"

	"github.com/Sulton-Ali/savdo/api/internal/ai"
)

// delayedClient wraps inner and sleeps delay before every Chat call
// returns — chat.go's own scriptedAnswer-shaped results still apply,
// this only adds latency so startTyping's own ticker (chat.go) gets time
// to re-send the typing action more than once before the turn finishes.
type delayedClient struct {
	inner ai.Client
	delay time.Duration
}

func (d delayedClient) Chat(ctx context.Context, req ai.Request) (ai.Response, error) {
	select {
	case <-time.After(d.delay):
	case <-ctx.Done():
		return ai.Response{}, ctx.Err()
	}
	return d.inner.Chat(ctx, req)
}

// TestHandleUpdate_freeText_sendsTypingBeforeReply pins O-30: a free-text
// turn shows Telegram's typing action at least once before sendAnswer's
// own reply goes out — startTyping's own doc comment (chat.go) explains
// why the first send is synchronous, so this ordering never races.
func TestHandleUpdate_freeText_sendsTypingBeforeReply(t *testing.T) {
	fake := ai.NewFake(scriptedAnswer("We have shoes in stock."))
	env := newTestEnv(t, fake)

	env.svc.HandleUpdate(context.Background(), textUpdate(1, 100, "alice", "uz", "Do you have shoes?"))

	if got := env.sender.typingCount(); got < 1 {
		t.Fatalf("typing actions sent = %d, want at least 1 for a free-text turn", got)
	}
	if len(env.sender.Messages) == 0 {
		t.Fatalf("want a reply sent")
	}
}

// TestHandleUpdate_slashCommand_sendsNoTyping pins O-30's other half: a
// slash command never calls startTyping — handleCommand's own path
// (update.go) never reaches it — and answers instantly, with no typing
// action at all. Scripting zero ai.Fake results also proves the command
// never reached the model.
func TestHandleUpdate_slashCommand_sendsNoTyping(t *testing.T) {
	env := newTestEnv(t, ai.NewFake())

	env.svc.HandleUpdate(context.Background(), textUpdate(1, 100, "alice", "uz", "/start"))

	if got := env.sender.typingCount(); got != 0 {
		t.Fatalf("typing actions sent = %d, want 0 for a slash command", got)
	}
}

// TestHandleUpdate_rateLimited_sendsNoTyping pins O-30's gate ordering:
// a turn O-25's per-chat rate limit rejects returns the static reply
// before handleFreeText ever calls startTyping — mirrors
// TestHandleUpdate_perChatRateLimit (limits_test.go) but asserts the
// typing side effect instead of the reply text.
func TestHandleUpdate_rateLimited_sendsNoTyping(t *testing.T) {
	env := newTestEnv(t, ai.NewFake())
	ctx := context.Background()
	const chatID = int64(1)

	env.svc.HandleUpdate(ctx, textUpdate(chatID, 100, "alice", "uz", "/start"))
	convID := env.conversationID(t, chatID)
	for i := 0; i < 20; i++ {
		env.insertLLMMessage(t, convID, 10, 10)
	}

	env.svc.HandleUpdate(ctx, textUpdate(chatID, 100, "alice", "uz", "Do you have shoes?"))

	if got := env.sender.typingCount(); got != 0 {
		t.Fatalf("typing actions sent = %d, want 0 for a rate-limited turn", got)
	}
}

// TestHandleUpdate_slowModel_sendsAtLeastTwoTypingActions pins the loop
// itself: a model call slower than one typing interval gets re-sent
// typing actions, not just the initial one. The interval is shrunk via
// Config.TypingInterval (rebuildServiceWithTypingInterval) rather than
// waiting out the real 4s default, keeping this test fast.
func TestHandleUpdate_slowModel_sendsAtLeastTwoTypingActions(t *testing.T) {
	fake := ai.NewFake(scriptedAnswer("We have shoes in stock."))
	env := newTestEnv(t, ai.NewFake()) // rebuilt below with the slow client
	env.rebuildServiceWithTypingInterval(delayedClient{inner: fake, delay: 300 * time.Millisecond}, 80*time.Millisecond)

	env.svc.HandleUpdate(context.Background(), textUpdate(1, 100, "alice", "uz", "Do you have shoes?"))

	if got := env.sender.typingCount(); got < 2 {
		t.Fatalf("typing actions sent = %d, want at least 2 (300ms call, 80ms interval)", got)
	}
}

// TestHandleUpdate_typingStopsAfterReply pins the loop's own stop point
// (update.go's handleFreeText, chat.go's startTyping doc comment): the
// turn calls stopTyping explicitly right after runFreeText returns, so
// no typing action is ever recorded at or after the reply — proven here
// from the fake sender's own call-order log (Events), not just a count —
// and the deferred stopTyping (the safety net for a panic/shutdown path)
// never re-fires a stale action either, proven by waiting several would-
// be intervals past the reply and checking the count never grew.
func TestHandleUpdate_typingStopsAfterReply(t *testing.T) {
	fake := ai.NewFake(scriptedAnswer("We have shoes in stock."))
	env := newTestEnv(t, ai.NewFake()) // rebuilt below with the short interval
	env.rebuildServiceWithTypingInterval(fake, 20*time.Millisecond)

	env.svc.HandleUpdate(context.Background(), textUpdate(1, 100, "alice", "uz", "Do you have shoes?"))

	events := env.sender.events()
	replyIdx := -1
	for i, e := range events {
		if e == "message" || e == "photo" {
			replyIdx = i
			break
		}
	}
	if replyIdx == -1 {
		t.Fatalf("events = %v, want at least one reply (message or photo)", events)
	}
	var sawTypingBeforeReply bool
	for i, e := range events {
		if e != "typing" {
			continue
		}
		if i >= replyIdx {
			t.Fatalf("events = %v, want no typing action at or after index %d (the reply) — handleFreeText's explicit stopTyping must run before the reply is sent", events, replyIdx)
		}
		sawTypingBeforeReply = true
	}
	if !sawTypingBeforeReply {
		t.Fatalf("events = %v, want at least one typing action before the reply", events)
	}

	before := env.sender.typingCount()
	time.Sleep(150 * time.Millisecond) // several would-be 20ms intervals
	if after := env.sender.typingCount(); after != before {
		t.Fatalf("typing actions after HandleUpdate returned = %d, want unchanged from %d (the loop must stay stopped)", after, before)
	}
}
