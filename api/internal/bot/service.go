// Package bot implements the "Bot question" flow (docs/03-ARCHITECTURE.md
// § Key flows) and the admin's read-only view of it (docs/05-API.md §
// Bot): a customer's Telegram message becomes an answer grounded only in
// the shop's own public-read data (ADR-009's "hard data boundary" —
// hard rule 10), rate-limited per chat and per shop (O-25), persisted to
// bot_conversations/bot_messages (docs/04-DATA-MODEL.md § 6) and visible
// to owner/manager through GET /bot/conversations[/{id}/messages].
//
// Service.HandleUpdate is transport-agnostic: cmd/bot's own long-polling
// loop and httpx.HandleBotWebhook (the same operation Phase 8 points a
// real Telegram webhook at) both call it with a decoded
// *models.Update — nothing here knows or cares which one is calling.
// Both should go through Dispatch (dispatch.go), not HandleUpdate
// directly: Dispatch is the shared, panic-recovered, concurrency- and
// per-chat-bounded, update_id-deduplicated worker (items 8/9); calling
// HandleUpdate directly is still supported (every existing test does)
// but skips all of that and runs synchronously in the caller's own
// goroutine.
//
// The data boundary is structural, not a prompt instruction: the three
// tools this package exposes to the model (tools.go) read only through
// internal/public.Handler (the same functions the public landing calls)
// and internal/content.Service.Resolve, whose response schemas
// (gen.PublicProductListItem, gen.ProductPublic, gen.VariantPublic, the
// O-19 content blocks) simply have no cost/quantity/staff/customer field
// to leak (ADR-010) — never internal/catalog, internal/stock,
// internal/crm or internal/sales (hard rule 10). Service itself holds a
// plain *db.Queries (q below), the same as every other module's own
// Service — the boundary is enforced by which functions the three tools
// call (tools.go's own doc comment) and pinned by this package's own
// O-27 boundary suite (boundary_test.go), never by q's own type: q could
// run any query in principle, the tools simply never ask it to.
package bot

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/internal/ai"
	"github.com/Sulton-Ali/savdo/api/internal/content"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/public"
)

const (
	// perChatLimit and perChatWindow are O-25's per-chat rolling-hour
	// limit: 20 messages that reach the LLM per hour. Checked before any
	// tool/LLM call; a limited turn gets a static reply instead
	// (stored with provider "static", which CountLLMMessagesSince's own
	// WHERE clause excludes from the count it enforces here).
	perChatLimit  = 20
	perChatWindow = time.Hour

	// promptWindowMessages and promptWindowDuration are O-26's prompt
	// context window: at most the last 20 messages of the last 24h are
	// replayed to the model as history.
	promptWindowMessages = 20
	promptWindowDuration = 24 * time.Hour

	// maxToolRounds is ADR-009's tool-loop bound ("Tool loop runs at most
	// 5 rounds").
	maxToolRounds = 5

	// defaultTypingInterval is chat.go's startTyping own re-send cadence
	// for Telegram's typing chat action (O-30): Telegram shows a chat
	// action for about 5 seconds (Bot API docs, sendChatAction), so
	// re-sending every 4 seconds keeps it visible without flooding.
	// Config.TypingInterval overrides this — only this package's own
	// tests do.
	defaultTypingInterval = 4 * time.Second
)

// Sender is the subset of the Telegram Bot API HandleUpdate needs to
// reply — narrow on purpose so tests script a fake instead of standing
// up a real *bot.Bot (D-112: "tests use a fake Telegram transport").
// sender.go's telegramSender adapts a real *bot.Bot to it for cmd/bot and
// cmd/api's webhook wiring.
type Sender interface {
	// SendMessage sends plain-text text to chatID. Reply formatting
	// (format.go) deliberately never asks for Telegram's MarkdownV2 parse
	// mode — see format.go's own doc comment for why plain text is the
	// safer of the two choices docs/00-DECISIONS.md D-119 allows.
	SendMessage(ctx context.Context, chatID int64, text string) error
	// SendPhoto sends the image at photoURL (an absolute URL,
	// format.go's absoluteMediaURL) to chatID with a plain-text caption
	// (D-116).
	SendPhoto(ctx context.Context, chatID int64, photoURL, caption string) error
	// SendTyping shows Telegram's "typing…" chat action for chatID
	// (O-30) — chat.go's startTyping calls this repeatedly while a
	// free-text turn's model call is in progress, never for a slash
	// command or a turn one of O-25's gates rejects (both answer
	// instantly, before startTyping is ever called).
	SendTyping(ctx context.Context, chatID int64) error
}

