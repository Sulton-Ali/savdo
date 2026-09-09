package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/money"
)

// isUniqueViolation reports whether err is a Postgres unique-violation
// (SQLSTATE 23505) — mirrors crm.conflictField/shop's own copy (internal
// to each of those packages; internal/bot has exactly one unique
// constraint to worry about, bot_conversations' own (shop_id,
// telegram_chat_id), so a one-line check is enough here, no field-naming
// needed).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// nilIfEmpty turns an empty string into a nil *string — the shape every
// nullable TelegramUsername column/param below expects: "" (Telegram
// gave no username at all) must store/COALESCE as NULL, never the
// literal empty string.
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// loadOrCreateConversation resolves the one bot_conversations row for
// (shopID, chatID) — O-26: "one bot_conversations row per (shop, chat)
// forever" — creating it on a miss. A concurrent miss racing this insert
// (two updates for a brand-new chat arriving together) is resolved by
// retrying the read once against UNIQUE(shop_id, telegram_chat_id)
// (0021_bot_conversations.sql), the constraint that is the actual
// guarantee here (bot.sql's own CreateBotConversation doc comment).
// telegramUsername (0023_bot_conversations_telegram_username.sql) is
// whatever the triggering update's message.from.username was, stored
// only on a fresh row here — an existing conversation's own username is
// refreshed by TouchBotConversation instead (persist below), on every
// turn, not just the first.
func (s *Service) loadOrCreateConversation(ctx context.Context, shopID uuid.UUID, chatID, telegramUserID int64, telegramUsername string) (db.BotConversation, error) {
	conv, err := s.q.GetBotConversationByChat(ctx, db.GetBotConversationByChatParams{ShopID: shopID, TelegramChatID: chatID})
	if err == nil {
		return conv, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.BotConversation{}, fmt.Errorf("bot: get conversation: %w", err)
	}

	conv, err = s.q.CreateBotConversation(ctx, db.CreateBotConversationParams{
		ID: newID(), ShopID: shopID, TelegramChatID: chatID, TelegramUserID: telegramUserID, Mode: db.BotModeCustomer,
		TelegramUsername: nilIfEmpty(telegramUsername),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return s.q.GetBotConversationByChat(ctx, db.GetBotConversationByChatParams{ShopID: shopID, TelegramChatID: chatID})
		}
		return db.BotConversation{}, fmt.Errorf("bot: create conversation: %w", err)
	}
	return conv, nil
}

// chatRateLimited reports whether convID's rolling-hour LLM message count
// is already at O-25's limit (20/hour) — checked before any tool/LLM
// call, per O-25's own ordering ("Per-chat limit checked before
// tool/LLM").
func (s *Service) chatRateLimited(ctx context.Context, shopID, convID uuid.UUID) (bool, error) {
	since := s.now().Add(-perChatWindow)
	count, err := s.q.CountLLMMessagesSince(ctx, db.CountLLMMessagesSinceParams{ShopID: shopID, ConversationID: convID, Since: since})
	if err != nil {
		return false, fmt.Errorf("bot: count llm messages: %w", err)
	}
	return count >= perChatLimit, nil
}

