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
//
// The data boundary is structural, not a prompt instruction: the three
// tools this package exposes to the model (tools.go) read only through
// internal/public.Handler (the same functions the public landing calls)
// and internal/content.Service.Resolve, whose response schemas
// (gen.PublicProductListItem, gen.ProductPublic, gen.VariantPublic, the
// O-19 content blocks) simply have no cost/quantity/staff/customer field
// to leak (ADR-010) — never internal/catalog, internal/stock,
// internal/crm or internal/sales (hard rule 10).
package bot

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

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
	// safer of the two choices docs/05-API.md's task spec allows.
	SendMessage(ctx context.Context, chatID int64, text string) error
	// SendPhoto sends the image at photoURL (an absolute URL,
	// format.go's absoluteMediaURL) to chatID with a plain-text caption
	// (D-116).
	SendPhoto(ctx context.Context, chatID int64, photoURL, caption string) error
}

// TelegramLinker is the account-linking capability `/start link_<code>`
// redeems (docs/05-API.md § Auth: `POST /auth/telegram/link` mints the
// code, `GET/DELETE /auth/telegram/link` read/clear the result). No type
// in the internal/auth this task merged implements it — that is T5's own
// file scope, not T4's, and nothing there does this today (checked
// directly against the merged internal/auth package, 2026-09-08). Linker
// stays nil until T5 wires a concrete implementation into
// NewService — until then /start link_<code> always answers
// texts.startLinkUnavailable rather than guessing at a signature T5
// hasn't defined yet (this task's own instruction: "else reply 'not
// available yet' and note it").
type TelegramLinker interface {
	LinkTelegram(ctx context.Context, code string, telegramUserID int64, telegramUsername string) error
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
}

// Service implements the "Bot question" flow end to end. Every method
// takes ctx first and resolves nothing from the environment itself
// (Config above, resolved once at construction, is the only
// configuration surface).
type Service struct {
	pool    *pgxpool.Pool
	q       *db.Queries
	ai      ai.Client
	pub     *public.Handler
	content *content.Service
	sender  Sender
	linker  TelegramLinker
	cfg     Config
	now     func() time.Time
	logger  *slog.Logger
}

// NewService builds a Service. now and logger may be nil (time.Now and
// slog.Default respectively) — tests pass a fixed clock so rate-limit and
// budget window assertions never race real wall-clock time.
func NewService(pool *pgxpool.Pool, q *db.Queries, aiClient ai.Client, pub *public.Handler, contentSvc *content.Service, sender Sender, linker TelegramLinker, cfg Config, now func() time.Time, logger *slog.Logger) *Service {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		pool: pool, q: q, ai: aiClient, pub: pub, content: contentSvc,
		sender: sender, linker: linker, cfg: cfg, now: now, logger: logger,
	}
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
