package shop

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
)

// Handler implements the nine `/shop`, `/locations` and `/staff`
// operations of gen.StrictServerInterface (GetShop/UpdateShop here;
// ListLocations/CreateLocation/UpdateLocation in handler_locations.go;
// ListStaff/CreateStaff/UpdateStaff/SetStaffPassword in
// handler_staff.go). internal/httpx forwards to it alongside auth.Handler
// in the composed strict server.
type Handler struct {
	svc *Service
}

// NewHandler wraps svc for the strict server interface.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// maxNameLength bounds shop name, location name and staff full name —
// none of these have a JSON Schema `maxLength` in the contract, so this
// is the one place all three agree on a limit rather than three
// independently-chosen numbers.
const maxNameLength = 120

// GetShop returns the current shop's settings. Any authenticated role.
func (h *Handler) GetShop(ctx context.Context, _ gen.GetShopRequestObject) (gen.GetShopResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}

	shopRow, err := h.svc.GetShop(ctx, authCtx.ShopID)
	if err != nil {
		return nil, fmt.Errorf("shop: get shop: %w", err)
	}
	return gen.GetShop200JSONResponse(toGenShop(shopRow)), nil
}

// UpdateShop updates shop settings. Requires shop.settings (owner only).
// currency and slug are immutable — ShopPatch has no fields for them, so
// there is nothing to reject; they simply can't be sent.
func (h *Handler) UpdateShop(ctx context.Context, req gen.UpdateShopRequestObject) (gen.UpdateShopResponseObject, error) {
	if _, ok := auth.FromContext(ctx); !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermShopSettings); err != nil {
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

	if body.Timezone != nil {
		if _, err := time.LoadLocation(*body.Timezone); err != nil {
			fields["timezone"] = "invalid"
		}
	}

	var defaultLocale *string
	if body.DefaultLocale != nil {
		if !body.DefaultLocale.Valid() {
			fields["defaultLocale"] = "invalid"
		} else {
			s := string(*body.DefaultLocale)
			defaultLocale = &s
		}
	}

	// D-44: the shop-wide default must be non-negative and fit the int32
	// column behind it — an absent field leaves the stored value
	// unchanged (UpdateShopInput.LowStockThreshold nil skips the COALESCE
	// narg entirely, same as every other field here).
	if body.LowStockThreshold != nil && (*body.LowStockThreshold < 0 || *body.LowStockThreshold > math.MaxInt32) {
		fields["lowStockThreshold"] = "invalid"
	}

	if len(fields) > 0 {
		return nil, apierr.Validation(fields)
	}

	updated, err := h.svc.UpdateShop(ctx, authCtx.ShopID, UpdateShopInput{
		Name:                 name,
		Timezone:             body.Timezone,
		DefaultLocale:        defaultLocale,
		AllowNegativeStock:   body.AllowNegativeStock,
		UpdateCostOnPurchase: body.UpdateCostOnPurchase,
		LowStockThreshold:    body.LowStockThreshold,
	})
	if err != nil {
		return nil, fmt.Errorf("shop: update shop: %w", err)
	}
	return gen.UpdateShop200JSONResponse(toGenShop(updated)), nil
}
