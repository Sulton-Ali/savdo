// Package catalog implements the Phase 2 catalogue: units, attribute
// definitions, categories, products, variants and product images
// (docs/03-ARCHITECTURE.md § Module map; docs/04-DATA-MODEL.md § 2). Every
// operation reads its tenant boundary from auth.FromContext (ADR-004) —
// never from a path, query or body parameter — writes are gated by
// auth.PermCatalogWrite, and cost fields (`costPrice`/`costOverride`) and
// full translations are shaped out of a response for a caller without
// auth.PermCostRead/auth.PermCatalogWrite in the service layer, never
// filtered client-side (ADR-010, § 8 rule 8: separate ForStaff/ForCashier
// queries, never one query filtered in Go).
package catalog

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// Service holds catalog's dependencies: the pool (needed for the
// multi-statement writes — product+translations+implicit variant,
// category slug retry, image cover swap — that must run in one
// transaction), the shop's default locale, resolved once at startup
// (O-11: single-shop MVP) rather than re-queried per request, and the
// media base URL product images resolve their thumb/card/full links
// against (media.URLs — see images.go).
type Service struct {
	pool          *pgxpool.Pool
	q             *db.Queries
	defaultLocale string
	mediaBaseURL  string
	invalidator   Invalidator
}

// Invalidator is the public-cache-clearing side effect a catalogue write
// (product/category/variant/image) triggers (O-20: "cleared for the shop
// on ... product/category writes" — extended here to variant and image
// writes too, since both change what GET /public/products/{slug} shows).
// Declared here, the consumer, so wiring *public.Service in via
// SetInvalidator never makes this package import internal/public — see
// content.Invalidator's doc comment for the cycle this avoids.
type Invalidator interface {
	Invalidate(shopID uuid.UUID)
}

// NewService builds the catalog Service. defaultLocale is the shop's
// `default_locale` column, used as the locale-fallback target and as the
// locale a Create's `translations` must include an entry for.
// mediaBaseURL is Config.MediaBaseURL (the same value media.Service is
// constructed with), injected rather than read from the environment here
// so catalog has exactly one source of truth for it, shared with the
// media module.
func NewService(pool *pgxpool.Pool, q *db.Queries, defaultLocale, mediaBaseURL string) *Service {
	return &Service{pool: pool, q: q, defaultLocale: defaultLocale, mediaBaseURL: mediaBaseURL}
}

// SetInvalidator wires inv as the public-cache invalidator every product/
// category/variant/image write calls after committing. Optional
// (cmd/api's own wiring is the only caller today); a nil invalidator —
// the zero value, or any test that never calls this — makes
// invalidatePublic a no-op, so nothing outside cmd/api needs to know this
// exists.
func (s *Service) SetInvalidator(inv Invalidator) {
	s.invalidator = inv
}

// invalidatePublic clears shopID's public-response cache, when an
// Invalidator is wired at all.
func (s *Service) invalidatePublic(shopID uuid.UUID) {
	if s.invalidator != nil {
		s.invalidator.Invalidate(shopID)
	}
}

// Handler implements catalog's 21 gen.StrictServerInterface operations by
// delegating to a Service. internal/httpx forwards to it (catalog.go),
// the same shape shop.Handler/shop.Service uses.
type Handler struct {
	svc *Service
}

// NewHandler wraps svc for the strict server interface.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// newID mints a UUID v7 (time-ordered, docs/03-ARCHITECTURE.md §
// Cross-cutting: "IDs"), falling back to a v4 in the practically
// unreachable case the v7 generator's clock read fails — mirrors
// shop.newID/auth.Service.Login's own id generation.
func newID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New()
	}
	return id
}

// perms bundles the two role-dependent capabilities every catalog read
// shapes its response by (§ 8 rule 8, ADR-010): includeCost gates
// `costPrice`/`costOverride`, includeTranslations gates the full
// `translations` map. Computed once per request from auth.FromContext,
// never re-derived per field.
type perms struct {
	includeCost         bool
	includeTranslations bool
}

// permsFromContext reads ctx's role and reports what it may see. Callers
// that also need auth.Context's ShopID call auth.FromContext themselves —
// this only answers the "what fields" question.
func permsFromContext(ctx context.Context) perms {
	return perms{
		includeCost:         auth.Require(ctx, auth.PermCostRead) == nil,
		includeTranslations: auth.Require(ctx, auth.PermCatalogWrite) == nil,
	}
}
