package bot

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/go-telegram/bot/models"
	"github.com/jackc/pgx/v5"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// Handler implements bot's slice of gen.StrictServerInterface: the two
// admin `/bot/conversations*` read operations (docs/05-API.md § Bot,
// owner/manager — auth.PermBotRead) and the Telegram webhook
// (HandleBotWebhook, allow-listed past auth.Service.Middleware —
// internal/auth/middleware.go's own doc comment — and authenticated
// instead by its own path secret, compared constant-time below).
type Handler struct {
	svc           *Service
	webhookSecret string
}

// NewHandler wraps svc for the strict server interface. webhookSecret is
// BOT_WEBHOOK_SECRET (Config.go); an empty value makes HandleBotWebhook
// 404 for every request (subtle.ConstantTimeCompare never reports two
// different-length byte slices equal, and a caller-supplied secret is
// never itself empty over HTTP — the path segment is non-optional in the
// contract), which is the safe default for a deployment that has not
// configured a Telegram webhook at all yet (Phase 7 ships polling only,
// cmd/bot/main.go's own doc comment).
func NewHandler(svc *Service, webhookSecret string) *Handler {
	return &Handler{svc: svc, webhookSecret: webhookSecret}
}

// ListBotConversations lists the shop's bot conversations, newest
// activity first (owner/manager, auth.PermBotRead).
func (h *Handler) ListBotConversations(ctx context.Context, req gen.ListBotConversationsRequestObject) (gen.ListBotConversationsResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermBotRead); err != nil {
		return nil, err
	}

	limit := clampLimit(req.Params.Limit)
	cAt, cID, err := decodeCursor(req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	cursorAt, cursorID := cursorPtr(cAt, cID)

	rows, err := h.svc.q.ListBotConversations(ctx, db.ListBotConversationsParams{
		ShopID: authCtx.ShopID, CursorActivityAt: cursorAt, CursorID: cursorID, Limit: limit + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("bot: list conversations: %w", err)
	}

	items, nextCursor := paginateConversations(rows, limit)
	out := make([]gen.BotConversation, len(items))
	for i, row := range items {
		out[i] = toGenBotConversation(row)
	}

	list := gen.BotConversationList{Items: out}
	if nextCursor != nil {
		list.NextCursor = nullableString(nextCursor)
	} else {
		list.NextCursor = nullableString(nil)
	}
	return gen.ListBotConversations200JSONResponse(list), nil
}

// ListBotConversationMessages lists one conversation's messages, oldest
// first (owner/manager, auth.PermBotRead). 404 when id names no
// conversation in the caller's shop — never another shop's (hard rule 1).
func (h *Handler) ListBotConversationMessages(ctx context.Context, req gen.ListBotConversationMessagesRequestObject) (gen.ListBotConversationMessagesResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermBotRead); err != nil {
		return nil, err
	}

	if _, err := h.svc.q.GetBotConversation(ctx, db.GetBotConversationParams{ShopID: authCtx.ShopID, ID: req.Id}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("conversation")
		}
		return nil, fmt.Errorf("bot: get conversation: %w", err)
	}

	limit := clampLimit(req.Params.Limit)
	cAt, cID, err := decodeCursor(req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	cursorAt, cursorID := cursorPtr(cAt, cID)

	rows, err := h.svc.q.ListBotMessages(ctx, db.ListBotMessagesParams{
		ShopID: authCtx.ShopID, ConversationID: req.Id, CursorCreatedAt: cursorAt, CursorID: cursorID, Limit: limit + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("bot: list messages: %w", err)
	}

	items, nextCursor := paginateMessages(rows, limit)
	out := make([]gen.BotMessage, len(items))
	for i, row := range items {
		msg, err := toGenBotMessage(row)
		if err != nil {
			return nil, fmt.Errorf("bot: convert message: %w", err)
		}
		out[i] = msg
	}

	list := gen.BotMessageList{Items: out}
	if nextCursor != nil {
		list.NextCursor = nullableString(nextCursor)
	} else {
		list.NextCursor = nullableString(nil)
	}
	return gen.ListBotConversationMessages200JSONResponse(list), nil
}

// HandleBotWebhook is Telegram's webhook target (allow-listed past
// auth.Service.Middleware — no session, authenticated by secret instead).
// secret is compared against webhookSecret in constant time
// (crypto/subtle.ConstantTimeCompare) so a timing attack cannot narrow
// down the correct value one byte at a time; any mismatch (including an
// unconfigured, empty webhookSecret) is 404, indistinguishable from a
// path Telegram never registered — never a 403, which would confirm the
// route exists to a prober guessing secrets. The decoded update is
// handled synchronously through Service.HandleUpdate; a slow LLM call
// can make this response slow, which Telegram tolerates within its own
// webhook timeout — Phase 8's own wiring task, not this one, is the
// right place to move this onto a background goroutine if that turns out
// to matter in production.
func (h *Handler) HandleBotWebhook(ctx context.Context, req gen.HandleBotWebhookRequestObject) (gen.HandleBotWebhookResponseObject, error) {
	if h.webhookSecret == "" || subtle.ConstantTimeCompare([]byte(req.Secret), []byte(h.webhookSecret)) != 1 {
		return nil, apierr.NotFound("bot_webhook")
	}

	if req.Body == nil {
		return gen.HandleBotWebhook200Response{}, nil
	}
	b, err := json.Marshal(*req.Body)
	if err != nil {
		return gen.HandleBotWebhook200Response{}, nil
	}
	var update models.Update
	if err := json.Unmarshal(b, &update); err != nil {
		// A malformed body from something that already knew the secret is
		// still answered 200 (Telegram itself never sends one) — nothing
		// useful to retry, and 4xx/5xx here only invites a retry storm.
		return gen.HandleBotWebhook200Response{}, nil
	}

	h.svc.HandleUpdate(ctx, &update)
	return gen.HandleBotWebhook200Response{}, nil
}
