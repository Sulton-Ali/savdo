package seed

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// ResetOwnerPasswordResult is what ResetOwnerPassword did, for the CLI's
// confirmation line and for tests.
type ResetOwnerPasswordResult struct {
	RevokedSessions int
}

// ResetOwnerPassword sets shopSlug's owner (db.GetOwner) to newPassword and
// revokes every one of the owner's sessions, in one transaction
// (docs/00-DECISIONS.md D-28): after this call the owner must log in again,
// with the new password, on every device that was signed in. newPassword
// is validated the same way every other password-setting path validates a
// new password (auth.Hash: at least auth.MinPasswordLength Unicode
// characters) and is never logged, by this function or its caller.
func ResetOwnerPassword(ctx context.Context, pool *pgxpool.Pool, shopSlug, newPassword string) (ResetOwnerPasswordResult, error) {
	if shopSlug == "" {
		shopSlug = DefaultShopSlug
	}

	hash, err := auth.Hash(newPassword)
	if err != nil {
		var apiErr *apierr.Error
		if errors.As(err, &apiErr) {
			if fields, ok := apiErr.Details["fields"].(map[string]string); ok {
				if msg, ok := fields["password"]; ok {
					return ResetOwnerPasswordResult{}, fmt.Errorf("invalid password: %s", msg)
				}
			}
		}
		return ResetOwnerPasswordResult{}, fmt.Errorf("reset-owner-password: %w", err)
	}

	var result ResetOwnerPasswordResult
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		q := db.New(tx)

		shop, err := q.GetShopBySlug(ctx, shopSlug)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("reset-owner-password: no shop with slug %q", shopSlug)
			}
			return fmt.Errorf("reset-owner-password: look up shop %q: %w", shopSlug, err)
		}

		owner, err := q.GetOwner(ctx, shop.ID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("reset-owner-password: shop %q has no owner", shopSlug)
			}
			return fmt.Errorf("reset-owner-password: load owner: %w", err)
		}

		// RevokeAllUserSessions is a plain UPDATE ... :exec (no returned row
		// count), so the number of sessions it is about to revoke is
		// counted here, inside the same transaction, before running it.
		sessions, err := q.ListUserSessions(ctx, db.ListUserSessionsParams{ShopID: shop.ID, UserID: owner.ID})
		if err != nil {
			return fmt.Errorf("reset-owner-password: list owner sessions: %w", err)
		}
		activeSessions := 0
		for _, session := range sessions {
			if session.RevokedAt == nil {
				activeSessions++
			}
		}

		if err := q.SetUserPassword(ctx, db.SetUserPasswordParams{ShopID: shop.ID, ID: owner.ID, PasswordHash: hash}); err != nil {
			return fmt.Errorf("reset-owner-password: set password: %w", err)
		}
		if err := q.RevokeAllUserSessions(ctx, db.RevokeAllUserSessionsParams{ShopID: shop.ID, UserID: owner.ID}); err != nil {
			return fmt.Errorf("reset-owner-password: revoke sessions: %w", err)
		}

		result.RevokedSessions = activeSessions
		return nil
	})
	if err != nil {
		return ResetOwnerPasswordResult{}, err
	}
	return result, nil
}
