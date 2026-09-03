package httpx

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// notImplementedResponse satisfies every Phase 1 *ResponseObject interface
// the strict server needs so `server` compiles against `gen.StrictServerInterface`
// before each operation's owning module lands. It always writes the shared
// `Error` envelope (ADR-013) with code INTERNAL and `details.reason:
// "not_implemented"` — there is no dedicated "not implemented" ErrorCode
// (docs/05-API.md § Conventions lists the full enum), and the spec declares
// no 501 for any of these operations.
//
// Each method here corresponds to one contracts/openapi.yaml operationId.
// Phase 1: replaced by T3/T4/T5 as the auth, shop/locations and staff
// handlers are implemented; delete the method here when its real handler
// lands.
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

func (r notImplementedResponse) VisitGetShopResponse(w http.ResponseWriter) error { return r.write(w) }
func (r notImplementedResponse) VisitUpdateShopResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitListLocationsResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitCreateLocationResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitUpdateLocationResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitListStaffResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitCreateStaffResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitUpdateStaffResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitSetStaffPasswordResponse(w http.ResponseWriter) error {
	return r.write(w)
}

// GetShop returns the current shop's settings.
// Phase 1: replaced by T3/T4/T5.
func (server) GetShop(_ context.Context, _ gen.GetShopRequestObject) (gen.GetShopResponseObject, error) {
	return notImplementedResponse{}, nil
}

// UpdateShop updates shop settings.
// Phase 1: replaced by T3/T4/T5.
func (server) UpdateShop(_ context.Context, _ gen.UpdateShopRequestObject) (gen.UpdateShopResponseObject, error) {
	return notImplementedResponse{}, nil
}

// ListLocations lists the shop's locations.
// Phase 1: replaced by T3/T4/T5.
func (server) ListLocations(_ context.Context, _ gen.ListLocationsRequestObject) (gen.ListLocationsResponseObject, error) {
	return notImplementedResponse{}, nil
}

// CreateLocation creates a location.
// Phase 1: replaced by T3/T4/T5.
func (server) CreateLocation(_ context.Context, _ gen.CreateLocationRequestObject) (gen.CreateLocationResponseObject, error) {
	return notImplementedResponse{}, nil
}

// UpdateLocation updates a location.
// Phase 1: replaced by T3/T4/T5.
func (server) UpdateLocation(_ context.Context, _ gen.UpdateLocationRequestObject) (gen.UpdateLocationResponseObject, error) {
	return notImplementedResponse{}, nil
}

// ListStaff lists the shop's staff.
// Phase 1: replaced by T3/T4/T5.
func (server) ListStaff(_ context.Context, _ gen.ListStaffRequestObject) (gen.ListStaffResponseObject, error) {
	return notImplementedResponse{}, nil
}

// CreateStaff creates a staff member.
// Phase 1: replaced by T3/T4/T5.
func (server) CreateStaff(_ context.Context, _ gen.CreateStaffRequestObject) (gen.CreateStaffResponseObject, error) {
	return notImplementedResponse{}, nil
}

// UpdateStaff updates a staff member.
// Phase 1: replaced by T3/T4/T5.
func (server) UpdateStaff(_ context.Context, _ gen.UpdateStaffRequestObject) (gen.UpdateStaffResponseObject, error) {
	return notImplementedResponse{}, nil
}

// SetStaffPassword sets a staff member's password.
// Phase 1: replaced by T3/T4/T5.
func (server) SetStaffPassword(_ context.Context, _ gen.SetStaffPasswordRequestObject) (gen.SetStaffPasswordResponseObject, error) {
	return notImplementedResponse{}, nil
}