// TelegramLinker is the account-linking capability `/start link_<code>`
// redeems (docs/05-API.md § Auth: `POST /auth/telegram/link` mints the
// code, `GET/DELETE /auth/telegram/link` read/clear the result).
// *auth.Service satisfies this structurally — its own CompleteLink
// method (internal/auth/telegram.go) has the identical shape, and that
// package declares its own auth.TelegramLinker interface with the same
// one method for the same reason — cmd/bot/main.go and cmd/api/main.go
// both wire authSvc in as this. A nil linker (e.g. a test that never
// exercises `/start link_<code>`) still answers
// texts.startLinkUnavailable rather than panicking (commands.go's own
// nil check).
type TelegramLinker interface {
	CompleteLink(ctx context.Context, code string, telegramUserID int64, telegramUsername string) error
}

// Config is Service's static configuration, resolved once at startup by
// cmd/bot/main.go and cmd/api/main.go — never re-read from the
// environment by this package itself, the same discipline every other
// module's Service follows.
type Config struct {
	// ShopID is the one shop this Service answers for (single-shop MVP,
	// ADR-004) — resolved once at startup from PUBLIC_SHOP_SLUG, the same
	// shop internal/public's own Service reads through.
	ShopID uuid.UUID
	// SiteURL builds absolute landing links from slugs (D-115) and
	// absolute media URLs for D-116's product photo — never built by the
	// model, always by format.go from a tool result's slug.
	SiteURL string
	// PriceInputPerMTok and PriceOutputPerMTok are USD per million
	// tokens, decimal strings (ADR-007) — the same two values internal/
	// ai.Config prices a single Chat call with (internal/config.Config's
	// AIPriceInputPerMTok/AIPriceOutputPerMTok). chat.go needs its own
	// copy because a turn's real cost is the *sum* across every round
	// runFreeText's tool loop makes, and internal/ai's own cost formula
	// (unexported, ai/config.go) only ever prices one round at a time —
	// this package cannot import an unexported symbol from another one,
	// so turnCost (chat.go) mirrors that formula instead. An empty or
	// malformed value degrades to a "0.000000" cost estimate (logged
	// once at construction, never a startup failure): by construction
	// both cmd/bot/main.go and cmd/api/main.go already validate the same
	// two strings via ai.New before ever building a Config here, so a
	// parse failure at this point would mean this package's own copy of
	// that validation has drifted, not a real misconfiguration.
	PriceInputPerMTok  string
	PriceOutputPerMTok string
	// DailyTokenBudget is O-25/D-120's env-level daily token cap
	// (AI_DAILY_TOKEN_BUDGET, internal/config.Config's own
	// AIDailyTokenBudget field) — applied whenever shops.ai_daily_token_
	// budget is NULL (shopOverBudget, persist.go). A plain pass-through,
	// never re-defaulted here: config.Load() already turns an unset
	// AI_DAILY_TOKEN_BUDGET into 200000 via its own envDefault, so 0
	// reaching this field only ever means an operator (or a test)
	// explicitly asked for it — D-120's own fail-closed value, not "the
	// caller forgot to set this".
	DailyTokenBudget int
	// TypingInterval overrides how often chat.go's startTyping re-sends
	// Telegram's typing chat action (O-30) while a free-text turn's
	// model call is in progress. Zero — every production caller —
	// defaults to defaultTypingInterval; only this package's own tests
	// (chat_test.go) set a shorter value, to observe more than one
	// re-send without paying the real interval in wall-clock test time.
	TypingInterval time.Duration
}

