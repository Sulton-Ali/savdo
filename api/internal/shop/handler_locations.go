package shop

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// ListLocations lists the shop's locations. Any authenticated role.
func (h *Handler) ListLocations(ctx context.Context, req gen.ListLocationsRequestObject) (gen.ListLocationsResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}

	limit := clampLimit(req.Params.Limit)
	createdAt, id, err := decodeCursor(req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	cursorCreatedAt, cursorID := cursorPtr(createdAt, id)

	rows, err := h.svc.ListLocations(ctx, authCtx.ShopID, limit+1, cursorCreatedAt, cursorID)
	if err != nil {
		return nil, fmt.Errorf("shop: list locations: %w", err)
	}

	items, nextCursor := paginateT(rows, limit, func(l db.Location) (time.Time, uuid.UUID) { return l.CreatedAt, l.ID })
	genItems := make([]gen.Location, len(items))
	for i, l := range items {
		genItems[i] = toGenLocation(l)
	}
	return gen.ListLocations200JSONResponse(gen.LocationList{Items: genItems, NextCursor: nextCursorResponse(nextCursor)}), nil
}

// CreateLocation creates a location. Requires locations.manage (owner
// only).
func (h *Handler) CreateLocation(ctx context.Context, req gen.CreateLocationRequestObject) (gen.CreateLocationResponseObject, error) {
	if _, ok := auth.FromContext(ctx); !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermLocationsManage); err != nil {
		return nil, err
	}
	authCtx, _ := auth.FromContext(ctx)

	body := req.Body
	fields := map[string]string{}
	name := strings.TrimSpace(body.Name)
	switch {
	case name == "":
		fields["name"] = "required"
	case utf8.RuneCountInString(name) > maxNameLength:
		fields["name"] = "too_long"
	}

	var kind db.LocationKind
	switch {
	case body.Kind == "":
		fields["kind"] = "required"
	case !body.Kind.Valid():
		fields["kind"] = "invalid"
	default:
		kind = db.LocationKind(body.Kind)
	}

	if len(fields) > 0 {
		return nil, apierr.Validation(fields)
	}

	wantDefault := body.IsDefault != nil && *body.IsDefault

	loc, err := h.svc.CreateLocation(ctx, authCtx.ShopID, name, kind, wantDefault)
	if err != nil {
		return nil, err
	}
	return gen.CreateLocation201JSONResponse(toGenLocation(loc)), nil
}

// UpdateLocation updates a location. Requires locations.manage (owner
// only). 404 when id doesn't belong to this shop.
func (h *Handler) UpdateLocation(ctx context.Context, req gen.UpdateLocationRequestObject) (gen.UpdateLocationResponseObject, error) {
	if _, ok := auth.FromContext(ctx); !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermLocationsManage); err != nil {
		return nil, err
	}
	authCtx, _ := auth.FromContext(ctx)

	body := req.Body
	fields := map[string]string{}

	var name *string
	if body.Name != nil {
		trimmed := strings.TrimSpace(*body.Name)
		switch {
		case trimmed == "":
			fields["name"] = "required"
		case utf8.RuneCountInString(trimmed) > maxNameLength:
			fields["name"] = "too_long"
		default:
			name = &trimmed
		}
	}

	var kind *db.LocationKind
	if body.Kind != nil {
		if !body.Kind.Valid() {
			fields["kind"] = "invalid"
		} else {
			k := db.LocationKind(*body.Kind)
			kind = &k
		}
	}

	if len(fields) > 0 {
		return nil, apierr.Validation(fields)
	}

	updated, err := h.svc.UpdateLocation(ctx, authCtx.ShopID, req.Id, LocationPatchInput{
		Name: name, Kind: kind, IsDefault: body.IsDefault, IsActive: body.IsActive,
	})
	if err != nil {
		return nil, err
	}
	return gen.UpdateLocation200JSONResponse(toGenLocation(updated)), nil
}
