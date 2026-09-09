package bot_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/internal/ai"
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
func (panicSender) SendTyping(context.Context, int64) error {
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
func (s *concurrencySender) SendTyping(context.Context, int64) error                { return nil }

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

// blockingAIClient blocks Chat until release is closed — MAJOR 2's own
// "Close waits for an in-flight turn" test needs a turn it can hold
// open on demand. started is closed exactly once, the first time Chat is
// entered, so the test can wait until the turn has actually reached the
// (simulated) LLM call before it starts asserting anything about Close.
type blockingAIClient struct {
	release     chan struct{}
	started     chan struct{}
	startedOnce sync.Once
}

func (c *blockingAIClient) Chat(ctx context.Context, _ ai.Request) (ai.Response, error) {
	c.startedOnce.Do(func() { close(c.started) })
	select {
	case <-c.release:
	case <-ctx.Done():
		return ai.Response{}, ctx.Err()
	}
	return ai.Response{
		Text: "Yes, in stock.", StopReason: "end_turn",
		Usage: ai.Usage{Provider: "anthropic", Model: "claude-sonnet-5", InputTokens: 5, OutputTokens: 5},
	}, nil
}

// TestServiceClose_waitsForInFlightTurn pins MAJOR 2: Close blocks until
// an already-dispatched, still-running turn finishes — never returning
// early just because its own shutdown deadline has not yet been
// reached, which would let a caller (cmd/bot/main.go, cmd/api/main.go)
// close the database pool out from under a turn that is only waiting to
// persist an already-billed LLM call.
func TestServiceClose_waitsForInFlightTurn(t *testing.T) {
	env := newTestEnv(t, nil)
	client := &blockingAIClient{release: make(chan struct{}), started: make(chan struct{})}
	svc := bot.NewService(env.pool, env.q, client, env.pub, env.content, env.sender, nil, env.cfg, env.clock.now, testLogger())

	svc.Dispatch(updateWithID(1, 70, 700, "alice", "uz", "Do you have shoes?"))
	select {
	case <-client.started:
	case <-time.After(2 * time.Second):
		t.Fatalf("the dispatched turn never reached Chat")
	}

	closeErr := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		closeErr <- svc.Close(ctx)
	}()

	select {
	case <-closeErr:
		t.Fatalf("Close returned before the in-flight turn finished")
	case <-time.After(150 * time.Millisecond):
		// still blocked, as expected
	}

	close(client.release) // let the turn finish

	select {
	case err := <-closeErr:
		if err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Close did not return after the in-flight turn finished")
	}
}

// TestDispatch_afterClose_rejected pins MAJOR 2's other half: once Close
// has begun, a later Dispatch call is rejected outright — no new
// conversation/message row is ever created for it.
func TestDispatch_afterClose_rejected(t *testing.T) {
	env := newTestEnv(t, noChatClient())
	svc := bot.NewService(env.pool, env.q, noChatClient(), env.pub, env.content, env.sender, nil, env.cfg, env.clock.now, testLogger())

	if err := svc.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	const chatID = int64(71)
	svc.Dispatch(updateWithID(1, chatID, 710, "alice", "uz", "/start"))

	time.Sleep(150 * time.Millisecond) // give a wrongly-accepted dispatch a chance to run
	if _, err := env.q.GetBotConversationByChat(context.Background(), db.GetBotConversationByChatParams{ShopID: env.shop.ID, TelegramChatID: chatID}); err == nil {
		t.Fatalf("want no conversation created — Dispatch after Close must be rejected")
	}
}

// delaySender sleeps delay on every SendMessage/SendPhoto call — MINOR
// 1's own "one chat's backlog must not starve another chat" test uses
// this to keep a chat's own dispatchOne goroutines busy long enough to
// observe whether a second chat's update has to wait behind them.
type delaySender struct{ delay time.Duration }

