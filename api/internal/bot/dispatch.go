package bot

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-telegram/bot/models"
)

// maxInFlightUpdates bounds how many updates may actually be *running*
// HandleUpdate at once, process-wide (item 9 — go-telegram/bot's own
// default dispatch is one bare `go r(ctx, b, upd)` per update, github.com/
// go-telegram/bot@v1.25.0/process_update.go, with neither a cap nor a
// recover; cmd/bot/main.go disables that with telegram.WithNotAsyncHandlers()
// and calls Dispatch instead, which provides both). 8 headroom over
// chatCallTimeout(45s)*maxToolRounds(5) worst case per update: generous
// enough for a small single shop's real traffic (D-02) without letting a
// burst spin up unbounded concurrent LLM calls.
const maxInFlightUpdates = 8

// maxQueuedUpdates bounds how many dispatchOne goroutines may exist at
// once — running, or merely *waiting* on their own chat's lock or on
// Service.sem (MINOR 2). Dispatch itself must never block its own caller
// (item 8: the webhook handler acknowledges 200 and returns immediately),
// so a Dispatch call past this limit is dropped (logged, not queued)
// rather than piling up an unbounded number of parked goroutines — a
// burst from one very chatty chat, or a Telegram outage-then-replay
// dumping a large backlog at once, would otherwise grow this without
// bound.
const maxQueuedUpdates = 64

// maxSeenUpdateIDs bounds seenUpdates' own de-duplication window (item
// 8): Telegram redelivers a webhook update it did not get a fast 200
// for, carrying the same update_id — a plain FIFO of the last N ids seen
// is enough to catch a retry that arrives while the first delivery is
// still (or was recently) processing; older ids age out because nothing
// this system does needs to remember a delivery from longer ago than a
// handful of in-flight/recently-finished updates.
const maxSeenUpdateIDs = 10_000

// Dispatch schedules update for Service's shared async worker: a panic-
// recovered, globally-bounded (maxInFlightUpdates), per-chat-serialized
// goroutine that calls HandleUpdate under Service's own baseCtx — never
// the caller's own ctx, which a webhook request's is canceled the moment
// HandleBotWebhook acknowledges 200, long before a multi-round tool loop
// can finish (item 8; chat.go's own chatCallTimeout*maxToolRounds bounds
// how long that background work can run instead; baseCtx itself is only
// ever canceled by Close, MAJOR 2). Both cmd/bot's polling loop
// (telegram.WithNotAsyncHandlers, so the go-telegram/bot library itself
// never spawns its own uncontrolled goroutine) and httpx.HandleBotWebhook
// call this, never HandleUpdate directly, so both transports share one
// concurrency budget, one panic boundary and one update_id de-dup set.
// Rejects (logs a class, drops the update) once Close has begun
// (MAJOR 2) or once maxQueuedUpdates goroutines already exist
// (MINOR 2). Always returns immediately.
//
// De-duplication (item 8) is process-local only, as the const doc
// comments above note: a future multi-process deployment of this same
// binary (not this one — ADR-004 is tenant-ready, not multi-process-per-
// shop yet) would need a shared store instead of this in-memory set.
func (s *Service) Dispatch(update *models.Update) {
	if update == nil {
		return
	}

	s.shutdownMu.Lock()
	if s.shuttingDown {
		s.shutdownMu.Unlock()
		s.logger.Warn("bot: dispatch rejected: service is shutting down")
		return
	}
	// minor m1: the queue slot is acquired *before* markSeen, not after
	// — the other order marked a queue-full-dropped update seen anyway,
	// so Telegram's own retry of that exact update_id (once a slot had
	// freed up and it could actually have been processed) was discarded
	// forever instead of getting a real second chance. An update that
	// never got a slot is never marked seen at all.
	select {
	case s.queueSlots <- struct{}{}:
	default:
		s.shutdownMu.Unlock()
		s.logger.Warn("bot: dispatch dropped: queue is full", "queue_limit", maxQueuedUpdates)
		return
	}
	if update.ID != 0 && s.seen.markSeen(update.ID) {
		<-s.queueSlots // never spawned: give the slot back
		s.shutdownMu.Unlock()
		return // already dispatched (or a Telegram retry of one still running)
	}
	// wg.Add must happen inside the same locked region Close's own
	// shuttingDown flip uses (service.go's own doc comment on
	// shutdownMu): otherwise Close could observe wg's count reach zero
	// and return *before* this Add is visible, letting Close hand the
	// database pool back to its caller while this goroutine is still
	// about to start real work.
	s.wg.Add(1)
	s.shutdownMu.Unlock()

	go func() {
		defer s.wg.Done()
		defer func() { <-s.queueSlots }()
		s.dispatchOne(update)
	}()
}

