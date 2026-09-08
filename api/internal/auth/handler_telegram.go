package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
)

// maxTelegramFieldLength bounds the free-text fields a Login Widget
// payload carries (firstName/lastName/username/hash/id) — the contract
// puts no maxLength on TelegramAuthRequest's own properties, but nothing
// stops a forged request from sending an arbitrarily large string for one
// of them, and VerifyLoginWidget's data-check-string building has no
// reason to process an unbounded amount of attacker-controlled text
// before it even gets to the HMAC comparison that would reject it anyway.
const maxTelegramFieldLength = 256

// AuthenticateTelegram authenticates via the Telegram Login Widget.
func (h *Handler) AuthenticateTelegram(ctx context.Context, req gen.AuthenticateTelegramRequestObject) (gen.AuthenticateTelegramResponseObject, error) {
	if fields := validateTelegramAuthRequest(req.Body); len(fields) > 0 {
		return nil, apierr.Validation(fields)
	}

	ri, _ := requestInfoFromContext(ctx)

	result, err := h.svc.AuthenticateTelegram(ctx, *req.Body, ri.userAgent, ri.ip)
	if err != nil {
		return nil, err
	}

	resp := gen.LoginResponse{
		User:    toGenUser(result.User),
		Session: toGenSession(result.Session, true),
	}
	// ADR-005 / the contract's own description: the Login Widget is a
	// web-only flow, so AuthenticateTelegram always starts a client: web
	// session (Service.AuthenticateTelegram hardcodes it) — a cookie, no
	// token in the body, same as Login's own client: web branch.
	if ri.w != nil {
		http.SetCookie(ri.w, sessionCookie(result.Token, ttlFor(result.Session.Client, h.svc.cfg), h.svc.cfg.CookieSecure))
	}

	return gen.AuthenticateTelegram200JSONResponse(resp), nil
}

// validateTelegramAuthRequest returns a field->reason map for a malformed
// TelegramAuthRequest: the contract's own required fields (id, authDate,
// hash) missing or blank, or any field over maxTelegramFieldLength.
func validateTelegramAuthRequest(body *gen.TelegramAuthRequest) map[string]string {
	fields := map[string]string{}
	if body.Id == "" {
		fields["id"] = "required"
	} else if len(body.Id) > maxTelegramFieldLength {
		fields["id"] = fmt.Sprintf("must be at most %d characters", maxTelegramFieldLength)
	}
	if body.Hash == "" {
		fields["hash"] = "required"
	} else if len(body.Hash) > maxTelegramFieldLength {
		fields["hash"] = fmt.Sprintf("must be at most %d characters", maxTelegramFieldLength)
	}
	if body.AuthDate == 0 {
		fields["authDate"] = "required"
	}
	for name, v := range map[string]*string{
		"firstName": body.FirstName, "lastName": body.LastName,
		"username": body.Username, "photoUrl": body.PhotoUrl,
	} {
		if v != nil && len(*v) > maxTelegramFieldLength {
			fields[name] = fmt.Sprintf("must be at most %d characters", maxTelegramFieldLength)
		}
	}
	return fields
}

// CreateTelegramLink starts linking the caller's account to a Telegram
// account. Any authenticated role.
func (h *Handler) CreateTelegramLink(ctx context.Context, _ gen.CreateTelegramLinkRequestObject) (gen.CreateTelegramLinkResponseObject, error) {
	authCtx, ok := FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}

	result, err := h.svc.CreateTelegramLink(ctx, authCtx.ShopID, authCtx.UserID)
	if err != nil {
		return nil, fmt.Errorf("auth: create telegram link: %w", err)
	}

	deepLink := fmt.Sprintf("https://t.me/%s?start=link_%s", h.svc.cfg.BotUsername, result.Code)
	return gen.CreateTelegramLink200JSONResponse(gen.TelegramLinkCode{
		Code:     result.Code,
		DeepLink: deepLink,
	}), nil
}

// GetTelegramLink reports the caller's current Telegram link status.
func (h *Handler) GetTelegramLink(ctx context.Context, _ gen.GetTelegramLinkRequestObject) (gen.GetTelegramLinkResponseObject, error) {
	authCtx, ok := FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}

	linked, username, err := h.svc.GetTelegramLink(ctx, authCtx.ShopID, authCtx.UserID)
	if err != nil {
		return nil, fmt.Errorf("auth: get telegram link: %w", err)
	}

	status := gen.TelegramLinkStatus{Linked: linked, TelegramUsername: nullableString(username)}
	return gen.GetTelegramLink200JSONResponse(status), nil
}

// DeleteTelegramLink unlinks the caller's Telegram account.
func (h *Handler) DeleteTelegramLink(ctx context.Context, _ gen.DeleteTelegramLinkRequestObject) (gen.DeleteTelegramLinkResponseObject, error) {
	authCtx, ok := FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}

	if err := h.svc.DeleteTelegramLink(ctx, authCtx.ShopID, authCtx.UserID); err != nil {
		return nil, fmt.Errorf("auth: delete telegram link: %w", err)
	}

	return gen.DeleteTelegramLink204Response{}, nil
}
