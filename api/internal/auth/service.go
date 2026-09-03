package auth

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/config"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// Service holds auth's dependencies and business logic: password
// verification, session issuance/revocation and login rate limiting.
// handler.go's Handler translates between this and the generated strict
// server types; Middleware (middleware.go) is also a method on Service so
// it shares the same db.Queries and config.
type Service struct {
	q      *db.Queries
	cfg    config.Config
	shopID uuid.UUID

	ipLimiter   *loginLimiter
	userLimiter *loginLimiter
}

// NewService builds the auth Service for shopID — the single shop this
// MVP serves (docs/03-ARCHITECTURE.md § Auth spec: "Shop resolution
// (single-shop MVP, tenant-ready)"). cmd/api resolves shopID once at
// startup via GetShopBySlug(cfg.ShopSlug) and fails fast if it doesn't
// exist. A future multi-tenant version replaces this single injected
// shopID with per-request resolution from the host/slug (ADR-004) without
// changing anything below Login itself.
func NewService(q *db.Queries, cfg config.Config, shopID uuid.UUID) *Service {
	return &Service{
		q:           q,
		cfg:         cfg,
		shopID:      shopID,
		ipLimiter:   newLoginLimiter(cfg.LoginRateIPPerMin),
		userLimiter: newLoginLimiter(cfg.LoginRateUserPerMin),
	}
}

// LoginResult is what a successful Login produces: the user and session
// rows, and the raw (unhashed) token — the only place the raw token ever
// exists outside the client's own hands. handler.go decides what to do
// with Token (set it as a cookie for a web session, put it in the response
// body for a mobile one) and never logs it.
type LoginResult struct {
	User    db.User
	Session db.Session
	Token   string
}

// ipKey renders ip for use as a rate-limiter key, or a constant string
// when it is nil (unparseable RemoteAddr) — attempts with no identifiable
// IP still get bucketed together for the per-IP limit rather than bypassing
// it entirely.
func ipKey(ip *netip.Addr) string {
	if ip == nil {
		return "unknown"
	}
	return ip.String()
}

// userLimiterKey normalizes username for the per-username rate limiter.
// users.username is citext (case-insensitive) — GetUserByUsername already
// matches "Owner1" and "owner1" to the same row — so the limiter must key
// on the same normalized form; otherwise an attacker could multiply their
// allowed attempts against one account by varying the case of the
// username on each request.
func userLimiterKey(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

// retryAfterSeconds rounds d up to a whole number of seconds for the 429
// response's Retry-After (defined in seconds by HTTP, and apierr.RateLimited
// takes an int).
func retryAfterSeconds(d time.Duration) int {
	secs := int(d / time.Second)
	if d%time.Second != 0 {
		secs++
	}
	if secs < 1 {
		secs = 1
	}
	return secs
}

// Login authenticates username/password against this Service's shop and,
// on success, issues a new session for client. Every rejection path —
// rate limited aside — returns the exact same apierr.Unauthenticated(): an
// unknown username, a wrong password and an inactive user are
// indistinguishable to the caller (docs/05-API.md § Auth spec), and Verify
// always runs (against a dummy hash when the username doesn't exist) so
// the three cases also take the same time.
func (s *Service) Login(ctx context.Context, username, password string, client db.SessionClient, userAgent string, ip *netip.Addr) (LoginResult, error) {
	now := time.Now()

	if ok, retryAfter := s.ipLimiter.allow(ipKey(ip), now); !ok {
		return LoginResult{}, apierr.RateLimited(retryAfterSeconds(retryAfter))
	}
	if ok, retryAfter := s.userLimiter.allow(userLimiterKey(username), now); !ok {
		return LoginResult{}, apierr.RateLimited(retryAfterSeconds(retryAfter))
	}

	user, err := s.q.GetUserByUsername(ctx, db.GetUserByUsernameParams{ShopID: s.shopID, Username: username})
	found := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return LoginResult{}, fmt.Errorf("auth: look up user: %w", err)
	}

	hash := dummyHash()
	if found {
		hash = user.PasswordHash
	}

	verified, err := Verify(hash, password)
	if err != nil {
		return LoginResult{}, fmt.Errorf("auth: verify password: %w", err)
	}

	if !found || !verified || !user.IsActive {
		return LoginResult{}, apierr.Unauthenticated()
	}

	rawToken, tokenHash, err := newToken()
	if err != nil {
		return LoginResult{}, fmt.Errorf("auth: generate session token: %w", err)
	}

	sessionID, err := uuid.NewV7()
	if err != nil {
		sessionID = uuid.New()
	}

	var ua *string
	if truncated := truncateUserAgent(userAgent); truncated != "" {
		ua = &truncated
	}

	session, err := s.q.CreateSession(ctx, db.CreateSessionParams{
		ID:        sessionID,
		ShopID:    s.shopID,
		UserID:    user.ID,
		TokenHash: tokenHash,
		Client:    client,
		UserAgent: ua,
		Ip:        ip,
		ExpiresAt: now.Add(ttlFor(client, s.cfg)),
	})
	if err != nil {
		return LoginResult{}, fmt.Errorf("auth: create session: %w", err)
	}

	if err := s.q.SetUserLastLogin(ctx, db.SetUserLastLoginParams{ShopID: s.shopID, ID: user.ID}); err != nil {
		return LoginResult{}, fmt.Errorf("auth: set last login: %w", err)
	}

	return LoginResult{User: user, Session: session, Token: rawToken}, nil
}

