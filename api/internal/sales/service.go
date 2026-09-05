// Package sales implements Phase 4's quick sale, void and return
// (correctness-critical, docs/03-ARCHITECTURE.md § Key flows › Quick
// sale; docs/04-DATA-MODEL.md § 4; ADR-006, ADR-007, ADR-010, ADR-013,
// ADR-014): `POST /sales` (CreateSale), `GET /sales/{id}` (GetSale),
// `GET /sales` (ListSales), `POST /sales/{id}/void` (VoidSale) and
// `POST /sales/{id}/return` (CreateSaleReturn).
//
// A sale's line prices and unit costs are always resolved server-side from
// the catalogue (D-56, hard rule 8) — never trusted from the client — and
// every line's stock leaves through stock.Service/stock.Move (hard rule 2,
// ADR-006), the same primitive purchases and adjustments already use.
package sales

import (
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// Service holds sales' dependencies: q, for GetSale/ListSales's plain
// reads and for what CreateSaleTx/VoidSaleTx/CreateSaleReturnTx are each
// handed as qtx by httpx (never h.svc.q directly inside any of those —
// each one's whole write runs on the one transaction httpx opens, per
// CreateSaleTx's own "single connection" doc comment). No pool and no
// *stock.Service field: none of this package's write operations open
// their own transaction — httpx.CreateSale/CreateSaleReturn open theirs
// via httpx.Idempotent, httpx.VoidSale opens its own plain one (no
// Idempotency-Key in the contract for that operation) — and every
// movement goes through the package-level stock.Move(ctx, qtx, ...),
// which takes the caller's own qtx, not a *stock.Service instance. now
// backs every time-dependent decision this package's writers make —
// VoidSaleTx's D-59 void-window comparison and CreateSaleTx's D-56 promo
// activation check (resolveSaleItems' own promoActive call) — a field
// rather than a bare time.Now() call at each site so a test can pin one
// exact instant near a calendar-day boundary (WithNow) instead of a real
// run risking a flip across local midnight mid-test.
type Service struct {
	q   *db.Queries
	now func() time.Time
}

// Option configures a Service NewService builds — currently only WithNow,
// for tests; production callers pass none.
type Option func(*Service)

// WithNow overrides the clock VoidSaleTx reads (default time.Now) — for a
// test that must pin the "today" instant D-59's void window compares
// against, e.g. an instant deliberately close to local midnight.
func WithNow(now func() time.Time) Option {
	return func(s *Service) { s.now = now }
}

// NewService builds the sales Service.
func NewService(q *db.Queries, opts ...Option) *Service {
	s := &Service{q: q, now: time.Now}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// newID mints a UUID v7 (time-ordered, docs/03-ARCHITECTURE.md §
// Cross-cutting: "IDs"), falling back to a v4 in the practically
// unreachable case the v7 generator's clock read fails — mirrors
// stock.newID/catalog.newID/shop.newID/audit.newID's own id generation.
func newID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New()
	}
	return id
}

// Handler implements sales' gen.StrictServerInterface operations. GetSale
// and ListSales are called directly; CreateSale's and CreateSaleReturn's
// Idempotency-Key replay wrapping, and VoidSale's own (Idempotency-Key-
// less) transaction, both live in internal/httpx (httpx.Idempotent /
// httpx.VoidSale), the same split stock.Handler/httpx.CreateStockAdjustment
// and stock.Handler/httpx.ReceivePurchase already use, for the same
// reason: internal/httpx already imports internal/sales to wire the
// router, so the reverse import would cycle. httpx/sales.go calls
// Handler.CreateSaleTx/VoidSaleTx/CreateSaleReturnTx (plain
// (gen.Sale, error) methods taking the transaction httpx opened) for the
// actual writes.
type Handler struct {
	svc *Service
}

// NewHandler wraps svc for the strict server interface.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}
