// Package auth implements Savdo's authentication and authorization
// (ADR-005, ADR-010): argon2id password hashing, opaque DB-backed sessions
// with sliding expiry, the strict-server middleware every protected
// operation runs through, the role/permission matrix, and the five
// `/auth/*` operations (login, logout, me, list/revoke sessions).
//
// Every later module reads the authenticated request's identity from
// Context via FromContext — never from the request body or a path
// parameter (hard rule 1 / ADR-004: shop_id always comes from here).
package auth

import (
	"context"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// Context is what Middleware puts on every authenticated request's
// context.Context. A later handler or service reads it with FromContext —
// shop_id, user_id and role are never taken from the request itself.
type Context struct {
	// ShopID is the tenant boundary (ADR-004): every business-table query
	// the rest of the request makes must filter by this.
	ShopID uuid.UUID

	// UserID identifies the authenticated user.
	UserID uuid.UUID

	// Role is the authenticated user's role, as stored on their `users`
	// row. Require and Permissions key off this.
	Role db.UserRole

	// SessionID is the session row this request authenticated with —
	// needed by Logout (which revokes exactly this session) and by
	// ListSessions (to mark the "current" one).
	SessionID uuid.UUID

	// Client says which transport carried the credential for *this*
	// request: SessionClientWeb when it came from the `savdo_session`
	// cookie, SessionClientMobile when it came from the `Authorization:
	// Bearer` header. In normal operation this always matches the
	// session's own persisted `client` column, because Login only ever
	// hands a cookie to a `client: web` login and only ever hands a token
	// to a `client: mobile` login — but Client here is deliberately
	// re-derived per request (see middleware.go's extractToken), not
	// copied from the session row, because it is what CSRF enforcement
	// and Logout's cookie-clearing actually need to know: was this
	// specific request authenticated the way a browser automatically
	// authenticates (the cookie), or the way a client that read the
	// token once and holds it explicitly does (the header)?
	Client db.SessionClient
}

// ctxKey is an unexported type so no other package can collide with this
// context key.
type ctxKey struct{}

// WithContext returns a copy of ctx carrying c, retrievable with
// FromContext.
func WithContext(ctx context.Context, c Context) context.Context {
	return context.WithValue(ctx, ctxKey{}, c)
}

// FromContext returns the Context Middleware attached to ctx, and whether
// one was present. Every service handling anything but the three
// allow-listed operations (docs/03-ARCHITECTURE.md § Auth) can assume ok is
// true — Middleware rejects the request with 401 before a handler ever
// runs otherwise — but callers still check ok rather than assume it, since
// a caller reachable from a test or a future allow-listed operation cannot
// rely on that invariant holding.
func FromContext(ctx context.Context) (Context, bool) {
	c, ok := ctx.Value(ctxKey{}).(Context)
	return c, ok
}
