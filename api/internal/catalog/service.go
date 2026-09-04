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
// transaction) and the shop's default locale, resolved once at startup
// (O-11: single-shop MVP) rather than re-queried per request.
type Service struct {
	pool          *pgxpool.Pool
	q             *db.Queries
	defaultLocale string
}

// NewService builds the catalog Service. defaultLocale is the shop's
// `default_locale` column, used as the locale-fallback target and as the
// locale a Create's `translations` must include an entry for.
func NewService(pool *pgxpool.Pool, q *db.Queries, defaultLocale string) *Service {
	return &Service{pool: pool, q: q, defaultLocale: defaultLocale}
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
