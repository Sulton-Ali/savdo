// Package seed creates the placeholder demo shop, its two locations and its
// three demo users (docs/00-DECISIONS.md D-30), idempotently: running it
// again against a database that already has some or all of these rows
// changes nothing for the rows that already exist — in particular it never
// resets a password, and never overwrites a shop's settings once the row
// exists. The real family shop's data replaces all of this at Phase 8
// onboarding.
package seed

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// DefaultShopSlug is the slug `savdo seed` and `savdo reset-owner-password`
// use when no --shop-slug is given.
const DefaultShopSlug = "savdo-demo"

// ShopName, LocationStoreName and LocationWarehouseName are the demo shop's
// name and its two locations (D-30).
const (
	ShopName              = "Savdo Demo"
	LocationStoreName     = "Doʻkon"
	LocationWarehouseName = "Ombor"
)

// DEV ONLY — cleartext passwords for the three seeded demo accounts. These
// exist only so a freshly seeded local/dev database is immediately usable;
// they must never protect a real account. `savdo reset-owner-password`
// (D-28) replaces the owner's before any real use, and the admin's staff
// password reset does the same for manager/cashier.
// They are not a leaked real credential; seed refuses to run against
// ENV=prod without --force, and reset-owner-password/staff password reset
// replace them before any real use.
//
//nolint:gosec // G101: intentional, documented dev-only demo passwords.
const (
	DevOwnerPassword   = "owner-dev-pass"
	DevManagerPassword = "manager-dev-pass"
	DevCashierPassword = "cashier-dev-pass"
)

// userSpec is one of the three demo users Seed upserts, keyed by username.
type userSpec struct {
	username string
	fullName string
	role     db.UserRole
	password string
}

var userSpecs = []userSpec{
	{username: "owner", fullName: "Demo Owner", role: db.UserRoleOwner, password: DevOwnerPassword},
	{username: "manager", fullName: "Demo Manager", role: db.UserRoleManager, password: DevManagerPassword},
	{username: "cashier", fullName: "Demo Cashier", role: db.UserRoleCashier, password: DevCashierPassword},
}

// locationSpec is one of the two demo locations Seed upserts, keyed by
// name.
type locationSpec struct {
	name      string
	kind      db.LocationKind
	isDefault bool
}

var locationSpecs = []locationSpec{
	{name: LocationStoreName, kind: db.LocationKindStore, isDefault: true},
	{name: LocationWarehouseName, kind: db.LocationKindWarehouse, isDefault: false},
}

// EntityResult is what happened to one entity Seed processed, in the order
// it was processed, for the CLI's one-line-per-entity summary and for
// tests.
type EntityResult struct {
	Kind    string // "shop", "location" or "user"
	Name    string // slug, location name or username
	Created bool   // false means the row already existed and was left alone
}

// Report is Seed's return value.
type Report struct {
	ShopID   uuid.UUID
	Entities []EntityResult
}

// Seed upserts the demo shop (shopSlug, or DefaultShopSlug when empty), its
// two locations and its three users, all inside one transaction. Every
// entity is looked up first and only created if missing — the shop by
// slug, a location by (shop, name), a user by (shop, username) — so a
// second call against the same database reports every entity as already
// existing and changes nothing: no password is reset, no shop setting is
// touched, and no location's is_default is changed once a default already
// exists.
func Seed(ctx context.Context, pool *pgxpool.Pool, shopSlug string) (Report, error) {
	if shopSlug == "" {
		shopSlug = DefaultShopSlug
	}

	var report Report
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		q := db.New(tx)

		shop, shopCreated, err := upsertShop(ctx, q, shopSlug)
		if err != nil {
			return err
		}
		report.ShopID = shop.ID
		report.Entities = append(report.Entities, EntityResult{Kind: "shop", Name: shop.Slug, Created: shopCreated})

		locationResults, err := upsertLocations(ctx, q, shop.ID)
		if err != nil {
			return err
		}
		report.Entities = append(report.Entities, locationResults...)

		userResults, err := upsertUsers(ctx, q, shop.ID)
		if err != nil {
			return err
		}
		report.Entities = append(report.Entities, userResults...)

		return nil
	})
	if err != nil {
		return Report{}, err
	}
	return report, nil
}

