package bot

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// maxCatalogCategories bounds /catalog's reply (D-115: "top categories
// with site links") — root-level categories only, capped so a shop with
// a very large tree still gets one short Telegram message.
const maxCatalogCategories = 20

// handleCommand dispatches one of the four D-113 slash commands
// (/start, /hours, /address, /catalog); anything else gets
// texts.unknownCommand. Command replies never call the LLM — they are
// answered statically from internal/content.Service.Resolve and
// internal/public.Handler, the same boundary the free-text tools read
// through (tools.go's own doc comment) — so they are persisted with
// provider "static" the same way O-24/O-25's fallback replies are
// (persist.go's own convention: "static" means "not a real LLM call",
// matching CountLLMMessagesSince's own filter). telegramUserID and
// isPrivateChat are the *live* update's own values (update.go), never
// conv's stored ones — only /start link_<code> needs either, but every
// command gets them so handleStart never has to guess which caller
// passed the real thing (Sonnet/Opus MAJOR 4's own fix).
func (s *Service) handleCommand(ctx context.Context, shop db.Shop, conv db.BotConversation, chatID int64, text, locale, telegramUsername string, telegramUserID int64, isPrivateChat bool) {
	cmd, payload := splitCommand(text)
	switch cmd {
	case "/start":
		s.handleStart(ctx, shop, conv, chatID, locale, payload, telegramUsername, telegramUserID, isPrivateChat)
	case "/hours":
		s.handleHours(ctx, shop, conv, chatID, locale)
	case "/address":
		s.handleAddress(ctx, shop, conv, chatID, locale)
	case "/catalog":
		s.handleCatalog(ctx, shop, conv, chatID, locale)
	default:
		s.replyStaticText(ctx, shop, conv, chatID, localeTexts(locale).unknownCommand)
	}
}

// splitCommand separates a slash command from its payload and strips a
// `@botusername` suffix Telegram appends to commands in group chats
// (e.g. "/start@savdo_bot link_abc123" -> "/start", "link_abc123").
func splitCommand(text string) (cmd, payload string) {
	fields := strings.SplitN(text, " ", 2)
	cmd = fields[0]
	if i := strings.IndexByte(cmd, '@'); i >= 0 {
		cmd = cmd[:i]
	}
	if len(fields) > 1 {
		payload = strings.TrimSpace(fields[1])
	}
	return cmd, payload
}

// replyStaticText persists text as a provider="static" assistant row and
// sends it — every command reply and every O-24/O-25 fallback goes
// through this one function.
func (s *Service) replyStaticText(ctx context.Context, shop db.Shop, conv db.BotConversation, chatID int64, text string) {
	provider := "static"
	if _, err := s.persist(ctx, persistParams{ConversationID: conv.ID, ShopID: shop.ID, Role: db.BotMessageRoleAssistant, Content: text, Provider: &provider}); err != nil {
		logWriteError(s.logger, "bot: persist static reply", err)
	}
	if err := s.sender.SendMessage(ctx, chatID, text); err != nil {
		s.logger.Error("bot: send message failed", "shop_id", shop.ID, "error", err)
	}
}

// handleStart answers plain /start with D-113's greeting, and /start
// link_<code> by redeeming code through s.linker — using the *live*
// telegramUserID/telegramUsername the triggering update itself carried,
// never conv.TelegramUserID (the *first* update that ever created this
// bot_conversations row, which can be a different Telegram user's id
// once T5 wires a real Linker: Sonnet/Opus MAJOR 4, an account-takeover
// risk in any chat more than one person can post to). isPrivateChat
// rejects the redemption outright in a group/supergroup/channel — a
// link code is a one-person credential, and "whoever sends /start
// link_<code> next" in a group is never guaranteed to be the Telegram
// account the owner meant to link. telegramUserID is 0 whenever the
// triggering update carried no `from` at all (update.go's own zero
// value for that case) — MINOR 4: a Telegram update with no sender
// identity can never redeem a link code either, the same refusal a
// non-private chat gets.
func (s *Service) handleStart(ctx context.Context, shop db.Shop, conv db.BotConversation, chatID int64, locale, payload, telegramUsername string, telegramUserID int64, isPrivateChat bool) {
	if strings.HasPrefix(payload, "link_") {
		if !isPrivateChat || telegramUserID == 0 {
			s.replyStaticText(ctx, shop, conv, chatID, localeTexts(locale).startLinkFailed)
			return
		}
		code := strings.TrimPrefix(payload, "link_")
		if s.linker == nil {
			s.replyStaticText(ctx, shop, conv, chatID, localeTexts(locale).startLinkUnavailable)
			return
		}
		if err := s.linker.LinkTelegram(ctx, code, telegramUserID, telegramUsername); err != nil {
			s.replyStaticText(ctx, shop, conv, chatID, localeTexts(locale).startLinkFailed)
			return
		}
		s.replyStaticText(ctx, shop, conv, chatID, localeTexts(locale).startLinkSuccess)
		return
	}
	s.replyStaticText(ctx, shop, conv, chatID, fmt.Sprintf(localeTexts(locale).greeting, shop.Name))
}

