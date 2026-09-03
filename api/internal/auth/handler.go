package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// Handler implements the five `/auth/*` operations of
// gen.StrictServerInterface (Login, Logout, GetMe, ListSessions,
// RevokeSession). internal/httpx embeds *Handler into its `server` type so
// those methods are promoted onto it; everything else is unimplemented.go's
// concern.
type Handler struct {
	svc *Service
}

// NewHandler wraps svc for the strict server interface.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Login authenticates with username and password.
func (h *Handler) Login(ctx context.Context, req gen.LoginRequestObject) (gen.LoginResponseObject, error) {
	if fields := validateLoginRequest(req.Body); len(fields) > 0 {
		return nil, apierr.Validation(fields)
	}

	ri, _ := requestInfoFromContext(ctx)

	result, err := h.svc.Login(ctx, req.Body.Username, req.Body.Password, toDBClient(req.Body.Client), ri.userAgent, ri.ip)
	if err != nil {
		return nil, err
	}

	resp := gen.LoginResponse{
		User:    toGenUser(result.User),
		Session: toGenSession(result.Session, true),
	}

	if result.Session.Client == db.SessionClientWeb {
		if ri.w != nil {
			http.SetCookie(ri.w, sessionCookie(result.Token, ttlFor(result.Session.Client, h.svc.cfg), h.svc.cfg.CookieSecure))
		}
	} else {
		resp.Token = &result.Token
	}

	return gen.Login200JSONResponse(resp), nil
}

// validateLoginRequest returns a field->reason map for a malformed login
// body — an empty username/password, or a client value other than the two
// the SessionClient enum defines. oapi-codegen only guarantees the request
// decoded as JSON, not that its fields are meaningful (hard rule 6 covers
// the shape; content validation is still this handler's job).
func validateLoginRequest(body *gen.LoginRequest) map[string]string {
	fields := map[string]string{}
	if body.Username == "" {
		fields["username"] = "required"
	}
	if body.Password == "" {
		fields["password"] = "required"
	}
	switch body.Client {
	case gen.Web, gen.Mobile:
	default:
		fields["client"] = "must be \"web\" or \"mobile\""
	}
	return fields
}

func toDBClient(c gen.SessionClient) db.SessionClient {
	if c == gen.Mobile {
		return db.SessionClientMobile
	}
	return db.SessionClientWeb
}

// Logout revokes the current session and, for a cookie-authenticated
// request, clears the cookie.
func (h *Handler) Logout(ctx context.Context, _ gen.LogoutRequestObject) (gen.LogoutResponseObject, error) {
	authCtx, ok := FromContext(ctx)
	if !ok {
		// Unreachable in production: Logout is not allow-listed, so
		// Middleware always attaches a Context before calling this.
		return nil, apierr.Unauthenticated()
	}

	if err := h.svc.Logout(ctx, authCtx.ShopID, authCtx.SessionID, authCtx.UserID); err != nil {
		return nil, fmt.Errorf("auth: logout: %w", err)
	}

	if authCtx.Client == db.SessionClientWeb {
		if ri, ok := requestInfoFromContext(ctx); ok && ri.w != nil {
			http.SetCookie(ri.w, clearSessionCookie(h.svc.cfg.CookieSecure))
		}
	}

	return gen.Logout204Response{}, nil
}

// GetMe returns the authenticated user, their shop and their permissions.
func (h *Handler) GetMe(ctx context.Context, _ gen.GetMeRequestObject) (gen.GetMeResponseObject, error) {
	authCtx, ok := FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}

	me, err := h.svc.GetMe(ctx, authCtx.ShopID, authCtx.UserID)
	if err != nil {
		return nil, fmt.Errorf("auth: get me: %w", err)
	}

	return gen.GetMe200JSONResponse(gen.Me{
		User:        toGenUser(me.User),
		Shop:        toGenShop(me.Shop),
		Permissions: Permissions(authCtx.Role),
	}), nil
}

// ListSessions lists the authenticated user's own sessions.
func (h *Handler) ListSessions(ctx context.Context, _ gen.ListSessionsRequestObject) (gen.ListSessionsResponseObject, error) {
	authCtx, ok := FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}

	sessions, err := h.svc.ListSessions(ctx, authCtx.ShopID, authCtx.UserID)
	if err != nil {
		return nil, fmt.Errorf("auth: list sessions: %w", err)
	}

	items := make([]gen.Session, len(sessions))
	for i, sess := range sessions {
		items[i] = toGenSession(sess, sess.ID == authCtx.SessionID)
	}

	return gen.ListSessions200JSONResponse(gen.SessionList{Items: items, NextCursor: nil}), nil
}

// RevokeSession revokes one of the authenticated user's own sessions.
func (h *Handler) RevokeSession(ctx context.Context, req gen.RevokeSessionRequestObject) (gen.RevokeSessionResponseObject, error) {
	authCtx, ok := FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}

	found, err := h.svc.RevokeSession(ctx, authCtx.ShopID, authCtx.UserID, req.Id)
	if err != nil {
		return nil, fmt.Errorf("auth: revoke session: %w", err)
	}
	if !found {
		return nil, apierr.NotFound("session")
	}

	return gen.RevokeSession204Response{}, nil
}

// toGenUser maps a db.User onto the API's User schema. PasswordHash and
// ShopID are deliberately not carried across — never serialize a password
// hash into a response (hard rule 9).
func toGenUser(u db.User) gen.User {
	return gen.User{
		Id:          u.ID,
		Username:    u.Username,
		FullName:    u.FullName,
		Phone:       u.Phone,
		Role:        gen.Role(u.Role),
		Locale:      gen.Locale(u.Locale),
		IsActive:    u.IsActive,
		LastLoginAt: u.LastLoginAt,
		CreatedAt:   u.CreatedAt,
	}
}

// toGenShop maps a db.Shop onto the API's Shop schema.
func toGenShop(sh db.Shop) gen.Shop {
	return gen.Shop{
		Id:                   sh.ID,
		Slug:                 sh.Slug,
		Name:                 sh.Name,
		Currency:             sh.Currency,
		Timezone:             sh.Timezone,
		DefaultLocale:        gen.Locale(sh.DefaultLocale),
		AllowNegativeStock:   sh.AllowNegativeStock,
		UpdateCostOnPurchase: sh.UpdateCostOnPurchase,
	}
}

// toGenSession maps a db.Session onto the API's Session schema. TokenHash
// is deliberately not carried across — it never leaves the server (ADR-005).
func toGenSession(s db.Session, current bool) gen.Session {
	var ip *string
	if s.Ip != nil {
		str := s.Ip.String()
		ip = &str
	}
	return gen.Session{
		Id:         s.ID,
		Client:     gen.SessionClient(s.Client),
		UserAgent:  s.UserAgent,
		Ip:         ip,
		CreatedAt:  s.CreatedAt,
		ExpiresAt:  s.ExpiresAt,
		LastSeenAt: s.LastSeenAt,
		Current:    current,
	}
}
