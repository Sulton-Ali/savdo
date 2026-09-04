package shop

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// ListLocations returns up to limit locations for shopID, newest first,
// resuming after (cursorCreatedAt, cursorID) when both are non-nil — the
// same keyset-pagination contract db.ListLocationsParams already
// documents. Callers wanting a "does another page exist" signal should
// request one more row than they intend to show and trim it themselves
// (handler.go does this); this method is a thin, directly-testable
// passthrough so a shop-isolation test can call it with one shop's
// context and assert it never sees another shop's rows.
func (s *Service) ListLocations(ctx context.Context, shopID uuid.UUID, limit int32, cursorCreatedAt *time.Time, cursorID *uuid.UUID) ([]db.Location, error) {
	rows, err := s.q.ListLocations(ctx, db.ListLocationsParams{
		ShopID:          shopID,
		CursorCreatedAt: cursorCreatedAt,
		CursorID:        cursorID,
		Limit:           limit,
	})
	if err != nil {
		return nil, fmt.Errorf("shop: list locations: %w", err)
	}
	return rows, nil
}

// CreateLocation creates a location for shopID. It runs inside a
// transaction that starts by locking the shop row itself (LockShop) —
// the per-tenant serialization point every default-changing path takes
// first, including UpdateLocation — before deciding anything. Only
// after holding that lock does it ask ShopHasLocations: earlier, this
// method inferred "is this the first location" from whether
// GetDefaultLocationForUpdate found a default row, which was wrong
// under contention — a concurrent takeover's default row is a *new*
// row from a plain CREATE's point of view, so that lock could report
// "no default found" (via FOR UPDATE's row re-evaluation skipping a
// row that changed out from under it) even though the shop already had
// one, silently making an unrequested location the default. Locking
// the shop row first closes that: no other default-changing
// transaction can even be mid-flight while this one decides isFirst,
// so ShopHasLocations is always answered against a fully-settled state.
// When the new location is to be the default, any existing one is
// cleared first, in the same transaction as the insert, so the partial
// "one default per shop" unique index is never violated by this
// method's own write — with the shop lock in place, that index should
// never actually fire; locations_shop_id_default_key (mapped to 409
// CONFLICT details.field=isDefault by conflictField) stays only as a
// last-resort backstop.
func (s *Service) CreateLocation(ctx context.Context, shopID uuid.UUID, name string, kind db.LocationKind, wantDefault bool) (db.Location, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return db.Location{}, fmt.Errorf("shop: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	if _, err := qtx.LockShop(ctx, shopID); err != nil {
		return db.Location{}, fmt.Errorf("shop: lock shop: %w", err)
	}

	hasLocations, err := qtx.ShopHasLocations(ctx, shopID)
	if err != nil {
		return db.Location{}, fmt.Errorf("shop: check existing locations: %w", err)
	}
	isDefault := wantDefault || !hasLocations

	if isDefault {
		if err := qtx.ClearDefaultLocation(ctx, shopID); err != nil {
			return db.Location{}, fmt.Errorf("shop: clear default location: %w", err)
		}
	}

	loc, err := qtx.CreateLocation(ctx, db.CreateLocationParams{
		ID:        newID(),
		ShopID:    shopID,
		Name:      name,
		Kind:      kind,
		IsDefault: isDefault,
		IsActive:  true,
	})
	if err != nil {
		if field, ok := conflictField(err); ok {
			return db.Location{}, apierr.Conflict(field)
		}
		return db.Location{}, fmt.Errorf("shop: create location: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return db.Location{}, fmt.Errorf("shop: commit create location: %w", err)
	}
	return loc, nil
}

// LocationPatchInput is UpdateLocation's field-level patch; a nil pointer
// leaves that field unchanged.
type LocationPatchInput struct {
	Name      *string
	Kind      *db.LocationKind
	IsDefault *bool
	IsActive  *bool
}

// UpdateLocation applies in to shopID's location id. Business rules
// (docs/06-ROADMAP.md Phase 1 T5 spec): a shop must always have exactly
// one default location, so unsetting the current default (isDefault:
// false) is refused with `fields.isDefault: invalid`, and deactivating
// it (isActive: false) is refused with `fields.isActive: invalid` —
// the same underlying rule, reported under whichever field the caller
// actually sent. Setting isDefault: true clears any existing default
// first, in the same transaction as the write.
//
// Whenever isDefault or isActive is part of the patch, everything runs
// inside one transaction that starts by locking the shop row itself
// (LockShop) — the same per-tenant serialization point CreateLocation
// takes first — then locks the target row (GetLocationForUpdate) to
// read its current state for the guards below. Two concurrent
// "change the default" requests, whether both PATCHes or one a
// CreateLocation, therefore fully serialize on the shop lock: the
// second never even reads its own state until the first has committed
// or rolled back, so both succeed (last writer wins), never a 500 and
// never spuriously silent. locations_shop_id_default_key (mapped to 409
// CONFLICT details.field=isDefault by conflictField) remains only as a
// last-resort backstop; with the shop lock in place it should never
// actually fire.
func (s *Service) UpdateLocation(ctx context.Context, shopID, id uuid.UUID, in LocationPatchInput) (db.Location, error) {
	if in.IsDefault == nil && in.IsActive == nil {
		// Fast path: neither field that can violate the "exactly one
		// default" invariant is being touched, so there is nothing to
		// guard and no need for a transaction — just confirm the
		// location belongs to this shop and apply the patch.
		if _, err := s.q.GetLocation(ctx, db.GetLocationParams{ShopID: shopID, ID: id}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return db.Location{}, apierr.NotFound("location")
			}
			return db.Location{}, fmt.Errorf("shop: get location: %w", err)
		}

		updated, err := s.q.UpdateLocation(ctx, db.UpdateLocationParams{
			Name: in.Name, Kind: in.Kind, ShopID: shopID, ID: id,
		})
		if err != nil {
			if field, ok := conflictField(err); ok {
				return db.Location{}, apierr.Conflict(field)
			}
			return db.Location{}, fmt.Errorf("shop: update location: %w", err)
		}
		return updated, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return db.Location{}, fmt.Errorf("shop: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	if _, err := qtx.LockShop(ctx, shopID); err != nil {
		return db.Location{}, fmt.Errorf("shop: lock shop: %w", err)
	}

	current, err := qtx.GetLocationForUpdate(ctx, db.GetLocationForUpdateParams{ShopID: shopID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Location{}, apierr.NotFound("location")
		}
		return db.Location{}, fmt.Errorf("shop: get location: %w", err)
	}

	if current.IsDefault && in.IsDefault != nil && !*in.IsDefault {
		return db.Location{}, apierr.Validation(map[string]string{"isDefault": "invalid"})
	}
	if current.IsDefault && in.IsActive != nil && !*in.IsActive {
		return db.Location{}, apierr.Validation(map[string]string{"isActive": "invalid"})
	}

	makingDefault := in.IsDefault != nil && *in.IsDefault && !current.IsDefault
	if makingDefault {
		if err := qtx.ClearDefaultLocation(ctx, shopID); err != nil {
			return db.Location{}, fmt.Errorf("shop: clear default location: %w", err)
		}
	}

	updated, err := qtx.UpdateLocation(ctx, db.UpdateLocationParams{
		Name: in.Name, Kind: in.Kind, IsDefault: in.IsDefault, IsActive: in.IsActive,
		ShopID: shopID, ID: id,
	})
	if err != nil {
		if field, ok := conflictField(err); ok {
			return db.Location{}, apierr.Conflict(field)
		}
		return db.Location{}, fmt.Errorf("shop: update location: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return db.Location{}, fmt.Errorf("shop: commit update location: %w", err)
	}
	return updated, nil
}

// toGenLocation maps a db.Location onto the API's Location schema.
func toGenLocation(l db.Location) gen.Location {
	return gen.Location{
		Id:        l.ID,
		Name:      l.Name,
		Kind:      gen.LocationKind(l.Kind),
		IsDefault: l.IsDefault,
		IsActive:  l.IsActive,
	}
}
