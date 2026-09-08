package httpx

import (
	"context"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// The three methods below satisfy gen.StrictServerInterface's `/bot/*`
// operations by forwarding to server.bot (*bot.Handler) — named, not
// embedded, for the same reason server.shop/server.crm/server.content
// are (healthz.go's doc comment on the server struct).

// ListBotConversations lists the shop's bot conversations (owner/manager).
func (s server) ListBotConversations(ctx context.Context, req gen.ListBotConversationsRequestObject) (gen.ListBotConversationsResponseObject, error) {
	return s.bot.ListBotConversations(ctx, req)
}

// ListBotConversationMessages lists one conversation's messages
// (owner/manager).
func (s server) ListBotConversationMessages(ctx context.Context, req gen.ListBotConversationMessagesRequestObject) (gen.ListBotConversationMessagesResponseObject, error) {
	return s.bot.ListBotConversationMessages(ctx, req)
}

// HandleBotWebhook is Telegram's webhook target (no session; secret
// path segment instead — bot.Handler.HandleBotWebhook's own doc comment).
func (s server) HandleBotWebhook(ctx context.Context, req gen.HandleBotWebhookRequestObject) (gen.HandleBotWebhookResponseObject, error) {
	return s.bot.HandleBotWebhook(ctx, req)
}