// shopLocation resolves shop.Timezone to a *time.Location, falling back
// to UTC when the stored value fails to load — the same defensive
// fallback internal/public.shopLocation and internal/sales' own copies
// use (every timezone this codebase writes is a valid IANA name; a shop
// row with a bad one is a data bug worth a wrong-but-safe fallback, not a
// crashed bot).
func shopLocation(shop db.Shop) *time.Location {
	loc, err := time.LoadLocation(shop.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// startOfDay is midnight of now's calendar date in loc — O-25's "Asia/
// Tashkent day" (or whatever timezone the shop is actually configured
// with).
func startOfDay(now time.Time, loc *time.Location) time.Time {
	local := now.In(loc)
	y, m, d := local.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// shopOverBudget reports whether shop's daily token spend (input+output,
// summed since local midnight in the shop's own timezone) is at or over
// its effective daily budget (O-25, D-120): shops.ai_daily_token_budget
// when the shop row sets one, else Config.DailyTokenBudget
// (AI_DAILY_TOKEN_BUDGET, default 200000 — NewService's own doc
// comment). D-120 reverses O-25's original "NULL means unlimited"
// reading now that an env-level fallback exists to mean "unlimited"
// would need its own explicit (very large) value instead: an effective
// budget of zero or less — the shop column NULL *and* the env value
// unset/zero — fails closed (no cap configured at all is never read as
// "no limit").
func (s *Service) shopOverBudget(ctx context.Context, shop db.Shop) (bool, error) {
	budget := s.cfg.DailyTokenBudget
	if shop.AiDailyTokenBudget != nil {
		budget = int(*shop.AiDailyTokenBudget)
	}
	if budget <= 0 {
		return true, nil
	}
	since := startOfDay(s.now(), shopLocation(shop))
	total, err := s.q.SumBotTokensSince(ctx, db.SumBotTokensSinceParams{ShopID: shop.ID, Since: since})
	if err != nil {
		return false, fmt.Errorf("bot: sum bot tokens: %w", err)
	}
	return total >= int64(budget), nil
}

// persistParams is one bot_messages row to write, plus the
// TouchBotConversation bump every row triggers (bot.sql's own doc
// comment: "Called once per turn the bot writes to bot_messages").
type persistParams struct {
	ConversationID uuid.UUID
	ShopID         uuid.UUID
	Role           db.BotMessageRole
	Content        string
	ToolCalls      []byte
	Provider       *string
	Model          *string
	InputTokens    *int32
	OutputTokens   *int32
	LatencyMs      *int32
	CostEstimate   *string // decimal string (ADR-007); nil means the column stays NULL.
	// TelegramUsername refreshes bot_conversations.telegram_username
	// (0023_bot_conversations_telegram_username.sql) opportunistically:
	// set it on a user-turn persist call (update.go has one, straight
	// from the triggering update's message.from.username), leave it nil
	// on every assistant persist call (there is no Telegram update to
	// read one from) — TouchBotConversation's own COALESCE keeps the
	// previously stored value in that case rather than wiping it to NULL.
	TelegramUsername *string
}

// persist inserts one bot_messages row (append-only — 0022_bot_messages.
// sql's own trigger rejects an UPDATE at the database level) and touches
// its conversation, returning the inserted row so a caller that needs
// its ID (handleFreeText's own loadHistory exclusion, Sonnet/Opus MAJOR
// 1's own fix) does not have to mint and pre-thread one itself. Errors
// are the caller's to log — by class only, never err's own text
// (logWriteError below) — and never the customer's to see: a failed
// audit write must not stop a reply from being sent (update.go's own
// call sites all log-and-continue on this).
func (s *Service) persist(ctx context.Context, p persistParams) (db.BotMessage, error) {
	var cost pgtype.Numeric
	if p.CostEstimate != nil {
		d, err := decimal.NewFromString(*p.CostEstimate)
		if err != nil {
			return db.BotMessage{}, fmt.Errorf("bot: parse cost estimate %q: %w", *p.CostEstimate, err)
		}
		cost = money.ToNumeric(d)
	}

	msg, err := s.q.InsertBotMessage(ctx, db.InsertBotMessageParams{
		ID: newID(), ConversationID: p.ConversationID, ShopID: p.ShopID, Role: p.Role, Content: p.Content,
		ToolCalls: p.ToolCalls, Provider: p.Provider, Model: p.Model,
		InputTokens: p.InputTokens, OutputTokens: p.OutputTokens, LatencyMs: p.LatencyMs, CostEstimate: cost,
	})
	if err != nil {
		return db.BotMessage{}, fmt.Errorf("bot: insert message: %w", err)
	}

	createdAt := msg.CreatedAt
	if _, err := s.q.TouchBotConversation(ctx, db.TouchBotConversationParams{
		LastMessageAt: &createdAt, TelegramUsername: p.TelegramUsername, ShopID: p.ShopID, ID: p.ConversationID,
	}); err != nil {
		return msg, fmt.Errorf("bot: touch conversation: %w", err)
	}
	return msg, nil
}

// logWriteError logs a failed write whose own params can carry the
// customer's or the model's raw text (persist's own Content field) by
// error class only — never err's Error() text (MINOR 5). Some Postgres
// errors embed a bound value in their own DETAIL clause (a unique-
// violation's "Key (col)=(value) already exists", for one); this is the
// same "never log request/response content itself" stance
// logProviderError (chat.go) already takes for a provider error, applied
// here to a database write error instead. Unwraps fully so the logged
// type is the actual root cause (e.g. *pgconn.PgError), not just
// whichever fmt.Errorf("...: %w", err) wrapper happened to return it.
func logWriteError(logger *slog.Logger, msg string, err error) {
	cause := err
	for {
		u := errors.Unwrap(cause)
		if u == nil {
			break
		}
		cause = u
	}
	logger.Error(msg, "error_type", fmt.Sprintf("%T", cause))
}
