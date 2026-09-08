package httpx

import (
	"context"
	"net/http"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
)

// This file is a placeholder: it exists only so gen.StrictServerInterface
// is fully implemented (and `go build ./...`/`make verify` stay green)
// between T3, which adds the ten operations below to
// contracts/openapi.yaml, and T4/T5, which give them real handlers backed
// by internal/auth (the four `/auth/telegram*`, `/auth/otp/*` and
// `/auth/password/reset` operations, plus the three `/auth/telegram/link`
// operations) and internal/bot (the webhook and the two
// `/bot/conversations*` read operations). Every method here does the same
// one thing — answer notImplemented() — and carries no business logic;
// whichever task implements an operation deletes that operation's stub
// from this file and adds a real one in the owning module's handler.go
// (router.go's server struct gains the corresponding field/embed the same
// way the other modules already do).
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

// RequestOtp — TODO(T4): internal/auth, OTP delivered via the bot to the
// username's linked Telegram account (docs/05-API.md § Auth).
func (server) RequestOtp(_ context.Context, _ gen.RequestOtpRequestObject) (gen.RequestOtpResponseObject, error) {
	return nil, notImplemented()
}

// VerifyOtp — TODO(T4): internal/auth.
func (server) VerifyOtp(_ context.Context, _ gen.VerifyOtpRequestObject) (gen.VerifyOtpResponseObject, error) {
	return nil, notImplemented()
}

// ResetPassword — TODO(T4): internal/auth.
func (server) ResetPassword(_ context.Context, _ gen.ResetPasswordRequestObject) (gen.ResetPasswordResponseObject, error) {
	return nil, notImplemented()
}

// AuthenticateTelegram — TODO(T4): internal/auth, Telegram Login Widget
// HMAC verification against TELEGRAM_BOT_TOKEN (ADR-005).
func (server) AuthenticateTelegram(_ context.Context, _ gen.AuthenticateTelegramRequestObject) (gen.AuthenticateTelegramResponseObject, error) {
	return nil, notImplemented()
}

// DeleteTelegramLink — TODO(T4): internal/auth.
func (server) DeleteTelegramLink(_ context.Context, _ gen.DeleteTelegramLinkRequestObject) (gen.DeleteTelegramLinkResponseObject, error) {
	return nil, notImplemented()
}

// GetTelegramLink — TODO(T4): internal/auth.
func (server) GetTelegramLink(_ context.Context, _ gen.GetTelegramLinkRequestObject) (gen.GetTelegramLinkResponseObject, error) {
	return nil, notImplemented()
}

// CreateTelegramLink — TODO(T4): internal/auth.
func (server) CreateTelegramLink(_ context.Context, _ gen.CreateTelegramLinkRequestObject) (gen.CreateTelegramLinkResponseObject, error) {
	return nil, notImplemented()
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
