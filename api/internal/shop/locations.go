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
// transaction and, before deciding anything, locks the shop's current
// default location row via GetDefaultLocationForUpdate — the same lock
// UpdateLocation takes first when it might change the default — so a
// concurrent CreateLocation/UpdateLocation racing to become the default
// serializes on that row instead of both reading a stale snapshot.
// GetDefaultLocationForUpdate returning pgx.ErrNoRows means the shop has
// no location yet (given the invariant every shop with ≥1 location has
// exactly one default), which is also how this method decides "this is
// the first location" — it becomes the default automatically regardless
// of wantDefault. When the new location is to be the default, any
// existing one is cleared first, in the same transaction as the insert,
// so the partial "one default per shop" unique index is never violated
// by this method's own write.
//
// The one race this lock cannot close is two concurrent creates that
// are BOTH the shop's very first location: there is no default row yet
// for either to lock. locations_shop_id_default_key (mapped to 409
// CONFLICT details.field=isDefault by conflictField) is the backstop
// for that narrow, onboarding-only window.
func (s *Service) CreateLocation(ctx context.Context, shopID uuid.UUID, name string, kind db.LocationKind, wantDefault bool) (db.Location, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return db.Location{}, fmt.Errorf("shop: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	_, err = qtx.GetDefaultLocationForUpdate(ctx, shopID)
	isFirst := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !isFirst {
		return db.Location{}, fmt.Errorf("shop: lock default location: %w", err)
	}
	isDefault := wantDefault || isFirst

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
// inside one transaction that locks, in order: the shop's current
// default location row (GetDefaultLocationForUpdate — every caller that
// might change the default, including CreateLocation, takes this same
// lock first), then the target row itself (GetLocationForUpdate). Two
// concurrent "make this the default" requests targeting different rows
// therefore serialize on the shared default-row lock before either
// reaches its own target: whichever commits first wins outright, and
// the second sees the first's result already committed rather than a
// stale snapshot — so both succeed (last writer wins), never a 500.
// locations_shop_id_default_key (mapped to 409 CONFLICT
// details.field=isDefault by conflictField) remains as a backstop for
// any interleaving this locking order doesn't itself rule out.
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

	if _, err := qtx.GetDefaultLocationForUpdate(ctx, shopID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return db.Location{}, fmt.Errorf("shop: lock default location: %w", err)
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