func (d delaySender) SendMessage(context.Context, int64, string) error {
	time.Sleep(d.delay)
	return nil
}
func (d delaySender) SendPhoto(context.Context, int64, string, string) error {
	time.Sleep(d.delay)
	return nil
}
func (d delaySender) SendTyping(context.Context, int64) error {
	time.Sleep(d.delay)
	return nil
}

// TestDispatch_oneChatBacklogDoesNotStarveAnotherChat pins MINOR 1: the
// per-chat lock is acquired *before* Service.sem (dispatch.go), so a
// single chat's own backlog of updates can occupy at most one of
// maxInFlightUpdates' global slots at a time — the rest block on that
// chat's own mutex, never touching the semaphore — leaving the other
// slots free for a second chat's update to run immediately instead of
// queueing behind the first chat's entire backlog. chatA sends more
// than maxInFlightUpdates (8) updates on purpose: with the semaphore
// acquired first (the bug this pins), all 8 slots fill with chatA's own
// goroutines immediately and chatB's own update queues behind chatA's
// *entire* backlog draining one turn at a time; with the chat lock
// acquired first (the fix), only one of chatA's goroutines ever touches
// the semaphore at a time, so chatB's own update always finds a free
// slot immediately regardless of how deep chatA's backlog is.
func TestDispatch_oneChatBacklogDoesNotStarveAnotherChat(t *testing.T) {
	env := newTestEnv(t, noChatClient())
	const perSendDelay = 150 * time.Millisecond
	svc := bot.NewService(env.pool, env.q, noChatClient(), env.pub, env.content, delaySender{delay: perSendDelay}, nil, env.cfg, env.clock.now, testLogger())

	const chatA = int64(80)
	const chatABacklog = 12 // > maxInFlightUpdates (8), see doc comment above
	for i := 0; i < chatABacklog; i++ {
		svc.Dispatch(updateWithID(100+i, chatA, 800+int64(i), "alice", "uz", "/hours"))
	}
	// Give chat A's own backlog a moment to actually start occupying
	// dispatchOne goroutines before chat B's update is dispatched.
	time.Sleep(30 * time.Millisecond)

	const chatB = int64(81)
	start := time.Now()
	svc.Dispatch(updateWithID(200, chatB, 810, "bob", "uz", "/address"))
	waitForConversation(t, env, chatB)
	elapsed := time.Since(start)

	// A free slot regardless of ordering must exist within roughly one
	// send's worth of time (the first chatA turn to finish); the bug this
	// pins instead makes chatB wait for several chatA turns to drain
	// (multiple times perSendDelay) before a slot ever reaches it.
	if elapsed > 3*perSendDelay {
		t.Fatalf("chat B's update took %s, want well under chat A's own %d-deep backlog — it must run on its own free semaphore slot, not queue behind chat A's entire backlog", elapsed, chatABacklog)
	}
}

// gatedSender blocks every SendMessage/SendPhoto call on release, closed
// exactly once by the test that owns it — item m1's own determinism fix:
// a fixed delay races the test's own setup (how many of a backlog have
// actually been *dispatched*, not just how much wall time has passed);
// this makes "hold every slot open until I say so" exact instead of
// probabilistic.
type gatedSender struct{ release chan struct{} }