// dispatchOne is Dispatch's own goroutine body: the per-chat lock is
// acquired *before* the global semaphore (MINOR 1) — the other order
// (semaphore first) lets a single chatty chat's own backlog occupy every
// one of Service.sem's maxInFlightUpdates slots while each of its own
// goroutines just sits blocked on the *same* per-chat mutex one after
// the other, starving every other chat's update out of a slot it could
// otherwise have used immediately. Locking the chat first means only one
// goroutine per chat is ever competing for a semaphore slot at a time;
// the rest of that chat's own backlog blocks on the chat lock itself,
// never touching the semaphore at all. Each release is deferred
// immediately after its acquire succeeds, and the panic recovery is
// deferred last (so it runs *first* on unwind, catching a panic from
// HandleUpdate before either lock is released) — never logging the
// panic value itself, which could echo back a fragment of the update's
// own text (hard rule 9's own "no secret, token... in logs" extends to
// arbitrary customer input the same way logProviderError's own doc
// comment reasons about provider error text).
func (s *Service) dispatchOne(update *models.Update) {
	chatID := updateChatID(update)
	if chatID != 0 {
		s.chatLocks.lock(chatID)
		defer s.chatLocks.unlock(chatID)
	}

	s.sem <- struct{}{}
	defer func() { <-s.sem }()

	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("bot: panic handling update", "panic_class", fmt.Sprintf("%T", r))
		}
	}()

	s.HandleUpdate(s.baseCtx, update)
}

// updateChatID reads the chat id HandleUpdate itself would key its own
// per-chat state on, without running any of HandleUpdate's own
// validation — 0 (models.Chat's own zero value, never a real Telegram
// chat id) means "no chat to serialize on", the same update HandleUpdate
// itself ignores.
func updateChatID(update *models.Update) int64 {
	if update == nil || update.Message == nil {
		return 0
	}
	return update.Message.Chat.ID
}

// chatLockTable is a map of per-chat mutexes (item 9: "a mutex keyed by
// chat id"): an entry exists only while at least one goroutine currently
// holds or is waiting for that chat's lock, and is deleted the moment
// the last one releases it — refs tracks that count under table's own
// mutex. Bounded by maxQueuedUpdates (dispatchOne is only ever reached
// from a goroutine Dispatch already admitted through queueSlots), not by
// maxInFlightUpdates: MINOR 1's own reordering means a chat lock can now
// be held (or waited on) *before* a goroutine ever reaches
// Service.sem.
type chatLockTable struct {
	mu    sync.Mutex
	locks map[int64]*chatLockEntry
}

type chatLockEntry struct {
	mu   sync.Mutex
	refs int
}

func newChatLockTable() *chatLockTable {
	return &chatLockTable{locks: make(map[int64]*chatLockEntry)}
}

// lock serializes every dispatchOne call for the same chatID: a second
// update for a chat already being processed blocks here until the first
// finishes, so O-25's rate-limit/budget gates are always checked one
// turn at a time per chat, never read-then-act racily against a
// concurrent turn for the same chat (item 9's own "limits are evaluated
// one turn at a time per chat").
func (t *chatLockTable) lock(chatID int64) {
	t.mu.Lock()
	e, ok := t.locks[chatID]
	if !ok {
		e = &chatLockEntry{}
		t.locks[chatID] = e
	}
	e.refs++
	t.mu.Unlock()

	e.mu.Lock()
}

func (t *chatLockTable) unlock(chatID int64) {
	t.mu.Lock()
	e := t.locks[chatID]
	e.refs--
	if e.refs == 0 {
		delete(t.locks, chatID)
	}
	t.mu.Unlock()

	e.mu.Unlock()
}

// seenUpdates is Dispatch's own bounded update_id de-dup set (item 8) —
// a plain FIFO of the last max ids, not an LRU: recency of *arrival* is
// all that matters here, not recency of access.
type seenUpdates struct {
	mu    sync.Mutex
	set   map[int64]struct{}
	order []int64
	max   int
}

func newSeenUpdates(maxIDs int) *seenUpdates {
	return &seenUpdates{set: make(map[int64]struct{}, maxIDs), max: maxIDs}
}

// markSeen reports whether id was already recorded, recording it either
// way — so the very first caller for a given id gets false (proceed) and
// every later one (a Telegram retry, or a duplicate Dispatch call) gets
// true (skip), without a separate "check" step that could race between
// two goroutines calling Dispatch for the same id at once.
func (s *seenUpdates) markSeen(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.set[id]; ok {
		return true
	}
	s.set[id] = struct{}{}
	s.order = append(s.order, id)
	if len(s.order) > s.max {
		oldest := s.order[0]
		s.order = s.order[1:]
		delete(s.set, oldest)
	}
	return false
}

// Close begins Service's own shutdown (MAJOR 2): every Dispatch call
// from this point on is rejected — logged, never queued — and Close
// blocks until every already-accepted dispatchOne goroutine finishes or
// ctx is done, whichever comes first. Callers (cmd/bot/main.go, cmd/api/
// main.go) call this, bounded by a shutdown timeout, before closing the
// database pool: without it, a turn caught between its own already-
// billed LLM call and persisting the result loses that write the moment
// the pool closes out from under it — CRITICAL 1's own under-counting
// gap, reopened at shutdown. If ctx's own deadline is reached first,
// Close force-cancels baseCtx (every dispatchOne goroutine's own
// context, service.go) as a last resort so a wedged turn cannot hold the
// process open forever, and returns ctx.Err().
func (s *Service) Close(ctx context.Context) error {
	s.shutdownMu.Lock()
	s.shuttingDown = true
	s.shutdownMu.Unlock()

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.baseCancel()
		return nil
	case <-ctx.Done():
		s.baseCancel()
		return ctx.Err()
	}
}
