package httpx

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// notImplementedResponse satisfies every Phase 4 T0 *ResponseObject
// interface the strict server needs so `server` keeps satisfying
// `gen.StrictServerInterface` for the 12 customers/sales/reports
// operations the contract just gained (docs/06-ROADMAP.md Phase 4),
// before each operation's owning module lands. It always writes the
// shared `Error` envelope (ADR-013) with code INTERNAL and
// `details.reason: "not_implemented"` — same shape and same rationale
// as Phase 1, 2 and 3's unimplemented.go (git history).
//
// Each method here corresponds to one contracts/openapi.yaml operationId.
// Replaced as the customers and sales handlers land in their own
// modules; delete the method here when its real handler lands.
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

func (r notImplementedResponse) VisitListCustomersResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitCreateCustomerResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitGetCustomerResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitUpdateCustomerResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitDeleteCustomerResponse(w http.ResponseWriter) error {
	return r.write(w)
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
func (r notImplementedResponse) VisitGetSalesSummaryReportResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitListSalesByProductResponse(w http.ResponseWriter) error {
	return r.write(w)
}

// ListCustomers lists the shop's customers.
// Phase 4 T0: replaced once the customers module lands.
func (server) ListCustomers(_ context.Context, _ gen.ListCustomersRequestObject) (gen.ListCustomersResponseObject, error) {
	return notImplementedResponse{}, nil
}

// CreateCustomer creates a customer.
// Phase 4 T0: replaced once the customers module lands.
func (server) CreateCustomer(_ context.Context, _ gen.CreateCustomerRequestObject) (gen.CreateCustomerResponseObject, error) {
	return notImplementedResponse{}, nil
}

// GetCustomer gets a customer.
// Phase 4 T0: replaced once the customers module lands.
func (server) GetCustomer(_ context.Context, _ gen.GetCustomerRequestObject) (gen.GetCustomerResponseObject, error) {
	return notImplementedResponse{}, nil
}

// UpdateCustomer updates a customer.
// Phase 4 T0: replaced once the customers module lands.
func (server) UpdateCustomer(_ context.Context, _ gen.UpdateCustomerRequestObject) (gen.UpdateCustomerResponseObject, error) {
	return notImplementedResponse{}, nil
}

// DeleteCustomer soft-deletes a customer.
// Phase 4 T0: replaced once the customers module lands.
func (server) DeleteCustomer(_ context.Context, _ gen.DeleteCustomerRequestObject) (gen.DeleteCustomerResponseObject, error) {
	return notImplementedResponse{}, nil
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

// GetSalesSummaryReport reports sales totals for a period.
// Phase 4 T0: replaced once the reports module lands.
func (server) GetSalesSummaryReport(_ context.Context, _ gen.GetSalesSummaryReportRequestObject) (gen.GetSalesSummaryReportResponseObject, error) {
	return notImplementedResponse{}, nil
}

// ListSalesByProduct reports sales by product for a period.
// Phase 4 T0: replaced once the reports module lands.
func (server) ListSalesByProduct(_ context.Context, _ gen.ListSalesByProductRequestObject) (gen.ListSalesByProductResponseObject, error) {
	return notImplementedResponse{}, nil
}