// Service implements the "Bot question" flow end to end. Every method
// takes ctx first and resolves nothing from the environment itself
// (Config above, resolved once at construction, is the only
// configuration surface).
type Service struct {
	pool *pgxpool.Pool
	// q is a plain *db.Queries — it can run any query in the schema, the
	// same as every other module's own Service (item 17). The O-27 data
	// boundary is enforced by which functions the three tools (tools.go)
	// call with it — internal/public.Handler and internal/content.
	// Service.Resolve only, never internal/catalog/stock/crm/sales
	// directly — and pinned by this package's own boundary suite
	// (boundary_test.go), not by q's own type.
	q       *db.Queries
	ai      ai.Client
	pub     *public.Handler
	content *content.Service
	sender  Sender
	linker  TelegramLinker
	cfg     Config
	now     func() time.Time
	logger  *slog.Logger

	// priceIn/priceOut are Config.PriceInputPerMTok/PriceOutputPerMTok,
	// decimal-parsed once at construction (turnCost, chat.go) — ADR-007:
	// never a float.
	priceIn  decimal.Decimal
	priceOut decimal.Decimal

	// typingInterval is Config.TypingInterval, defaulted to
	// defaultTypingInterval when unset (parsePriceOrZero's own sibling
	// default, NewService below) — chat.go's startTyping own re-send
	// cadence.
	typingInterval time.Duration

	// dispatch.go's shared async worker state: sem bounds global
	// in-flight updates, chatLocks serializes per chat, seen de-
	// duplicates by update_id. All three are process-local (dispatch.go's
	// own doc comment) and only ever touched through Dispatch/
	// dispatchOne — HandleUpdate itself remains synchronous and knows
	// nothing about any of this, which is what every existing test calling
	// it directly still relies on.
	sem        chan struct{}
	queueSlots chan struct{} // bounds maxQueuedUpdates (MINOR 2), acquired in Dispatch before spawning a goroutine
	chatLocks  *chatLockTable
	seen       *seenUpdates

	// baseCtx is the context every dispatchOne goroutine runs
	// HandleUpdate under — never context.Background() directly, so Close
	// (dispatch.go) can force-cancel every still-running turn if its own
	// shutdown deadline is reached (MAJOR 2). baseCancel is called
	// exactly once, by Close.
	baseCtx    context.Context
	baseCancel context.CancelFunc

	// shutdownMu guards shuttingDown and wg.Add together as one atomic
	// step (dispatch.go's own Dispatch/Close doc comments): Close must
	// never observe wg's count reach zero while a Dispatch call that
	// already decided to proceed has not yet called wg.Add.
	shutdownMu   sync.Mutex
	shuttingDown bool
	wg           sync.WaitGroup
}

// NewService builds a Service. now and logger may be nil (time.Now and
// slog.Default respectively) — tests pass a fixed clock so rate-limit and
// budget window assertions never race real wall-clock time. sender may
// also be nil (an unconfigured TELEGRAM_BOT_TOKEN, cmd/api/main.go's own
// doc comment): it is replaced with a nilSender (sender.go) that turns
// every send attempt into a clear, logged error instead of a nil-
// interface panic (item 10's own guard).
func NewService(pool *pgxpool.Pool, q *db.Queries, aiClient ai.Client, pub *public.Handler, contentSvc *content.Service, sender Sender, linker TelegramLinker, cfg Config, now func() time.Time, logger *slog.Logger) *Service {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	if sender == nil {
		sender = nilSender{}
	}
	typingInterval := cfg.TypingInterval
	if typingInterval <= 0 {
		typingInterval = defaultTypingInterval
	}
	baseCtx, baseCancel := context.WithCancel(context.Background())
	return &Service{
		pool: pool, q: q, ai: aiClient, pub: pub, content: contentSvc,
		sender: sender, linker: linker, cfg: cfg, now: now, logger: logger,
		priceIn:        parsePriceOrZero(cfg.PriceInputPerMTok, logger, "PriceInputPerMTok"),
		priceOut:       parsePriceOrZero(cfg.PriceOutputPerMTok, logger, "PriceOutputPerMTok"),
		typingInterval: typingInterval,
		sem:            make(chan struct{}, maxInFlightUpdates),
		queueSlots:     make(chan struct{}, maxQueuedUpdates),
		chatLocks:      newChatLockTable(),
		seen:           newSeenUpdates(maxSeenUpdateIDs),
		baseCtx:        baseCtx,
		baseCancel:     baseCancel,
	}
}

// parsePriceOrZero decimal-parses s (Config.PriceInputPerMTok/
// PriceOutputPerMTok), returning decimal.Zero for an unset value (many
// tests never set these, and cost is not what they are testing) or a
// malformed one (logged once, never a construction failure — Config's
// own doc comment explains why a parse failure here is unexpected in
// production).
func parsePriceOrZero(s string, logger *slog.Logger, field string) decimal.Decimal {
	if s == "" {
		return decimal.Zero
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		logger.Error("bot: invalid price config; cost estimates will read 0.000000 until fixed", "field", field, "value", s)
		return decimal.Zero
	}
	return d
}

// newID mints a UUID v7 (time-ordered, docs/03-ARCHITECTURE.md §
// Cross-cutting: "IDs"), falling back to a v4 in the practically
// unreachable case the v7 generator's clock read fails — mirrors every
// other module's own newID (crm.newID, shop.newID, ...).
func newID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New()
	}
	return id
}
