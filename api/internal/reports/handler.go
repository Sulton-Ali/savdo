package reports

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// loadShop fetches shopID's row, wrapping the (practically unreachable —
// auth.FromContext only ever carries a shop_id Middleware already resolved
// a session against) not-found case the same way stock.requestLocaleFor's
// callers do: as a plain error, not a 404, since a missing shop row here
// is a server-side data bug, never a client mistake.
func loadShop(ctx context.Context, q *db.Queries, shopID uuid.UUID) (db.Shop, error) {
	shopRow, err := q.GetShop(ctx, shopID)
	if err != nil {
		return db.Shop{}, fmt.Errorf("reports: get shop: %w", err)
	}
	return shopRow, nil
}

// validateLocation reports whether locationID (nil = "no filter, do
// nothing") names a location belonging to shopID, returning it unchanged
// as *uuid.UUID for the caller to pass straight to a sqlc params struct.
// An unowned or nonexistent id is 400 VALIDATION_FAILED naming
// `locationId` — both report operations' contract responses list 400 but
// not 404 (unlike stock/purchases' locationId checks, which have a 404
// branch to use), so this reuses the "invalid" vocabulary (O-12) instead
// of apierr.NotFound.
func validateLocation(ctx context.Context, q *db.Queries, shopID uuid.UUID, locationID *uuid.UUID) (*uuid.UUID, error) {
	if locationID == nil {
		return nil, nil
	}
	if _, err := q.GetLocation(ctx, db.GetLocationParams{ShopID: shopID, ID: *locationID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.Validation(map[string]string{"locationId": "invalid"})
		}
		return nil, fmt.Errorf("reports: get location: %w", err)
	}
	return locationID, nil
}
