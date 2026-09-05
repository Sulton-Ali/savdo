// Package sales implements Phase 4's quick sale (correctness-critical,
// docs/03-ARCHITECTURE.md § Key flows › Quick sale; docs/04-DATA-MODEL.md
// § 4; ADR-006, ADR-007, ADR-010, ADR-013, ADR-014). This task (T3) covers
// `POST /sales` (CreateSale), `GET /sales/{id}` (GetSale) and `GET /sales`
// (ListSales); void and return are a later task and are not implemented
// here (httpx/unimplemented.go still stubs them).
//
// A sale's line prices and unit costs are always resolved server-side from
// the catalogue (D-56, hard rule 8) — never trusted from the client — and
// every line's stock leaves through stock.Service/stock.Move (hard rule 2,
// ADR-006), the same primitive purchases and adjustments already use.
package sales

import (
	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// Service holds sales' dependencies: q, for GetSale/ListSales's plain
// reads and for what CreateSaleTx is handed as qtx by httpx.Idempotent
// (never h.svc.q directly — CreateSale's whole write runs on the one
// transaction that helper opens, per its own "single connection" doc
// comment). No pool and no *stock.Service field: this task's three
// operations never open their own transaction (CreateSale's always comes
// from httpx.Idempotent) and every sale_out movement goes through the
// package-level stock.Move(ctx, qtx, ...), which takes the caller's own
// qtx, not a *stock.Service instance. The void/return task adds whatever
// dependency it needs (e.g. pool, for its own non-idempotent transaction,
// the same way stock.Service's pool backs CancelPurchase) when it lands,
// rather than this task pre-wiring it unused.
type Service struct {
	q *db.Queries
}

// NewService builds the sales Service.
func NewService(q *db.Queries) *Service {
	return &Service{q: q}
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
// and ListSales are called directly; CreateSale's Idempotency-Key replay
// wrapping lives in internal/httpx (httpx.Idempotent), the same split
// stock.Handler/httpx.CreateStockAdjustment and
// stock.Handler/httpx.ReceivePurchase already use, for the same reason:
// internal/httpx already imports internal/sales to wire the router, so the
// reverse import would cycle. httpx/sales.go calls Handler.CreateSaleTx
// (a plain (gen.Sale, error) method taking the transaction Idempotent
// opened) for the actual write.
type Handler struct {
	svc *Service
}

// NewHandler wraps svc for the strict server interface.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}