// upsertShop returns the shop for slug, creating it (name ShopName,
// currency/timezone/locale left to the migration's column defaults per
// CreateShop's own doc comment) if it does not exist yet. An existing
// shop's settings are never touched here — only UpdateShop, which Seed
// never calls, changes them.
func upsertShop(ctx context.Context, q *db.Queries, slug string) (db.Shop, bool, error) {
	shop, err := q.GetShopBySlug(ctx, slug)
	if err == nil {
		return shop, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.Shop{}, false, fmt.Errorf("seed: look up shop %q: %w", slug, err)
	}

	shop, err = q.CreateShop(ctx, db.CreateShopParams{ID: newUUID(), Slug: slug, Name: ShopName})
	if err != nil {
		return db.Shop{}, false, fmt.Errorf("seed: create shop %q: %w", slug, err)
	}
	return shop, true, nil
}

// upsertLocations creates whichever of locationSpecs don't already exist
// for shopID (matched by name), leaving existing ones untouched. It never
// sets is_default = true on a new row if the shop already has a default
// location, so re-running Seed after the owner has changed the default
// elsewhere can't fight that choice or violate the one-default-per-shop
// constraint.
func upsertLocations(ctx context.Context, q *db.Queries, shopID uuid.UUID) ([]EntityResult, error) {
	existing, err := q.ListLocations(ctx, db.ListLocationsParams{ShopID: shopID, Limit: 1000})
	if err != nil {
		return nil, fmt.Errorf("seed: list locations: %w", err)
	}

	byName := make(map[string]struct{}, len(existing))
	haveDefault := false
	for _, loc := range existing {
		byName[loc.Name] = struct{}{}
		if loc.IsDefault {
			haveDefault = true
		}
	}

	results := make([]EntityResult, 0, len(locationSpecs))
	for _, spec := range locationSpecs {
		if _, ok := byName[spec.name]; ok {
			results = append(results, EntityResult{Kind: "location", Name: spec.name, Created: false})
			continue
		}

		isDefault := spec.isDefault && !haveDefault
		_, err := q.CreateLocation(ctx, db.CreateLocationParams{
			ID:        newUUID(),
			ShopID:    shopID,
			Name:      spec.name,
			Kind:      spec.kind,
			IsDefault: isDefault,
			IsActive:  true,
		})
		if err != nil {
			return nil, fmt.Errorf("seed: create location %q: %w", spec.name, err)
		}
		if isDefault {
			haveDefault = true
		}
		results = append(results, EntityResult{Kind: "location", Name: spec.name, Created: true})
	}
	return results, nil
}

// upsertUsers creates whichever of userSpecs don't already exist for
// shopID (matched by username), leaving an existing user's password_hash
// exactly as it is — Seed only ever sets the initial password for a user
// it is itself creating.
func upsertUsers(ctx context.Context, q *db.Queries, shopID uuid.UUID) ([]EntityResult, error) {
	results := make([]EntityResult, 0, len(userSpecs))
	for _, spec := range userSpecs {
		_, err := q.GetUserByUsername(ctx, db.GetUserByUsernameParams{ShopID: shopID, Username: spec.username})
		if err == nil {
			results = append(results, EntityResult{Kind: "user", Name: spec.username, Created: false})
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("seed: look up user %q: %w", spec.username, err)
		}

		hash, err := auth.Hash(spec.password)
		if err != nil {
			// Unreachable in practice: every DevXPassword constant above is
			// well over auth.MinPasswordLength, so the only failure mode
			// auth.Hash has left is crypto/rand itself failing.
			return nil, fmt.Errorf("seed: hash password for %q: %w", spec.username, err)
		}

		_, err = q.CreateUser(ctx, db.CreateUserParams{
			ID:           newUUID(),
			ShopID:       shopID,
			Username:     spec.username,
			PasswordHash: hash,
			FullName:     spec.fullName,
			Role:         spec.role,
			Locale:       "uz",
		})
		if err != nil {
			return nil, fmt.Errorf("seed: create user %q: %w", spec.username, err)
		}
		results = append(results, EntityResult{Kind: "user", Name: spec.username, Created: true})
	}
	return results, nil
}

// newUUID returns a v7 UUID (ADR-003's convention for app-generated ids),
// falling back to v4 in the vanishingly unlikely case NewV7 fails — the
// same fallback auth/service.go's session issuance uses.
func newUUID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		id = uuid.New()
	}
	return id
}
