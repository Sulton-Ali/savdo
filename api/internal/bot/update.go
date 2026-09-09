package bot

import (
	"context"
	"math"
	"regexp"
	"strings"

	"github.com/go-telegram/bot/models"
	"github.com/google/uuid"

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

	// Sonnet/Opus MAJOR 1: the user's own message is persisted first, on
	// every path, before either of O-25's gates (chatRateLimited,
	// shopOverBudget) or the command dispatch ever run — a rate-limited,
	// over-budget, or gate-error turn must still leave its own question
	// in the transcript (O-25's own "the rate-limited message is
	// stored"; D-114's whole retention purpose), the same way a command
	// always has. loadHistory (handleFreeText, chat.go) excludes this
	// row by id instead of requiring persist to run after it, so the
	// LLM's own prompt still never sees the current question twice.
	// MAJOR (round 4): redactLinkCode strips any link_<code> substring
	// from what gets persisted AND from what a free-text turn hands the
	// model — never just the exact "/start link_<code>" command shape,
	// which missed a code pasted as plain text, the full deep-link URL,
	// or a case/whitespace/@suffix variant of the command itself. A
	// command still gets the real, unredacted text (handleCommand below)
	// — only what gets written to bot_messages and what a free-text turn
	// sends to the model changes.
	redactedText := redactLinkCode(text)
	userMsg, err := s.persist(ctx, persistParams{
		ConversationID: conv.ID, ShopID: shop.ID, Role: db.BotMessageRoleUser, Content: redactedText,
		TelegramUsername: nilIfEmpty(telegramUsername),
	})
	if err != nil {
		// The reply still goes out even if the audit write failed — a
		// logging gap must never become a customer-visible outage
		// (ADR-013's error handling is server-side, not this).
		logWriteError(s.logger, "bot: persist user message", err)
	}

	if strings.HasPrefix(text, "/") {
		s.handleCommand(ctx, shop, conv, msg.Chat.ID, text, locale, telegramUsername, telegramUserID, isPrivateChat)
		return
	}

	s.handleFreeText(ctx, shop, conv, msg.Chat.ID, locale, redactedText, userMsg.ID)
}

// handleFreeText runs O-25's two gates (per-chat rate limit, then
// per-shop daily budget) before ever calling the LLM, then the tool loop
// itself (chat.go), persists the answer and sends it. userMsgID is the
// current turn's own already-persisted user-message row (HandleUpdate
// persists it before either gate runs, MAJOR 1) — loadHistory excludes
// it by id so the LLM's own prompt never replays the current question a
// second time.
func (s *Service) handleFreeText(ctx context.Context, shop db.Shop, conv db.BotConversation, chatID int64, locale, text string, userMsgID uuid.UUID) {
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

	history, err := s.loadHistory(ctx, shop.ID, conv.ID, userMsgID)
	if err != nil {
		s.logger.Error("bot: load history", "error", err)
		s.replyStaticText(ctx, shop, conv, chatID, fallbackText(shop.Name, locale, s.contactLine(ctx, shop.ID, locale)))
		return
	}

	// O-30: the typing indicator only covers the model call itself —
	// every gate above already answered instantly, so a rate-limited or
	// over-budget turn (or a history-load failure) never shows it.
	// stopTyping is deferred, not called only on the success path, so it
	// still runs if runFreeText panics (dispatch.go's own recover
	// unwinds through this defer first) or ctx is canceled mid-call
	// (Close's own shutdown, baseCtx).
	stopTyping := s.startTyping(ctx, chatID)
	defer stopTyping()

	outcome := s.runFreeText(ctx, shop, locale, history, text)
	if outcome.Static {
		s.replyStaticFallback(ctx, shop, conv, chatID, locale, outcome)
		return
	}

	provider, model, cost := outcome.Provider, outcome.Model, outcome.CostEstimate
	inputTok, outputTok, latency := clampInt32(outcome.InputTokens), clampInt32(outcome.OutputTokens), clampInt32(outcome.LatencyMs)
	if _, err := s.persist(ctx, persistParams{
		ConversationID: conv.ID, ShopID: shop.ID, Role: db.BotMessageRoleAssistant, Content: outcome.Text,
		ToolCalls: outcome.ToolCallsLog, Provider: &provider, Model: &model,
		InputTokens: &inputTok, OutputTokens: &outputTok, LatencyMs: &latency, CostEstimate: &cost,
	}); err != nil {
		logWriteError(s.logger, "bot: persist assistant message", err)
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
	if _, err := s.persist(ctx, persistParams{
		ConversationID: conv.ID, ShopID: shop.ID, Role: db.BotMessageRoleAssistant, Content: text,
		ToolCalls: outcome.ToolCallsLog, Provider: &provider, Model: &model,
		InputTokens: &inputTok, OutputTokens: &outputTok, LatencyMs: &latency, CostEstimate: &cost,
	}); err != nil {
		logWriteError(s.logger, "bot: persist assistant message", err)
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

// linkCodeToken matches a link_<code> credential anywhere in a message —
// not just inside an exact "/start link_<code>" command — in the base32
// alphabet internal/auth/telegram.go's own newSelectorToken encodes a
// link code with (RFC 4648 §6: uppercase A-Z and digits 2-7, no padding;
// see otp.go's own selectorEncoding). Case-insensitive: a customer can
// retype or paste a code in a different case than the deep-link URL
// showed it in, and the match must still catch it. "link_" itself is
// never itself base32 (the underscore is not in the alphabet), so the
// match can never run past the code into unrelated following text.
var linkCodeToken = regexp.MustCompile(`(?i)link_[A-Z2-7]+`)

// redactLinkCode replaces every link_<code> occurrence text carries —
// wherever it appears — with a fixed placeholder (MAJOR, round 4): an
// un-redeemed code is a bearer credential for whoever's account minted
// it (POST /auth/telegram/link), and both the admin transcript (GET
// /bot/conversations/{id}/messages, manager-readable) and the model's
// own prompt (a real call could echo it straight back in its answer)
// must never carry it. Catches the canonical "/start link_<code>"
// command, the same command with different case/whitespace/an
// "@botusername" suffix (redactLinkCode does not need to parse the
// command shape at all — it only looks for the credential token
// itself), a customer pasting the code as plain text with no command
// around it at all, and the code embedded in a full
// "https://t.me/<bot>?start=link_<code>" deep-link URL pasted instead
// of tapped. Accepted residual risk, not fixed here: a customer who
// pastes *only* the part after "link_" — the bare code with no prefix
// at all — cannot be told apart from arbitrary free text and is not
// redacted.
func redactLinkCode(text string) string {
	return linkCodeToken.ReplaceAllString(text, "link_***")
}
