package httpx

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// notImplementedResponse satisfies every Phase 3 T1 *ResponseObject
// interface the strict server needs so `server` compiles against
// `gen.StrictServerInterface` before each operation's owning module lands
// (docs/06-ROADMAP.md Phase 3). It always writes the shared `Error`
// envelope (ADR-013) with code INTERNAL and `details.reason:
// "not_implemented"` — same shape and same rationale as Phase 1's and
// Phase 2's unimplemented.go (git history), which this file re-creates
// for the 17 suppliers/purchases/stock/image-retag operations T1 adds to
// the contract.
//
// Each method here corresponds to one contracts/openapi.yaml operationId.
// Replaced as the supplier, purchase and stock handlers land in their own
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

func (r notImplementedResponse) VisitListSuppliersResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitCreateSupplierResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitGetSupplierResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitUpdateSupplierResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitDeleteSupplierResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitListPurchasesResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitCreatePurchaseResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitGetPurchaseResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitUpdatePurchaseResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitReceivePurchaseResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitCancelPurchaseResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitListStockLevelsResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitListStockMovementsResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitCreateStockAdjustmentResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitCreateStockTransferResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitListLowStockResponse(w http.ResponseWriter) error {
	return r.write(w)
}

// ListSuppliers lists the shop's suppliers.
// Phase 3 T1: replaced once the suppliers module lands.
func (server) ListSuppliers(_ context.Context, _ gen.ListSuppliersRequestObject) (gen.ListSuppliersResponseObject, error) {
	return notImplementedResponse{}, nil
}

// CreateSupplier creates a supplier.
// Phase 3 T1: replaced once the suppliers module lands.
func (server) CreateSupplier(_ context.Context, _ gen.CreateSupplierRequestObject) (gen.CreateSupplierResponseObject, error) {
	return notImplementedResponse{}, nil
}

// GetSupplier gets a supplier.
// Phase 3 T1: replaced once the suppliers module lands.
func (server) GetSupplier(_ context.Context, _ gen.GetSupplierRequestObject) (gen.GetSupplierResponseObject, error) {
	return notImplementedResponse{}, nil
}

// UpdateSupplier updates a supplier.
// Phase 3 T1: replaced once the suppliers module lands.
func (server) UpdateSupplier(_ context.Context, _ gen.UpdateSupplierRequestObject) (gen.UpdateSupplierResponseObject, error) {
	return notImplementedResponse{}, nil
}

// DeleteSupplier soft-deletes a supplier.
// Phase 3 T1: replaced once the suppliers module lands.
func (server) DeleteSupplier(_ context.Context, _ gen.DeleteSupplierRequestObject) (gen.DeleteSupplierResponseObject, error) {
	return notImplementedResponse{}, nil
}

// ListPurchases lists the shop's purchases.
// Phase 3 T1: replaced once the purchases module lands.
func (server) ListPurchases(_ context.Context, _ gen.ListPurchasesRequestObject) (gen.ListPurchasesResponseObject, error) {
	return notImplementedResponse{}, nil
}

// CreatePurchase creates a draft purchase.
// Phase 3 T1: replaced once the purchases module lands.
func (server) CreatePurchase(_ context.Context, _ gen.CreatePurchaseRequestObject) (gen.CreatePurchaseResponseObject, error) {
	return notImplementedResponse{}, nil
}

// GetPurchase gets a purchase.
// Phase 3 T1: replaced once the purchases module lands.
func (server) GetPurchase(_ context.Context, _ gen.GetPurchaseRequestObject) (gen.GetPurchaseResponseObject, error) {
	return notImplementedResponse{}, nil
}

// UpdatePurchase updates a draft purchase.
// Phase 3 T1: replaced once the purchases module lands.
func (server) UpdatePurchase(_ context.Context, _ gen.UpdatePurchaseRequestObject) (gen.UpdatePurchaseResponseObject, error) {
	return notImplementedResponse{}, nil
}

// ReceivePurchase receives a draft purchase, writing purchase_in stock
// movements. Phase 3 T1: replaced once the purchases/stock modules land.
func (server) ReceivePurchase(_ context.Context, _ gen.ReceivePurchaseRequestObject) (gen.ReceivePurchaseResponseObject, error) {
	return notImplementedResponse{}, nil
}

// CancelPurchase cancels a purchase, reversing stock movements if it was
// already received. Phase 3 T1: replaced once the purchases/stock
// modules land.
func (server) CancelPurchase(_ context.Context, _ gen.CancelPurchaseRequestObject) (gen.CancelPurchaseResponseObject, error) {
	return notImplementedResponse{}, nil
}

// ListStockLevels lists stock levels per variant and location.
// Phase 3 T1: replaced once the stock module lands.
func (server) ListStockLevels(_ context.Context, _ gen.ListStockLevelsRequestObject) (gen.ListStockLevelsResponseObject, error) {
	return notImplementedResponse{}, nil
}

// ListStockMovements lists the append-only stock movement ledger.
// Phase 3 T1: replaced once the stock module lands.
func (server) ListStockMovements(_ context.Context, _ gen.ListStockMovementsRequestObject) (gen.ListStockMovementsResponseObject, error) {
	return notImplementedResponse{}, nil
}

// CreateStockAdjustment records a manual stock adjustment.
// Phase 3 T1: replaced once the stock module lands.
func (server) CreateStockAdjustment(_ context.Context, _ gen.CreateStockAdjustmentRequestObject) (gen.CreateStockAdjustmentResponseObject, error) {
	return notImplementedResponse{}, nil
}

// CreateStockTransfer transfers stock between two locations.
// Phase 3 T1: replaced once the stock module lands.
func (server) CreateStockTransfer(_ context.Context, _ gen.CreateStockTransferRequestObject) (gen.CreateStockTransferResponseObject, error) {
	return notImplementedResponse{}, nil
}

// ListLowStock lists variants at or below their effective low-stock
// threshold. Phase 3 T1: replaced once the stock module lands.
func (server) ListLowStock(_ context.Context, _ gen.ListLowStockRequestObject) (gen.ListLowStockResponseObject, error) {
	return notImplementedResponse{}, nil
}
