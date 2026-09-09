package httpx

import (
	"context"
	"net/http"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
)

// This file is a placeholder: it exists only so gen.StrictServerInterface
// is fully implemented (and `go build ./...`/`make verify` stay green)
// until T4 gives the three operations below real handlers backed by
// internal/bot (the webhook and the two `/bot/conversations*` read
// operations). The seven `/auth/telegram*`, `/auth/otp/*` and
// `/auth/password/reset` operations this file used to stub are now real
// (internal/auth/handler_telegram.go, handler_otp.go), forwarded the same
// way the rest of auth's operations are — via router.go's embedded
// *auth.Handler. Every method here does the same one thing — answer
// notImplemented() — and carries no business logic; whichever task
// implements an operation deletes that operation's stub from this file
// and adds a real one in the owning module's handler.go (router.go's
// server struct gains the corresponding field/embed the same way the
// other modules already do).
//
// notImplemented is a 501 built directly as an *apierr.Error literal
// rather than through a new apierr helper: 501 has no ErrorCode of its
// own (there is no NOT_IMPLEMENTED value in the contract's ErrorCode enum
// and this task's scope does not include contracts/openapi.yaml's error
// vocabulary or the apierr package), so this reuses the closest existing
// code, INTERNAL, the same way asError's own fallback does for any
// otherwise-unmapped error. The literal (rather than one of apierr's own
// constructors) is a temporary scaffold for this placeholder file only —
// not a pattern to copy elsewhere; real handlers build errors through
// apierr's constructors (Unauthenticated, Forbidden, NotFound, …), same
// as every other module.
func notImplemented() error {
	return &apierr.Error{
		Status:  http.StatusNotImplemented,
		Code:    gen.INTERNAL,
		Details: map[string]any{"reason": "not_implemented"},
	}
}

// ListBotConversations — TODO(T5): internal/bot, manager+
// (docs/04-DATA-MODEL.md § 7 "Bot conversations (read)").
func (server) ListBotConversations(_ context.Context, _ gen.ListBotConversationsRequestObject) (gen.ListBotConversationsResponseObject, error) {
	return nil, notImplemented()
}

// ListBotConversationMessages — TODO(T5): internal/bot, manager+.
func (server) ListBotConversationMessages(_ context.Context, _ gen.ListBotConversationMessagesRequestObject) (gen.ListBotConversationMessagesResponseObject, error) {
	return nil, notImplemented()
}

// HandleBotWebhook — TODO(T5): internal/bot. Telegram-only; `secret` must
// be compared against TELEGRAM_WEBHOOK_SECRET in constant time, 404 on a
// mismatch (docs/05-API.md § Bot) — this stub answers 501 for every
// secret regardless, which is fine only because nothing calls it yet.
func (server) HandleBotWebhook(_ context.Context, _ gen.HandleBotWebhookRequestObject) (gen.HandleBotWebhookResponseObject, error) {
	return nil, notImplemented()
}
