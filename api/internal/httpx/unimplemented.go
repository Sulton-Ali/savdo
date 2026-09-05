package httpx

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// notImplementedResponse satisfies every Phase 4 T0 *ResponseObject
// interface the strict server needs so `server` keeps satisfying
// `gen.StrictServerInterface` for the sales operations the contract
// gained in Phase 4 (docs/06-ROADMAP.md), before the sales module lands.
// The five customers operations this file used to stub the same way are
// now implemented for real by T2's crm module, and the two
// `/reports/sales/*` operations by T5's reports.go. It always writes the
// shared `Error` envelope (ADR-013) with code INTERNAL and
// `details.reason: "not_implemented"` — same shape and same rationale as
// Phase 1, 2 and 3's unimplemented.go (git history).
//
// Each method here corresponds to one contracts/openapi.yaml operationId.
// Replaced as the sales module lands; delete the method here when its
// real handler lands.
type notImplementedResponse struct{}

func (notImplementedResponse) write(w http.ResponseWriter) error {
	body := gen.Error{}
	body.Error.Code = gen.INTERNAL
	details := map[string]interface{}{"reason": "not_implemented"}
	body.Error.Details = &details

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	return json.NewEncoder(w).Encode(body)
}

func (r notImplementedResponse) VisitListSalesResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitCreateSaleResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitGetSaleResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitVoidSaleResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitCreateSaleReturnResponse(w http.ResponseWriter) error {
	return r.write(w)
}

// ListSales lists the shop's sales.
// Phase 4 T0: replaced once the sales module lands.
func (server) ListSales(_ context.Context, _ gen.ListSalesRequestObject) (gen.ListSalesResponseObject, error) {
	return notImplementedResponse{}, nil
}

// CreateSale completes a quick sale.
// Phase 4 T0: replaced once the sales module lands.
func (server) CreateSale(_ context.Context, _ gen.CreateSaleRequestObject) (gen.CreateSaleResponseObject, error) {
	return notImplementedResponse{}, nil
}

// GetSale gets a sale.
// Phase 4 T0: replaced once the sales module lands.
func (server) GetSale(_ context.Context, _ gen.GetSaleRequestObject) (gen.GetSaleResponseObject, error) {
	return notImplementedResponse{}, nil
}

// VoidSale voids a completed sale.
// Phase 4 T0: replaced once the sales module lands.
func (server) VoidSale(_ context.Context, _ gen.VoidSaleRequestObject) (gen.VoidSaleResponseObject, error) {
	return notImplementedResponse{}, nil
}

// CreateSaleReturn returns part or all of a completed sale.
// Phase 4 T0: replaced once the sales module lands.
func (server) CreateSaleReturn(_ context.Context, _ gen.CreateSaleReturnRequestObject) (gen.CreateSaleReturnResponseObject, error) {
	return notImplementedResponse{}, nil
}