func (s *Service) handleHours(ctx context.Context, shop db.Shop, conv db.BotConversation, chatID int64, locale string) {
	t := localeTexts(locale)
	resolved, err := s.content.Resolve(ctx, shop.ID, gen.Locale(locale))
	if err != nil {
		s.logger.Error("bot: resolve hours", "error", err)
		s.replyStaticText(ctx, shop, conv, chatID, t.hoursUnavailable)
		return
	}
	block, ok := resolved[gen.Hours]
	if !ok || block.Data == nil {
		s.replyStaticText(ctx, shop, conv, chatID, t.hoursUnavailable)
		return
	}
	var h gen.ContentHours
	if err := decodeInto(block.Data, &h); err != nil {
		s.replyStaticText(ctx, shop, conv, chatID, t.hoursUnavailable)
		return
	}
	s.replyStaticText(ctx, shop, conv, chatID, formatHours(h, t))
}

func formatHours(h gen.ContentHours, t texts) string {
	var b strings.Builder
	b.WriteString(t.hoursNoDaysHeader)
	for _, d := range h.Days {
		b.WriteString("\n")
		b.WriteString(t.dayNames[string(d.Day)])
		b.WriteString(": ")
		if d.Closed || d.Open == nil || d.Close == nil {
			b.WriteString("—")
			continue
		}
		b.WriteString(*d.Open)
		b.WriteString("–")
		b.WriteString(*d.Close)
	}
	if h.Note != nil && strings.TrimSpace(*h.Note) != "" {
		b.WriteString("\n")
		b.WriteString(*h.Note)
	}
	return b.String()
}

func (s *Service) handleAddress(ctx context.Context, shop db.Shop, conv db.BotConversation, chatID int64, locale string) {
	t := localeTexts(locale)
	resolved, err := s.content.Resolve(ctx, shop.ID, gen.Locale(locale))
	if err != nil {
		s.logger.Error("bot: resolve address", "error", err)
		s.replyStaticText(ctx, shop, conv, chatID, t.addressUnavailable)
		return
	}
	block, ok := resolved[gen.Contacts]
	if !ok || block.Data == nil {
		s.replyStaticText(ctx, shop, conv, chatID, t.addressUnavailable)
		return
	}
	var c gen.ContentContacts
	if err := decodeInto(block.Data, &c); err != nil {
		s.replyStaticText(ctx, shop, conv, chatID, t.addressUnavailable)
		return
	}
	var b strings.Builder
	b.WriteString(c.Address)
	if c.Phone != "" {
		b.WriteString("\n")
		b.WriteString(c.Phone)
	}
	if c.MapUrl != nil && *c.MapUrl != "" {
		b.WriteString("\n")
		b.WriteString(*c.MapUrl)
	}
	s.replyStaticText(ctx, shop, conv, chatID, b.String())
}

// contactLine builds O-24's "phone and Telegram link from the contacts
// block" — used by the static fallback (update.go), not by /address
// (which shows the full contacts block already).
func (s *Service) contactLine(ctx context.Context, shopID uuid.UUID, locale string) string {
	resolved, err := s.content.Resolve(ctx, shopID, gen.Locale(locale))
	if err != nil {
		return ""
	}
	var parts []string
	if block, ok := resolved[gen.Contacts]; ok && block.Data != nil {
		var c gen.ContentContacts
		if err := decodeInto(block.Data, &c); err == nil && c.Phone != "" {
			parts = append(parts, c.Phone)
		}
	}
	if block, ok := resolved[gen.Social]; ok && block.Data != nil {
		var soc gen.ContentSocial
		if err := decodeInto(block.Data, &soc); err == nil && soc.Telegram != nil && *soc.Telegram != "" {
			parts = append(parts, *soc.Telegram)
		}
	}
	return strings.Join(parts, " · ")
}

func (s *Service) handleCatalog(ctx context.Context, shop db.Shop, conv db.BotConversation, chatID int64, locale string) {
	t := localeTexts(locale)
	resp, err := withLocale(ctx, locale, func(ctx context.Context) (gen.ListPublicCategoriesResponseObject, error) {
		return s.pub.ListPublicCategories(ctx, gen.ListPublicCategoriesRequestObject{})
	})
	if err != nil {
		s.replyStaticText(ctx, shop, conv, chatID, t.catalogUnavailable)
		return
	}
	list, ok := resp.(gen.ListPublicCategories200JSONResponse)
	if !ok {
		s.replyStaticText(ctx, shop, conv, chatID, t.catalogUnavailable)
		return
	}

	var top []gen.PublicCategory
	for _, c := range list.Items {
		if _, err := c.ParentSlug.Get(); err != nil { // no value -> root category
			top = append(top, c)
			if len(top) >= maxCatalogCategories {
				break
			}
		}
	}
	if len(top) == 0 {
		s.replyStaticText(ctx, shop, conv, chatID, t.catalogEmpty)
		return
	}

	var b strings.Builder
	b.WriteString(t.catalogHeader)
	for _, c := range top {
		b.WriteString("\n")
		b.WriteString(c.Name)
		b.WriteString(" — ")
		b.WriteString(categoryURL(s.cfg.SiteURL, locale, c.Slug))
	}
	s.replyStaticText(ctx, shop, conv, chatID, b.String())
}
