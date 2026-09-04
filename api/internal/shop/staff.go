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
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// ListStaff returns up to limit users for shopID, newest first — same
// keyset-pagination contract as ListLocations.
func (s *Service) ListStaff(ctx context.Context, shopID uuid.UUID, limit int32, cursorCreatedAt *time.Time, cursorID *uuid.UUID) ([]db.User, error) {
	rows, err := s.q.ListUsers(ctx, db.ListUsersParams{
		ShopID:          shopID,
		CursorCreatedAt: cursorCreatedAt,
		CursorID:        cursorID,
		Limit:           limit,
	})
	if err != nil {
		return nil, fmt.Errorf("shop: list staff: %w", err)
	}
	return rows, nil
}

// CreateStaffInput is CreateStaff's already-shape-validated input: by the
// time this reaches the Service, username is trimmed/lowercased and
// length/pattern-checked, password is length-checked, role is manager or
// cashier (never owner — the API never lets a caller create a second
// owner), and Locale is nil only when the caller omitted it (this method
// then defaults to the shop's own default_locale).
type CreateStaffInput struct {
	Username string
	Password string
	FullName string
	Phone    *string
	Role     db.UserRole
	Locale   *string
}

// CreateStaff creates a manager or cashier for shopID.
func (s *Service) CreateStaff(ctx context.Context, shopID uuid.UUID, in CreateStaffInput) (db.User, error) {
	hash, err := auth.Hash(in.Password)
	if err != nil {
		return db.User{}, err
	}

	locale := in.Locale
	if locale == nil {
		shopRow, err := s.q.GetShop(ctx, shopID)
		if err != nil {
			return db.User{}, fmt.Errorf("shop: load shop default locale: %w", err)
		}
		locale = &shopRow.DefaultLocale
	}

	user, err := s.q.CreateUser(ctx, db.CreateUserParams{
		ID:           newID(),
		ShopID:       shopID,
		Username:     in.Username,
		PasswordHash: hash,
		FullName:     in.FullName,
		Phone:        in.Phone,
		Role:         in.Role,
		Locale:       *locale,
	})
	if err != nil {
		if field, ok := conflictField(err); ok {
			return db.User{}, apierr.Conflict(field)
		}
		return db.User{}, fmt.Errorf("shop: create staff: %w", err)
	}
	return user, nil
}

// StaffPatchInput is UpdateStaff's field-level patch; a nil pointer
// leaves that field unchanged.
type StaffPatchInput struct {
	FullName *string
	Phone    *string
	Role     *db.UserRole
	IsActive *bool
	Locale   *string
}

