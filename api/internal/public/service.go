// Package public implements Phase 6's unauthenticated landing endpoints
// (docs/03-ARCHITECTURE.md § Module map; docs/05-API.md § "Content and
// public"; D-99, D-103, D-105, D-106; O-20/O-21/O-22): shop identity +
// content blocks, active categories, a filterable/paginated product list
// and one product by slug. Every operation resolves its own tenant
// boundary from PUBLIC_SHOP_SLUG (D-105) via resolveShop — never from
// auth.FromContext, which this package never reads (there is no session
// to read: every GetPublic*/ListPublic* operation is allow-listed past
// auth.Service.Middleware, api/internal/auth/middleware.go). Responses
// are cached in-process for 60 s per (shop, path, query, resolved
// locale) with a strong ETag (cache.go), cleared for a shop by
// Invalidate — content.Service.Upsert and catalog.Service's
// product/category/variant/image writes call it via their own
// Invalidator interfaces (see those packages' doc comments for why the
// interface lives there, not here: avoiding a content/catalog <-> public
// import cycle).
package public

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/content"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// cacheTTL is O-20's "TTL 60 s" for both the resolved-shop lookup and
// every cached response body.
const cacheTTL = 60 * time.Second

// Service holds public's dependencies: q for the read-only queries every
// operation runs, contentSvc for GET /public/shop's content blocks
// (content.Service.Resolve), publicShopSlug (Config.PublicShopSlug,
// D-105) and mediaBaseURL (Config.MediaBaseURL, the same value
// media.Service/catalog.Service resolve image URLs against).
type Service struct {
	q              *db.Queries
	contentSvc     *content.Service
	publicShopSlug string
	mediaBaseURL   string

	shopMu        sync.RWMutex
	shop          db.Shop
	shopResolved  bool
	shopExpiresAt time.Time

	respMu    sync.RWMutex
	resp      map[string]cacheEntry
	respBytes int // running total of len(entry.body) across resp; see cacheSet/evictLocked
}

// NewService builds the public Service.
func NewService(q *db.Queries, contentSvc *content.Service, publicShopSlug, mediaBaseURL string) *Service {
	return &Service{q: q, contentSvc: contentSvc, publicShopSlug: publicShopSlug, mediaBaseURL: mediaBaseURL}
}

// Handler implements public's four gen.StrictServerInterface operations
// (GetPublicShop, ListPublicCategories, ListPublicProducts,
// GetPublicProductBySlug — handler.go) by delegating to a Service.
type Handler struct {
	svc *Service
}

// NewHandler wraps svc for the strict server interface.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// resolveShop resolves PUBLIC_SHOP_SLUG to its shop row, cached in
// process for cacheTTL — "resolve the shop by slug per request (cached
// with the response cache)" (the task spec this package implements): a
// cache hit never touches the database at all, and a miss queries once
// and refreshes the cache for every request in the next 60 s, the same
// staleness budget every other public response accepts. Returns
// apierr.NotFound("shop") when the slug names no shop — the 404 every
// public operation surfaces, and the condition cmd/api's startup warm-up
// (WarmShop) logs instead of failing on.
func (s *Service) resolveShop(ctx context.Context) (db.Shop, error) {
	s.shopMu.RLock()
	if s.shopResolved && time.Now().Before(s.shopExpiresAt) {
		shop := s.shop
		s.shopMu.RUnlock()
		return shop, nil
	}
	s.shopMu.RUnlock()

	row, err := s.q.GetShopBySlug(ctx, s.publicShopSlug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Shop{}, apierr.NotFound("shop")
		}
		return db.Shop{}, fmt.Errorf("public: get shop %q: %w", s.publicShopSlug, err)
	}

	s.shopMu.Lock()
	s.shop = row
	s.shopResolved = true
	s.shopExpiresAt = time.Now().Add(cacheTTL)
	s.shopMu.Unlock()
	return row, nil
}

// WarmShop resolves PUBLIC_SHOP_SLUG once, for cmd/api's startup
// warning (docs/07-DEVOPS.md-described "unknown slug -> 404 NOT_FOUND
// and a startup warning log"): unlike auth's ShopSlug (config.go),
// api/cmd/api/main.go never fails startup over this — a bad
// PUBLIC_SHOP_SLUG degrades only the unauthenticated landing surface,
// not the whole API — it just logs whatever error WarmShop returns.
func (s *Service) WarmShop(ctx context.Context) error {
	_, err := s.resolveShop(ctx)
	return err
}

// Invalidate clears every cached response for shopID — content.Invalidator
// and catalog.Invalidator's single method, so *Service satisfies both
// interfaces structurally without either package importing this one. It
// also drops the memoised shop row (resolveShop's own shopMu/shop/
// shopResolved/shopExpiresAt) when it names the same shop, so a shop
// identity write (name, currency, defaultLocale, timezone —
// shop.Service.UpdateShop) is picked up by the very next request rather
// than waiting out resolveShop's own cacheTTL on top of the response
// cache's — the two TTLs would otherwise stack, doubling the worst-case
// staleness this method exists to bound.
//
// shop.Service itself is deliberately NOT wired to call this (unlike
// content.Service/catalog.Service, wired in cmd/api/main.go): doing so
// would need internal/shop to depend on an Invalidator interface the way
// content/catalog do, touching a third module for a task scoped to
// internal/public (and internal/db's stock/products queries) — a shop
// identity edit is rare, administrative, and already bounded by
// resolveShop's own cacheTTL (60 s, the same staleness budget O-20
// already accepts for every other public response); this comment
// documents that choice rather than making it silently.
func (s *Service) Invalidate(shopID uuid.UUID) {
	s.respMu.Lock()
	for key, entry := range s.resp {
		if entry.shopID == shopID {
			s.respBytes -= len(entry.body)
			delete(s.resp, key)
		}
	}
	s.respMu.Unlock()

	s.shopMu.Lock()
	if s.shopResolved && s.shop.ID == shopID {
		s.shopResolved = false
	}
	s.shopMu.Unlock()
}
