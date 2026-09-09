package bot

import (
	"context"
	"math"
	"strings"

	"github.com/go-telegram/bot/models"

	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// HandleUpdate is the "Bot question" flow's entry point
// (docs/03-ARCHITECTURE.md § Key flows): decoded once by whichever
// transport received it (cmd/bot's own long-polling loop, or
// httpx.HandleBotWebhook once Phase 8 points a real Telegram webhook at
// it), then handled identically either way — both should call Dispatch
// (dispatch.go), not this method directly, in production; every test in
// this package calls HandleUpdate itself, synchronously, on purpose.
// Customer mode only (D-111): anything that is not a text message (an
// edited message, a callback query, a photo with no caption, ...) is
// ignored — nothing here answers those.
func (s *Service) HandleUpdate(ctx context.Context, update *models.Update) {
	if update == nil || update.Message == nil {
		return
	}
	msg := update.Message
	if msg.Chat.ID == 0 {
		return
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}

	var telegramUserID int64
	var telegramUsername, languageCode string
	if msg.From != nil {
		telegramUserID = msg.From.ID
		telegramUsername = msg.From.Username
		languageCode = msg.From.LanguageCode
	}
	// isPrivateChat gates /start link_<code> (commands.go): Sonnet/Opus
	// MAJOR 4 — an account link must never be redeemable from a group
	// chat, where "whoever speaks first/next" is not necessarily the
	// Telegram user the owner actually meant to link.
	isPrivateChat := msg.Chat.Type == models.ChatTypePrivate

	shop, err := s.q.GetShop(ctx, s.cfg.ShopID)
	if err != nil {
		s.logger.Error("bot: load shop", "error", err)
		return
	}

	conv, err := s.loadOrCreateConversation(ctx, shop.ID, msg.Chat.ID, telegramUserID, telegramUsername)
	if err != nil {
		s.logger.Error("bot: load conversation", "error", err)
		return
	}

	locale := resolveCommandLocale(languageCode, shop.DefaultLocale)

	if strings.HasPrefix(text, "/") {
		// Commands never call the LLM (commands.go's own doc comment), so
		// there is no history-ordering concern here — persist the user's
		// own message unconditionally, same as every other role="user"
		// row.
		if err := s.persist(ctx, persistParams{
			ConversationID: conv.ID, ShopID: shop.ID, Role: db.BotMessageRoleUser, Content: text,
			TelegramUsername: nilIfEmpty(telegramUsername),
		}); err != nil {
			s.logger.Error("bot: persist user message", "error", err)
		}
		s.handleCommand(ctx, shop, conv, msg.Chat.ID, text, locale, telegramUsername, telegramUserID, isPrivateChat)
		return
	}

	s.handleFreeText(ctx, shop, conv, msg.Chat.ID, locale, text, telegramUsername)
}

// handleFreeText runs O-25's two gates (per-chat rate limit, then
// per-shop daily budget) before ever calling the LLM, then the tool loop
// itself (chat.go), persists the answer and sends it.
func (s *Service) handleFreeText(ctx context.Context, shop db.Shop, conv db.BotConversation, chatID int64, locale, text, telegramUsername string) {
	limited, err := s.chatRateLimited(ctx, shop.ID, conv.ID)
	if err != nil {
		s.logger.Error("bot: rate limit check", "error", err)
		s.replyStaticText(ctx, shop, conv, chatID, localeTexts(locale).rateLimited)
		return
	}
	if limited {
		s.replyStaticText(ctx, shop, conv, chatID, localeTexts(locale).rateLimited)
		return
	}

	overBudget, err := s.shopOverBudget(ctx, shop)
	if err != nil {
		// MAJOR 3: a failed budget check fails closed, the same way a
		// failed rate-limit check already does above — never "log and
		// call the LLM anyway".
		s.logger.Error("bot: budget check failed; failing closed", "error", err)
		s.replyStaticText(ctx, shop, conv, chatID, fallbackText(shop.Name, locale, s.contactLine(ctx, shop.ID, locale)))
		return
	}
	if overBudget {
		s.replyStaticText(ctx, shop, conv, chatID, fallbackText(shop.Name, locale, s.contactLine(ctx, shop.ID, locale)))
		return
	}

	// Sonnet/Opus MAJOR 5's own fix: history is loaded, and only *then* is the user's
	// own message persisted, so the current turn's question never
	// appears twice in runFreeText's own prompt (once replayed from
	// history, once appended as the current turn) and never wastes one
	// of O-26's 20 history slots on itself.
	history, err := s.loadHistory(ctx, shop.ID, conv.ID)
	if err != nil {
		s.logger.Error("bot: load history", "error", err)
		s.replyStaticText(ctx, shop, conv, chatID, fallbackText(shop.Name, locale, s.contactLine(ctx, shop.ID, locale)))
		return
	}

	if err := s.persist(ctx, persistParams{
		ConversationID: conv.ID, ShopID: shop.ID, Role: db.BotMessageRoleUser, Content: text,
		TelegramUsername: nilIfEmpty(telegramUsername),
	}); err != nil {
		// The reply still goes out even if the audit write failed — a
		// logging gap must never become a customer-visible outage
		// (ADR-013's error handling is server-side, not this).
		s.logger.Error("bot: persist user message", "error", err)
	}

	outcome := s.runFreeText(ctx, shop, locale, history, text)
	if outcome.Static {
		s.replyStaticFallback(ctx, shop, conv, chatID, locale, outcome)
		return
	}

	provider, model, cost := outcome.Provider, outcome.Model, outcome.CostEstimate
	inputTok, outputTok, latency := clampInt32(outcome.InputTokens), clampInt32(outcome.OutputTokens), clampInt32(outcome.LatencyMs)
	if err := s.persist(ctx, persistParams{
		ConversationID: conv.ID, ShopID: shop.ID, Role: db.BotMessageRoleAssistant, Content: outcome.Text,
		ToolCalls: outcome.ToolCallsLog, Provider: &provider, Model: &model,
		InputTokens: &inputTok, OutputTokens: &outputTok, LatencyMs: &latency, CostEstimate: &cost,
	}); err != nil {
		s.logger.Error("bot: persist assistant message", "error", err)
	}

	s.sendAnswer(ctx, shop, chatID, outcome)
}

// replyStaticFallback sends O-24's static fallback text for a Static
// llmOutcome and persists it — with the *real* provider/model/tokens/
// cost when the outcome carries them (a real call happened this turn,
// even though the text sent is the fallback: CRITICAL 1's own fix, so this
// turn still counts toward O-25's limits), or as a plain provider=
// "static" row with no tokens when it does not (no call ever reached the
// model this turn — replyStaticText's own, unchanged, behavior).
func (s *Service) replyStaticFallback(ctx context.Context, shop db.Shop, conv db.BotConversation, chatID int64, locale string, outcome llmOutcome) {
	text := fallbackText(shop.Name, locale, s.contactLine(ctx, shop.ID, locale))
	if outcome.Provider == "" {
		s.replyStaticText(ctx, shop, conv, chatID, text)
		return
	}

	provider, model, cost := outcome.Provider, outcome.Model, outcome.CostEstimate
	inputTok, outputTok, latency := clampInt32(outcome.InputTokens), clampInt32(outcome.OutputTokens), clampInt32(outcome.LatencyMs)
	if err := s.persist(ctx, persistParams{
		ConversationID: conv.ID, ShopID: shop.ID, Role: db.BotMessageRoleAssistant, Content: text,
		ToolCalls: outcome.ToolCallsLog, Provider: &provider, Model: &model,
		InputTokens: &inputTok, OutputTokens: &outputTok, LatencyMs: &latency, CostEstimate: &cost,
	}); err != nil {
		s.logger.Error("bot: persist assistant message", "error", err)
	}
	if err := s.sender.SendMessage(ctx, chatID, text); err != nil {
		s.logger.Error("bot: send message failed", "shop_id", shop.ID, "error", err)
	}
}

// clampInt32 bounds v (an ai.Usage token count or latency reading — an
// externally-reported int this package never fully trusts, ai/client.go's
// own doc comment) to bot_messages.input_tokens/output_tokens/latency_ms's
// own int32 column type before the narrowing conversion, the same
// explicit-range-check-before-cast shape catalog.int32Field and
// httpx.int32HTTPStatus use for their own gosec (G115) narrowing
// conversions — never a silent, wrapped-around int32(v) of an
// out-of-range value. Negative clamps to 0 (a token count/latency is
// never meaningfully negative); this never actually clamps in practice
// (no real provider reports anywhere near math.MaxInt32 tokens), but
// gosec has no way to see that statically.
func clampInt32(v int) int32 {
	if v < 0 {
		return 0
	}
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(v) // #nosec G115 -- range-checked immediately above
}
