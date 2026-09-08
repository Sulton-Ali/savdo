package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// allowlistedOperations are the operationIDs Middleware lets through
// without a session — the oapi-codegen strict server passes the Go method
// name (PascalCase, e.g. "GetHealthz"), not the OpenAPI operationId
// (lowerCamel, "getHealthz") — verified by reading the generated
// middleware(handler, "...") call sites in gen/api.gen.go, since the two
// casings differ and only one of them is what actually reaches this
// function's operationID parameter at runtime. The four Public* entries
// are Phase 6's `GET /public/*` (D-99/D-105, `security: []` in the
// contract): this map is the one place that actually enforces "no
// session required" — the contract's `security: []` is documentation,
// not enforcement — so internal/public reads no auth.Context anywhere
// (it has none to read).
var allowlistedOperations = map[string]bool{
	"GetHealthz":             true,
	"GetReadyz":              true,
	"Login":                  true,
	"GetPublicShop":          true,
	"ListPublicCategories":   true,
	"ListPublicProducts":     true,
	"GetPublicProductBySlug": true,
}

// requestCtxKey is the context key requestInfo is stored under.
type requestCtxKey struct{}

// requestInfo carries the pieces of the raw *http.Request and
// http.ResponseWriter a StrictServerInterface method has no other way to
// reach: the strict interface's methods take only (ctx, request), never
// (w, r) (see gen.StrictServerInterface / gen.StrictHandlerFunc in
// gen/api.gen.go). Middleware — which does receive w and r, per
// gen.StrictMiddlewareFunc's signature — stashes what handler.go needs
// (Login to set the cookie / read User-Agent and IP, Logout to clear the
// cookie) into ctx before calling the inner handler.
type requestInfo struct {
	w         http.ResponseWriter
	userAgent string
	ip        *netip.Addr
}

func withRequestInfo(ctx context.Context, ri requestInfo) context.Context {
	return context.WithValue(ctx, requestCtxKey{}, ri)
}

func requestInfoFromContext(ctx context.Context) (requestInfo, bool) {
	ri, ok := ctx.Value(requestCtxKey{}).(requestInfo)
	return ri, ok
}

// tokenSource says which transport a request's credential arrived on. It
// exists only inside Middleware/extractToken — Context.Client (the
// db.SessionClient-typed field other packages read) is derived from it.
type tokenSource int

const (
	sourceBearer tokenSource = iota
	sourceCookie
)

func (s tokenSource) asClient() db.SessionClient {
	if s == sourceCookie {
		return db.SessionClientWeb
	}
	return db.SessionClientMobile
}

// extractToken reads the session token from the request: the Authorization
// header takes priority when present (an explicit, deliberate credential —
// never something a browser attaches automatically), falling back to the
// savdo_session cookie otherwise (docs/05-API.md § Conventions).
func extractToken(r *http.Request) (token string, source tokenSource, ok bool) {
	if h := r.Header.Get("Authorization"); h != "" {
		if rest, found := strings.CutPrefix(h, "Bearer "); found && rest != "" {
			return rest, sourceBearer, true
		}
	}
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		return c.Value, sourceCookie, true
	}
	return "", 0, false
}

// safeMethods are the HTTP methods CSRF enforcement exempts (docs/05-API.md
// § Conventions: "mutating requests require the X-Requested-With: savdo
// header").
var safeMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodOptions: true,
}

// csrfRequired reports whether a request authenticated via source, using
// method, must carry the X-Requested-With header: only cookie-authenticated
// requests are exposed to CSRF (a bearer token is never attached by a
// browser automatically), and only for methods that mutate state.
func csrfRequired(source tokenSource, method string) bool {
	return source == sourceCookie && !safeMethods[method]
}

// csrfError is the 403 FORBIDDEN Middleware returns when csrfRequired is
// true and the header is missing or wrong, naming the reason so a client
// (or a test) can tell this apart from a permission-based Forbidden.
func csrfError() error {
	return &apierr.Error{
		Status:  http.StatusForbidden,
		Code:    gen.FORBIDDEN,
		Details: map[string]any{"reason": "csrf"},
	}
}

