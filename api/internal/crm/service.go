// Package crm implements the Phase 3 suppliers directory
// (docs/03-ARCHITECTURE.md § Module map: "crm owns customers, suppliers";
// docs/04-DATA-MODEL.md § 5). Customers ship in a later phase — this
// package's only table today is suppliers. Every operation reads its
// tenant boundary from auth.FromContext (ADR-004) and is gated by
// auth.PermSuppliersManage (owner/manager, docs/04-DATA-MODEL.md § 7).
package crm

import (
	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// Service holds crm's dependencies. Every supplier operation is a single
// statement (no multi-table write needs a transaction), so unlike
// stock.Service/shop.Service there is no pool field here — q is enough.
type Service struct {
	q *db.Queries
}

// NewService builds the crm Service.
func NewService(q *db.Queries) *Service {
	return &Service{q: q}
}

// Handler implements crm's slice of gen.StrictServerInterface (the five
// /suppliers operations).
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
// catalog.newID/shop.newID/stock.newID's own id generation.
func newID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New()
	}
	return id
}