// Logout revokes exactly the session identified by (shopID, sessionID,
// userID) — the one the current request authenticated with. RevokeSession
// is already a no-op (not an error) when the session is already revoked,
// so Logout is naturally idempotent.
func (s *Service) Logout(ctx context.Context, shopID, sessionID, userID uuid.UUID) error {
	if err := s.q.RevokeSession(ctx, db.RevokeSessionParams{ShopID: shopID, ID: sessionID, UserID: userID}); err != nil {
		return fmt.Errorf("auth: revoke session: %w", err)
	}
	return nil
}

// Me is what GetMe needs: the full user row (GetSessionByTokenHash's row,
// used to build the auth Context, only carries the subset of user columns
// needed to authenticate — not phone/lastLoginAt/createdAt) and the shop.
type Me struct {
	User db.User
	Shop db.Shop
}

// GetMe loads the authenticated user's full profile and their shop.
func (s *Service) GetMe(ctx context.Context, shopID, userID uuid.UUID) (Me, error) {
	user, err := s.q.GetUserByID(ctx, db.GetUserByIDParams{ShopID: shopID, ID: userID})
	if err != nil {
		return Me{}, fmt.Errorf("auth: load user: %w", err)
	}
	shop, err := s.q.GetShop(ctx, shopID)
	if err != nil {
		return Me{}, fmt.Errorf("auth: load shop: %w", err)
	}
	return Me{User: user, Shop: shop}, nil
}

// ListSessions returns every session belonging to (shopID, userID),
// newest first. The list is small (one browser, one phone, maybe a couple
// of stale ones) so, per docs/05-API.md's spec for this endpoint, the
// handler returns all of them on one page rather than actually paginating.
func (s *Service) ListSessions(ctx context.Context, shopID, userID uuid.UUID) ([]db.Session, error) {
	sessions, err := s.q.ListUserSessions(ctx, db.ListUserSessionsParams{ShopID: shopID, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("auth: list sessions: %w", err)
	}
	return sessions, nil
}

// RevokeSession revokes sessionID if it belongs to (shopID, userID),
// reporting found=false when it does not — the caller's own session,
// however, already-revoked included (idempotent), never another user's.
// There is no "get one session by id" query (only ListUserSessions and the
// token-keyed GetSessionByTokenHash), so existence is checked by scanning
// the caller's own session list, which the docstring on ListSessions notes
// is already expected to be small.
func (s *Service) RevokeSession(ctx context.Context, shopID, userID, sessionID uuid.UUID) (found bool, err error) {
	sessions, err := s.ListSessions(ctx, shopID, userID)
	if err != nil {
		return false, err
	}

	exists := false
	for _, sess := range sessions {
		if sess.ID == sessionID {
			exists = true
			break
		}
	}
	if !exists {
		return false, nil
	}

	if err := s.q.RevokeSession(ctx, db.RevokeSessionParams{ShopID: shopID, ID: sessionID, UserID: userID}); err != nil {
		return false, fmt.Errorf("auth: revoke session: %w", err)
	}
	return true, nil
}