// clientIP extracts the caller's address for session logging and rate
// limiting. It only trusts X-Forwarded-For when prod is true (i.e.
// cfg.Env == "prod", behind Caddy — docs/03-ARCHITECTURE.md); in dev, or
// when the header is absent, it falls back to r.RemoteAddr. A value that
// fails to parse as an IP (a malformed header, a test's "example.com"
// RemoteAddr) yields nil rather than an error — callers already treat a
// nil *netip.Addr as "unknown", matching the nullable `ip inet` column.
//
// X-Forwarded-For is append-only: each proxy in the chain appends the peer
// address it saw to the end of whatever value it received (RFC 7239 calls
// this the "for" parameter chain). That means the FIRST entry is whatever
// the original connecting client put there — attacker-controlled, since
// nothing stops a client from sending its own X-Forwarded-For with an
// arbitrary value — while the LAST entry is the address the nearest
// trusted hop actually observed on its own TCP connection, which a client
// cannot forge. Reading [0] (the first hop) lets a client bypass the
// per-IP rate limit and forge sessions.ip by spoofing that header; reading
// the last hop instead is what a single reverse proxy directly in front
// of this API requires. This holds only as long as Caddy (Phase 8,
// docs/07-DEVOPS.md) is configured as that single trusted hop — i.e. it
// must always append its own observed peer address as the last entry
// (Caddy's default `reverse_proxy` behavior) rather than passing an
// upstream value through unchanged — otherwise a client-controlled proxy
// ahead of Caddy could still forge the last entry.
func clientIP(r *http.Request, prod bool) *netip.Addr {
	raw := ""
	if prod {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			raw = lastForwardedHop(xff)
		}
	}
	if raw == "" {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			raw = r.RemoteAddr
		} else {
			raw = host
		}
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return nil
	}
	return &addr
}

// lastForwardedHop returns the last comma-separated entry of an
// X-Forwarded-For header value, trimmed — see clientIP's doc comment for
// why the last entry, not the first, is the one a trusted proxy actually
// controls.
func lastForwardedHop(xff string) string {
	parts := strings.Split(xff, ",")
	return strings.TrimSpace(parts[len(parts)-1])
}

// Middleware is the gen.StrictMiddlewareFunc wired into every operation
// (internal/httpx.NewRouter). For the three allow-listed operations it
// only stashes requestInfo and passes the request through unauthenticated.
// For every other operation it:
//  1. extracts the token (cookie or bearer),
//  2. resolves it to a live session + user via GetSessionByTokenHash,
//  3. rejects (401 UNAUTHENTICATED) a missing/unknown/expired/revoked
//     session or an inactive user — the exact same response regardless of
//     which of those it was, per docs/05-API.md,
//  4. enforces CSRF on a cookie-authenticated mutating request,
//  5. slides the session's expiry when it has gone quiet for over a
//     minute (D-29), and
//  6. attaches the auth.Context the rest of the request reads via
//     FromContext.
//
// CSRF runs before the sliding-expiry touch (authorize, then mutate): a
// request that fails the CSRF check is rejected outright, so it must never
// have the side effect of extending the session it rode in on — otherwise
// a CSRF-blocked request would still keep an otherwise-idle session alive.
func (s *Service) Middleware(f gen.StrictHandlerFunc, operationID string) gen.StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
		ctx = withRequestInfo(ctx, requestInfo{
			w:         w,
			userAgent: r.UserAgent(),
			ip:        clientIP(r, s.cfg.Env == "prod"),
		})

		if allowlistedOperations[operationID] {
			return f(ctx, w, r, request)
		}

		token, source, ok := extractToken(r)
		if !ok {
			return nil, apierr.Unauthenticated()
		}

		row, err := s.q.GetSessionByTokenHash(ctx, hashToken(token))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, apierr.Unauthenticated()
			}
			return nil, fmt.Errorf("auth: look up session: %w", err)
		}
		if !row.IsActive {
			return nil, apierr.Unauthenticated()
		}

		if csrfRequired(source, r.Method) && r.Header.Get(csrfHeader) != csrfHeaderValue {
			return nil, csrfError()
		}

		if needsTouch(row.LastSeenAt, time.Now()) {
			newExpiry := time.Now().Add(ttlFor(row.Client, s.cfg))
			if err := s.q.TouchSession(ctx, db.TouchSessionParams{
				ShopID:    row.ShopID,
				ID:        row.ID,
				ExpiresAt: newExpiry,
			}); err != nil {
				return nil, fmt.Errorf("auth: touch session: %w", err)
			}
		}

		ctx = WithContext(ctx, Context{
			ShopID:    row.ShopID,
			UserID:    row.UserID,
			Role:      row.Role,
			SessionID: row.ID,
			Client:    source.asClient(),
		})

		return f(ctx, w, r, request)
	}
}
