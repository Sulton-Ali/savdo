// Package stock implements the Phase 3 stock ledger
// (docs/03-ARCHITECTURE.md § Module map; docs/04-DATA-MODEL.md § 3,
// correctness-critical; ADR-006). Move is the only place in this codebase
// that ever writes stock_levels (hard rule 2); every other write in this
// package — adjustments, transfers — goes through it. Purchases and
// suppliers are a later task (T4); this package only provides the Move
// primitive T4's purchase-receive/cancel handlers will call, plus the
// `/stock/*` read and write endpoints and the `savdo stock rebuild` CLI
// this task's own scope covers.
package stock

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// Service holds stock's dependencies: pool for opening the transactions
// every write (Move, an adjustment, a transfer) runs inside, and q for
// reads that don't need one (ListStockLevels, ListStockMovements,
// ListLowStock).
type Service struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

// NewService builds the stock Service.
func NewService(pool *pgxpool.Pool, q *db.Queries) *Service {
	return &Service{pool: pool, q: q}
}

// Handler implements stock's gen.StrictServerInterface operations
// (ListStockLevels, ListStockMovements, CreateStockTransfer, ListLowStock
// directly; CreateAdjustment is called from httpx/stock.go's own
// CreateStockAdjustment, which wraps it with the Idempotency-Key replay
// helper — httpx.Idempotent — since that helper lives in internal/httpx
// and this package must not import it (internal/httpx already imports
// internal/stock to wire the router; the reverse import would cycle).
type Handler struct {
	svc *Service
}

// NewHandler wraps svc for the strict server interface.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// createdByName resolves userID's display name for a single-movement
// response (CreateAdjustment, CreateStockTransfer) — the list endpoint
// (ListStockMovements) gets it from ListMovementsWithCreatedByName's own
// join instead, so as not to pay one extra query per row. Returns nil,
// nil for a nil userID (a system-written movement — not expected from a
// handler, which always sets ActorID to the authenticated caller, but
// Move's own MoveParams allows it) or for a user that no longer exists
// (mirrors the sqlc query's LEFT JOIN: "createdBy is set but the name is
// unknown" is not an error, per the contract's own createdByName doc
// comment).
func (s *Service) createdByName(ctx context.Context, shopID uuid.UUID, userID *uuid.UUID) (*string, error) {
	if userID == nil {
		return nil, nil
	}
	user, err := s.q.GetUserByID(ctx, db.GetUserByIDParams{ShopID: shopID, ID: *userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("stock: get created-by user: %w", err)
	}
	return &user.FullName, nil
}

// newID mints a UUID v7 (time-ordered, docs/03-ARCHITECTURE.md §
// Cross-cutting: "IDs"), falling back to a v4 in the practically
// unreachable case the v7 generator's clock read fails — mirrors
// catalog.newID/shop.newID/audit.newID's own id generation.
func newID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New()
	}
	return id
}
