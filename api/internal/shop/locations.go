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
// transaction because it must first find out whether this is the shop's
// first location (which becomes the default automatically regardless of
// wantDefault) and, when the new location is to be the default, clear
// any existing default first — the same ClearDefaultLocation-then-write
// ordering UpdateLocation uses, so the partial "one default per shop"
// unique index is never violated.
func (s *Service) CreateLocation(ctx context.Context, shopID uuid.UUID, name string, kind db.LocationKind, wantDefault bool) (db.Location, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return db.Location{}, fmt.Errorf("shop: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	existing, err := qtx.ListLocations(ctx, db.ListLocationsParams{ShopID: shopID, Limit: 1})
	if err != nil {
		return db.Location{}, fmt.Errorf("shop: check existing locations: %w", err)
	}
	isDefault := wantDefault || len(existing) == 0

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
// false) or deactivating it (isActive: false) is refused with the same
// `fields.isDefault: invalid` error; setting isDefault: true clears any
// existing default first, in the same transaction as the write.
func (s *Service) UpdateLocation(ctx context.Context, shopID, id uuid.UUID, in LocationPatchInput) (db.Location, error) {
	current, err := s.q.GetLocation(ctx, db.GetLocationParams{ShopID: shopID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Location{}, apierr.NotFound("location")
		}
		return db.Location{}, fmt.Errorf("shop: get location: %w", err)
	}

	unsettingDefault := current.IsDefault && in.IsDefault != nil && !*in.IsDefault
	deactivatingDefault := current.IsDefault && in.IsActive != nil && !*in.IsActive
	if unsettingDefault || deactivatingDefault {
		return db.Location{}, apierr.Validation(map[string]string{"isDefault": "invalid"})
	}

	makingDefault := in.IsDefault != nil && *in.IsDefault && !current.IsDefault
	if !makingDefault {
		updated, err := s.q.UpdateLocation(ctx, db.UpdateLocationParams{
			Name: in.Name, Kind: in.Kind, IsDefault: in.IsDefault, IsActive: in.IsActive,
			ShopID: shopID, ID: id,
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

	if err := qtx.ClearDefaultLocation(ctx, shopID); err != nil {
		return db.Location{}, fmt.Errorf("shop: clear default location: %w", err)
	}
	updated, err := qtx.UpdateLocation(ctx, db.UpdateLocationParams{
		Name: in.Name, Kind: in.Kind, IsDefault: in.IsDefault, IsActive: in.IsActive,
		ShopID: shopID, ID: id,
	})
	if err != nil {
		if field, ok := conflictField(err); ok {
			return db.Location{}, apierr.Conflict(field)
		}
		return db.Location{}, fmt.Errorf("shop: update location (make default): %w", err)
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
