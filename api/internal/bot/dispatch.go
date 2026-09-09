package bot

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-telegram/bot/models"
)

// maxInFlightUpdates bounds how many updates HandleUpdate may run at
// once, process-wide (item 9 — go-telegram/bot's own default dispatch is
// one bare `go r(ctx, b, upd)` per update, github.com/go-telegram/
// bot@v1.25.0/process_update.go, with neither a cap nor a recover;
// cmd/bot/main.go disables that with telegram.WithNotAsyncHandlers() and
// calls Dispatch instead, which provides both). 8 headroom over
// chatCallTimeout(45s)*maxToolRounds(5) worst case per update: generous
// enough for a small single shop's real traffic (D-02) without letting a
// burst spin up unbounded concurrent LLM calls.
const maxInFlightUpdates = 8

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
// goroutine that calls HandleUpdate with a background context — never
// the caller's own ctx, which a webhook request's is canceled the moment
// HandleBotWebhook acknowledges 200, long before a multi-round tool loop
// can finish (item 8; chat.go's own chatCallTimeout*maxToolRounds bounds
// how long that background work can run instead). Both cmd/bot's polling
// loop (telegram.WithNotAsyncHandlers, so the go-telegram/bot library
// itself never spawns its own uncontrolled goroutine) and
// httpx.HandleBotWebhook call this, never HandleUpdate directly, so both
// transports share one concurrency budget, one panic boundary and one
// update_id de-dup set. Returns immediately.
//
// De-duplication (item 8) is process-local only, as the const doc
// comments above note: a future multi-process deployment of this same
// binary (not this one — ADR-004 is tenant-ready, not multi-process-per-
// shop yet) would need a shared store instead of this in-memory set.
func (s *Service) Dispatch(update *models.Update) {
	if update == nil {
		return
	}
	if update.ID != 0 && s.seen.markSeen(update.ID) {
		return // already dispatched (or a Telegram retry of one still running)
	}
	go s.dispatchOne(update)
}

// dispatchOne is Dispatch's own goroutine body: acquire the global slot,
// then the per-chat lock, then run — each release deferred immediately
// after its acquire succeeds, and the panic recovery deferred last (so
// it runs *first* on unwind, catching a panic from HandleUpdate before
// either lock is released) — never logging the panic value itself,
// which could echo back a fragment of the update's own text (hard rule
// 9's own "no secret, token... in logs" extends to arbitrary customer
// input the same way logProviderError's own doc comment reasons about
// provider error text).
func (s *Service) dispatchOne(update *models.Update) {
	s.sem <- struct{}{}
	defer func() { <-s.sem }()

	chatID := updateChatID(update)
	if chatID != 0 {
		s.chatLocks.lock(chatID)
		defer s.chatLocks.unlock(chatID)
	}

	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("bot: panic handling update", "panic_class", fmt.Sprintf("%T", r))
		}
	}()

	s.HandleUpdate(context.Background(), update)
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

// chatLockTable is a bounded map of per-chat mutexes (item 9: "a mutex
// keyed by chat id, bounded map"): an entry exists only while at least
// one goroutine currently holds or is waiting for that chat's lock, and
// is deleted the moment the last one releases it — refs tracks that
// count under table's own mutex. Because lock() is only ever called
// after dispatchOne has already acquired one of Service.sem's
// maxInFlightUpdates slots, at most that many goroutines can ever be
// past the semaphore at once, so the table can never hold more than
// maxInFlightUpdates entries either — bounded by construction, no
// separate eviction policy needed.
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
