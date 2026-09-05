// Package reports implements the two Phase 4 sales reports (D-55):
// GET /reports/sales/summary and GET /reports/sales/by-product. Both read
// from the sales ledger sqlc already exposes (api/db/queries/reports.sql:
// SalesSummaryForStaff, SalesSummaryForCashier, SalesByProduct) — this
// package's own job is the role split (manager+ vs. cashier, ADR-010), the
// shop-timezone calendar-day math (§ 04-DATA-MODEL.md rule 9: timestamps
// are UTC, the shop timezone is applied in reports only) and converting
// the query rows to the contract's wire shapes. GET /stock/low is the
// third report the owner asked for (D-55) — it needs no work here, it
// already exists (D-65).
//
// Every operation reads shop_id from auth.FromContext (ADR-004), never
// from a query parameter.
package reports

import (
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// Service holds reports' one dependency. Both reports are read-only
// aggregate queries — no multi-statement write ever needs a transaction —
// so, like crm.Service, there is no pool field here.
type Service struct {
	q *db.Queries
}

// NewService builds the reports Service.
func NewService(q *db.Queries) *Service {
	return &Service{q: q}
}

// Handler implements reports' slice of gen.StrictServerInterface (the two
// `/reports/sales/*` operations).
type Handler struct {
	svc *Service
}

// NewHandler wraps svc for the strict server interface.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}