// UpdateStaff applies in to shopID's staff member id, acted on by
// actorID. Owner protections (docs/06-ROADMAP.md Phase 1 T5 spec): the
// owner's role/isActive can never be changed through this endpoint, and
// no user (owner included) can change their own role or deactivate
// themselves. Deactivating a (non-owner, non-self) user revokes every
// one of their sessions in the same transaction as the write.
func (s *Service) UpdateStaff(ctx context.Context, shopID, actorID, id uuid.UUID, in StaffPatchInput) (db.User, error) {
	target, err := s.q.GetUserByID(ctx, db.GetUserByIDParams{ShopID: shopID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.User{}, apierr.NotFound("staff")
		}
		return db.User{}, fmt.Errorf("shop: get staff: %w", err)
	}

	fields := map[string]string{}
	if in.Role != nil {
		if target.Role == db.UserRoleOwner || id == actorID {
			fields["role"] = "invalid"
		}
	}
	if in.IsActive != nil {
		if target.Role == db.UserRoleOwner {
			fields["isActive"] = "invalid"
		} else if id == actorID && !*in.IsActive {
			fields["isActive"] = "invalid"
		}
	}
	if len(fields) > 0 {
		return db.User{}, apierr.Validation(fields)
	}

	deactivating := in.IsActive != nil && !*in.IsActive
	if !deactivating {
		updated, err := s.q.UpdateUser(ctx, db.UpdateUserParams{
			FullName: in.FullName, Phone: in.Phone, Role: in.Role, Locale: in.Locale, IsActive: in.IsActive,
			ShopID: shopID, ID: id,
		})
		if err != nil {
			if field, ok := conflictField(err); ok {
				return db.User{}, apierr.Conflict(field)
			}
			return db.User{}, fmt.Errorf("shop: update staff: %w", err)
		}
		return updated, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return db.User{}, fmt.Errorf("shop: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	updated, err := qtx.UpdateUser(ctx, db.UpdateUserParams{
		FullName: in.FullName, Phone: in.Phone, Role: in.Role, Locale: in.Locale, IsActive: in.IsActive,
		ShopID: shopID, ID: id,
	})
	if err != nil {
		if field, ok := conflictField(err); ok {
			return db.User{}, apierr.Conflict(field)
		}
		return db.User{}, fmt.Errorf("shop: update staff (deactivate): %w", err)
	}
	if err := qtx.RevokeAllUserSessions(ctx, db.RevokeAllUserSessionsParams{ShopID: shopID, UserID: id}); err != nil {
		return db.User{}, fmt.Errorf("shop: revoke sessions on deactivate: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return db.User{}, fmt.Errorf("shop: commit deactivate staff: %w", err)
	}
	return updated, nil
}

// SetStaffPassword hashes password and sets it on shopID's user id,
// revoking every one of that user's sessions in the same transaction —
// including when the owner resets their own password (D-28). Resetting
// an inactive user's password is allowed (it is not gated on IsActive);
// it grants that user nothing until an owner reactivates them, since
// Login's own IsActive check (auth.Service.Login) still refuses a
// deactivated user regardless of how current their password hash is.
func (s *Service) SetStaffPassword(ctx context.Context, shopID, id uuid.UUID, password string) error {
	if _, err := s.q.GetUserByID(ctx, db.GetUserByIDParams{ShopID: shopID, ID: id}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.NotFound("staff")
		}
		return fmt.Errorf("shop: get staff: %w", err)
	}

	hash, err := auth.Hash(password)
	if err != nil {
		return err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("shop: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	if err := qtx.SetUserPassword(ctx, db.SetUserPasswordParams{ShopID: shopID, ID: id, PasswordHash: hash}); err != nil {
		return fmt.Errorf("shop: set staff password: %w", err)
	}
	if err := qtx.RevokeAllUserSessions(ctx, db.RevokeAllUserSessionsParams{ShopID: shopID, UserID: id}); err != nil {
		return fmt.Errorf("shop: revoke sessions on password reset: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("shop: commit password reset: %w", err)
	}
	return nil
}

// toGenUser maps a db.User onto the API's User schema. PasswordHash and
// ShopID are deliberately never carried across — a password hash must
// never reach a response (hard rule 9), and shop_id is the tenant
// boundary, not response payload.
//
// Phone maps a stored literal empty string to nil (never a bare ""), the
// counterpart to UpdateStaff's phone handling (handler_staff.go): since
// UpdateUser's `phone = COALESCE($2, phone)` can never write a true SQL
// NULL over an existing non-null value, the one way this package has
// (without a query change out of its scope) to honor a `PATCH
// {"phone":""}` "clear" request is to store the literal empty string and
// present it as absent here — every response, not just the one right
// after the PATCH, so the API never shows a client a literal "" phone.
func toGenUser(u db.User) gen.User {
	phone := u.Phone
	if phone != nil && *phone == "" {
		phone = nil
	}
	return gen.User{
		Id:          u.ID,
		Username:    u.Username,
		FullName:    u.FullName,
		Phone:       phone,
		Role:        gen.Role(u.Role),
		Locale:      gen.Locale(u.Locale),
		IsActive:    u.IsActive,
		LastLoginAt: u.LastLoginAt,
		CreatedAt:   u.CreatedAt,
	}
}