func (g gatedSender) SendMessage(ctx context.Context, _ int64, _ string) error {
	select {
	case <-g.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}
func (g gatedSender) SendPhoto(ctx context.Context, _ int64, _, _ string) error {
	select {
	case <-g.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}
func (g gatedSender) SendTyping(ctx context.Context, _ int64) error {
	select {
	case <-g.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// TestDispatch_queueFullDropIsNotPermanentlyMarkedSeen pins minor m1: an
// update dropped because the queue was full must not be recorded in
// seenUpdates — a genuine Telegram retry of that same update_id, arriving
// once a slot has freed up, must still get a real chance to run instead
// of being discarded forever as "already seen".
func TestDispatch_queueFullDropIsNotPermanentlyMarkedSeen(t *testing.T) {
	env := newTestEnv(t, noChatClient())
	sender := gatedSender{release: make(chan struct{})}
	svc := bot.NewService(env.pool, env.q, noChatClient(), env.pub, env.content, sender, nil, env.cfg, env.clock.now, testLogger())

	// Fill every one of maxQueuedUpdates' slots with one chat's own
	// backlog: each of these goroutines holds a queue slot for as long as
	// it is running *or* still waiting on the shared per-chat lock. Every
	// Dispatch call here is synchronous up through acquiring (or being
	// refused) its own queue slot — dispatchOne's own goroutine only
	// starts *after* that — so by the time this loop returns, all
	// maxQueuedUpdates(64) slots are deterministically occupied: no sleep,
	// no race, nothing here depends on wall-clock timing at all. None of
	// them can finish and free a slot back up until this test closes
	// sender.release below, since sender.SendMessage blocks on it and
	// every one of these updates ends in a reply send.
	const chatA = int64(85)
	const backlog = 64
	for i := 0; i < backlog; i++ {
		svc.Dispatch(updateWithID(2000+i, chatA, 850000+int64(i), "x", "uz", "/hours"))
	}

	const dupID = 9999
	const probeChat = int64(86)
	svc.Dispatch(updateWithID(dupID, probeChat, 860000, "y", "uz", "/hours")) // must be dropped: queue full

	// The queue is deterministically full at this point (see the loop's
	// own comment above) — no polling needed to confirm the drop; check
	// once.
	if _, err := env.q.GetBotConversationByChat(context.Background(), db.GetBotConversationByChatParams{ShopID: env.shop.ID, TelegramChatID: probeChat}); err == nil {
		t.Fatalf("probe update was processed — queue was not actually full, this test's own setup is wrong")
	}

	// Let chatA's whole backlog run and finish, freeing every slot.
	close(sender.release)
	waitForConversation(t, env, chatA)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		convID, err := env.q.GetBotConversationByChat(context.Background(), db.GetBotConversationByChatParams{ShopID: env.shop.ID, TelegramChatID: chatA})
		if err == nil {
			rows, err := env.q.ListBotMessages(context.Background(), db.ListBotMessagesParams{ShopID: env.shop.ID, ConversationID: convID.ID, Limit: 1000})
			if err == nil && len(rows) >= backlog*2 { // user + assistant row per turn
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Redeliver the *same* update_id now that the queue has room — with
	// the fix, it was never marked seen, so this must actually run.
	svc.Dispatch(updateWithID(dupID, probeChat, 860000, "y", "uz", "/hours"))
	waitForConversation(t, env, probeChat)
}

// TestServiceClose_forceCancelsAfterDeadline pins minor m3: once ctx's
// own deadline passes, Close returns ctx.Err() immediately (never
// waiting the full duration of a still-blocked turn) having force-
// cancelled baseCtx — the blocked turn observes ctx.Done() instead of
// hanging forever.
func TestServiceClose_forceCancelsAfterDeadline(t *testing.T) {
	env := newTestEnv(t, nil)
	client := &blockingAIClient{release: make(chan struct{}), started: make(chan struct{})}
	svc := bot.NewService(env.pool, env.q, client, env.pub, env.content, env.sender, nil, env.cfg, env.clock.now, testLogger())

	svc.Dispatch(updateWithID(1, 91, 910, "alice", "uz", "Do you have shoes?"))
	select {
	case <-client.started:
	case <-time.After(2 * time.Second):
		t.Fatalf("the dispatched turn never reached Chat")
	}
	// Deliberately never closing client.release: the turn only ends via
	// baseCtx's own cancellation.

	closeCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := svc.Close(closeCtx)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close() error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > 1*time.Second {
		t.Fatalf("Close() took %s, want it to return promptly once its own deadline passed, not wait for the still-blocked turn", elapsed)
	}
}
