package httpx

import (
	"context"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// The nine methods below satisfy gen.StrictServerInterface's `/shop`,
// `/locations` and `/staff` operations by forwarding to server.shop
// (*shop.Handler). They can't be promoted the way *auth.Handler's methods
// are (see the server struct's doc comment in healthz.go) because
// *auth.Handler and *shop.Handler are both named "Handler" — embedding
// both anonymously would collide on that field name — so this file is
// the "combined handler in httpx" the module's task scope calls for:
// one small, typed, mechanical forwarding layer, replacing what
// unimplemented.go stubbed out before shop's own module existed.

// GetShop returns the current shop's settings.
func (s server) GetShop(ctx context.Context, req gen.GetShopRequestObject) (gen.GetShopResponseObject, error) {
	return s.shop.GetShop(ctx, req)
}

// UpdateShop updates shop settings.
func (s server) UpdateShop(ctx context.Context, req gen.UpdateShopRequestObject) (gen.UpdateShopResponseObject, error) {
	return s.shop.UpdateShop(ctx, req)
}

// ListLocations lists the shop's locations.
func (s server) ListLocations(ctx context.Context, req gen.ListLocationsRequestObject) (gen.ListLocationsResponseObject, error) {
	return s.shop.ListLocations(ctx, req)
}

// CreateLocation creates a location.
func (s server) CreateLocation(ctx context.Context, req gen.CreateLocationRequestObject) (gen.CreateLocationResponseObject, error) {
	return s.shop.CreateLocation(ctx, req)
}

// UpdateLocation updates a location.
func (s server) UpdateLocation(ctx context.Context, req gen.UpdateLocationRequestObject) (gen.UpdateLocationResponseObject, error) {
	return s.shop.UpdateLocation(ctx, req)
}

// ListStaff lists the shop's staff.
func (s server) ListStaff(ctx context.Context, req gen.ListStaffRequestObject) (gen.ListStaffResponseObject, error) {
	return s.shop.ListStaff(ctx, req)
}

// CreateStaff creates a staff member.
func (s server) CreateStaff(ctx context.Context, req gen.CreateStaffRequestObject) (gen.CreateStaffResponseObject, error) {
	return s.shop.CreateStaff(ctx, req)
}

// UpdateStaff updates a staff member.
func (s server) UpdateStaff(ctx context.Context, req gen.UpdateStaffRequestObject) (gen.UpdateStaffResponseObject, error) {
	return s.shop.UpdateStaff(ctx, req)
}

// SetStaffPassword sets a staff member's password.
func (s server) SetStaffPassword(ctx context.Context, req gen.SetStaffPasswordRequestObject) (gen.SetStaffPasswordResponseObject, error) {
	return s.shop.SetStaffPassword(ctx, req)
}
