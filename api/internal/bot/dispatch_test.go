package bot_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/internal/bot"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// updateWithID is updateInChat with an explicit update_id — Dispatch's
// own de-dup set (item 8) keys on it; textUpdate/updateInChat leave it
// at the zero value, fine for every test that calls HandleUpdate
// directly (bypassing Dispatch) but not for these.
func updateWithID(id int, chatID, telegramUserID int64, username, langCode, text string) *models.Update {
	u := updateInChat(chatID, telegramUserID, username, langCode, text, models.ChatTypePrivate)
	u.ID = int64(id)
	return u
}

// waitForConversation polls (up to 2s) until chatID's conversation
// exists — Dispatch runs HandleUpdate on its own background goroutine
// (dispatch.go), so a test can never assert on its effects immediately
// after calling Dispatch the way it can after calling HandleUpdate
// directly.
func waitForConversation(t *testing.T, env *testEnv, chatID int64) uuid.UUID {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conv, err := env.q.GetBotConversationByChat(context.Background(), db.GetBotConversationByChatParams{ShopID: env.shop.ID, TelegramChatID: chatID})
		if err == nil {
			return conv.ID
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("conversation for chat %d never appeared within the poll window", chatID)
	return uuid.UUID{}
}

// waitForMessageCount polls (up to 2s) until convID has at least want
// bot_messages rows.
func waitForMessageCount(t *testing.T, env *testEnv, convID uuid.UUID, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var got int
	for time.Now().Before(deadline) {
		rows, err := env.q.ListBotMessages(context.Background(), db.ListBotMessagesParams{ShopID: env.shop.ID, ConversationID: convID, Limit: 1000})
		if err == nil {
			got = len(rows)
			if got >= want {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("bot_messages for conversation %s has %d rows, want at least %d within the poll window", convID, got, want)
}

// TestDispatch_dedupByUpdateID pins item 8: the same update_id posted
// twice results in exactly one HandleUpdate — the second Dispatch call
// is dropped by the de-dup set before it ever spawns a goroutine, so it
// never even races the first one to decide who "wins"; it simply never
// runs at all.
func TestDispatch_dedupByUpdateID(t *testing.T) {
	env := newTestEnv(t, noChatClient())
	const chatID = int64(50)

	env.svc.Dispatch(updateWithID(1, chatID, 500, "alice", "uz", "/start"))
	env.svc.Dispatch(updateWithID(1, chatID, 500, "alice", "uz", "/start")) // same update_id: must be dropped

	convID := waitForConversation(t, env, chatID)
	waitForMessageCount(t, env, convID, 2) // one user row + one assistant row from the single processed update

	time.Sleep(150 * time.Millisecond) // give a wrongly-not-deduped second run a chance to land
	rows, err := env.q.ListBotMessages(context.Background(), db.ListBotMessagesParams{ShopID: env.shop.ID, ConversationID: convID, Limit: 1000})
	if err != nil {
		t.Fatalf("ListBotMessages: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("bot_messages = %d rows, want exactly 2 (one HandleUpdate call, not two)", len(rows))
	}
}

// panicSender panics on every SendMessage call — item 9's own scenario:
// a handler panic (anywhere in the call chain triggered by replying)
// must never propagate out of Dispatch's own goroutine.
type panicSender struct{}

func (panicSender) SendMessage(context.Context, int64, string) error {
	panic("simulated handler panic")
}
func (panicSender) SendPhoto(context.Context, int64, string, string) error {
	panic("simulated handler panic")
}

// TestDispatch_panicRecovered_perChatLockStillReleased pins item 9's own
// two guarantees together: a panic inside one dispatched update never
// crashes the process (this test function returning normally — instead
// of the whole `go test` binary aborting, which an unrecovered panic in
// a goroutine would cause — is itself the proof), and the per-chat lock
// a panicking update held is still released afterward, so a later update
// for the *same* chat is not left permanently stuck waiting on it.
func TestDispatch_panicRecovered_perChatLockStillReleased(t *testing.T) {
	env := newTestEnv(t, noChatClient())
	svc := bot.NewService(env.pool, env.q, noChatClient(), env.pub, env.content, panicSender{}, nil, env.cfg, env.clock.now, testLogger())
	const chatID = int64(51)

	svc.Dispatch(updateWithID(1, chatID, 510, "alice", "uz", "/start"))
	// Give the panicking goroutine time to run (and recover) before
	// dispatching the next update for the same chat.
	time.Sleep(150 * time.Millisecond)

	// A second update for the same chat must still be processed — proof
	// the per-chat lock was released, not left held by the goroutine that
	// panicked.
	svc.Dispatch(updateWithID(2, chatID, 510, "alice", "uz", "/hours"))

	convID := waitForConversation(t, env, chatID)
	// Both turns fully persist (user row + assistant row each) before
	// their own send panics (replyStaticText's own persist-then-send
	// order, commands.go) — 4 rows total is proof the *second* update
	// actually ran too, not just that the first one's panic was recovered.
	waitForMessageCount(t, env, convID, 4)
}

// concurrencySender counts how many SendMessage calls are in flight at
// once (an atomic counter, sampled at its own peak) — item 9's own
// per-chat-serialization test uses this to observe that two updates for
// the *same* chat never overlap.
type concurrencySender struct {
	inFlight int32
	peak     int32
}

func (s *concurrencySender) SendMessage(context.Context, int64, string) error {
	n := atomic.AddInt32(&s.inFlight, 1)
	for {
		p := atomic.LoadInt32(&s.peak)
		if n <= p || atomic.CompareAndSwapInt32(&s.peak, p, n) {
			break
		}
	}
	time.Sleep(50 * time.Millisecond) // hold the "in flight" window open long enough for a race to show up
	atomic.AddInt32(&s.inFlight, -1)
	return nil
}
func (s *concurrencySender) SendPhoto(context.Context, int64, string, string) error { return nil }

// TestDispatch_sameChatSerialized pins item 9's own per-chat
// serialization: two updates dispatched at once for the *same* chat
// never run their reply concurrently — chatLockTable (dispatch.go)
// blocks the second until the first fully finishes, so O-25's gates are
// always evaluated one turn at a time per chat, never read-then-act
// racily against a concurrent turn for the same chat.
func TestDispatch_sameChatSerialized(t *testing.T) {
	env := newTestEnv(t, noChatClient())
	sender := &concurrencySender{}
	svc := bot.NewService(env.pool, env.q, noChatClient(), env.pub, env.content, sender, nil, env.cfg, env.clock.now, testLogger())
	const chatID = int64(52)

	svc.Dispatch(updateWithID(1, chatID, 520, "alice", "uz", "/hours"))
	svc.Dispatch(updateWithID(2, chatID, 520, "alice", "uz", "/address"))

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&sender.peak) > 0 && atomic.LoadInt32(&sender.inFlight) == 0 {
			break // both sends have happened and finished
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&sender.peak); got != 1 {
		t.Fatalf("peak concurrent SendMessage calls for one chat = %d, want 1 (updates for the same chat must never overlap)", got)
	}
}
