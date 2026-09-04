// Package shop implements shop settings, locations and staff management
// (docs/03-ARCHITECTURE.md § Module map: "shop" owns shops, locations and
// shop_settings; docs/04-DATA-MODEL.md § 1, § 7). Every operation reads
// its tenant boundary from auth.FromContext (ADR-004) — never from a path,
// query or body parameter — and staff/location management is gated by the
// owner-only permissions ADR-010 defines (auth.PermShopSettings,
// auth.PermLocationsManage, auth.PermStaffManage).
package shop

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// Service holds shop's dependencies and business rules: shop settings,
// location defaulting (locations.go) and staff lifecycle (staff.go),
// including the owner and self-protection rules docs/06-ROADMAP.md Phase
// 1 T5 specifies. handler.go and its siblings translate between this and
// the generated strict server types.
type Service struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

// NewService builds the shop Service. pool is used only for the handful
// of operations that need a transaction (default-location handling,
// staff deactivation, password reset); every other read/write goes
// through q directly.
func NewService(pool *pgxpool.Pool, q *db.Queries) *Service {
	return &Service{pool: pool, q: q}
}

// newID mints a UUID v7 (time-ordered, per docs/03-ARCHITECTURE.md §
// Cross-cutting: "IDs"), falling back to a v4 in the practically
// unreachable case the v7 generator's clock read fails — mirrors
// auth.Service.Login's own sessionID generation.
func newID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New()
	}
	return id
}

// conflictField maps a unique-violation (pgx error code 23505) to the
// API's `details.field` name by constraint name — never by parsing the
// driver's error message text, which is not a stable contract. Returns
// ok=false for any other error (including a 23505 on a constraint this
// package doesn't recognize), so callers fall back to wrapping the error
// as a 500 rather than mis-reporting it as a conflict. Shared by
// locations.go and staff.go, whose CREATE/UPDATE writes are the only ones
// that can hit a unique constraint.
func conflictField(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return "", false
	}
	switch pgErr.ConstraintName {
	case "users_shop_id_username_key":
		return "username", true
	case "users_shop_id_phone_key":
		return "phone", true
	case "locations_shop_id_name_key":
		return "name", true
	case "locations_shop_id_default_key":
		// The partial unique index enforcing "at most one default
		// location per shop". CreateLocation and UpdateLocation both
		// lock the shop row itself (LockShop) as their first statement
		// before deciding anything default-related, which fully
		// serializes every default-changing transaction for a shop —
		// this mapping is only a last-resort backstop for whatever this
		// codebase might get wrong about that locking order in the
		// future, not a path expected to fire in production: a
		// concurrent request that also ended up trying to set
		// is_default=true would lose here with a clean 409 instead of a
		// 500, rather than corrupting the "at most one default"
		// invariant the index itself still guarantees regardless.
		return "isDefault", true
	default:
		return "", false
	}
}

// GetShop returns shopID's settings row.
func (s *Service) GetShop(ctx context.Context, shopID uuid.UUID) (db.Shop, error) {
	shopRow, err := s.q.GetShop(ctx, shopID)
	if err != nil {
		return db.Shop{}, fmt.Errorf("shop: get shop: %w", err)
	}
	return shopRow, nil
}

// UpdateShopInput is UpdateShop's field-level patch: a nil pointer leaves
// that field unchanged (sqlc's COALESCE pattern — see UpdateShopParams).
// currency and slug are deliberately absent: they are immutable
// (docs/06-ROADMAP.md Phase 1 T5 spec).
type UpdateShopInput struct {
	Name                 *string
	Timezone             *string
	DefaultLocale        *string
	AllowNegativeStock   *bool
	UpdateCostOnPurchase *bool
	LowStockThreshold    *int
}

// UpdateShop applies in to shopID's settings. Field-shape validation
// (name length, a loadable timezone, a known locale, a non-negative
// lowStockThreshold that fits int32) is handler.go's job; by the time
// this runs, in's values are already known-good.
func (s *Service) UpdateShop(ctx context.Context, shopID uuid.UUID, in UpdateShopInput) (db.Shop, error) {
	var lowStockThreshold *int32
	if in.LowStockThreshold != nil {
		v := int32(*in.LowStockThreshold) // #nosec G115 -- range-checked in handler.go
		lowStockThreshold = &v
	}
	updated, err := s.q.UpdateShop(ctx, db.UpdateShopParams{
		Name:                 in.Name,
		Timezone:             in.Timezone,
		DefaultLocale:        in.DefaultLocale,
		AllowNegativeStock:   in.AllowNegativeStock,
		UpdateCostOnPurchase: in.UpdateCostOnPurchase,
		LowStockThreshold:    lowStockThreshold,
		ID:                   shopID,
	})
	if err != nil {
		return db.Shop{}, fmt.Errorf("shop: update shop: %w", err)
	}
	return updated, nil
}

// toGenShop maps a db.Shop onto the API's Shop schema — the one place a
// shop row becomes a response, so a field mistakenly added here (or
// forgotten) is a one-file review, not a hunt across every handler.
func toGenShop(sh db.Shop) gen.Shop {
	return gen.Shop{
		Id:                   sh.ID,
		Slug:                 sh.Slug,
		Name:                 sh.Name,
		Currency:             sh.Currency,
		Timezone:             sh.Timezone,
		DefaultLocale:        gen.Locale(sh.DefaultLocale),
		AllowNegativeStock:   sh.AllowNegativeStock,
		UpdateCostOnPurchase: sh.UpdateCostOnPurchase,
		LowStockThreshold:    int(sh.LowStockThreshold),
	}
}
