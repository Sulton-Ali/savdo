package seed_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
	"github.com/Sulton-Ali/savdo/api/internal/seed"
)

// createActiveSession inserts one non-revoked session for userID, the way
// a real login would, so ResetOwnerPassword's session revocation has
// something real to revoke.
func createActiveSession(ctx context.Context, t *testing.T, q *db.Queries, shopID, userID uuid.UUID) db.Session {
	t.Helper()
	session, err := q.CreateSession(ctx, db.CreateSessionParams{
		ID:        uuid.New(),
		ShopID:    shopID,
		UserID:    userID,
		TokenHash: []byte("test-token-hash"),
		Client:    db.SessionClientWeb,
		ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	return session
}

func TestResetOwnerPasswordChangesTheHashRevokesSessionsAndInvalidatesTheOldPassword(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()

	report, err := seed.Seed(ctx, pool, seed.DefaultShopSlug)
	if err != nil {
		t.Fatalf("Seed() error = %v", err)
	}

	q := db.New(pool)
	owner, err := q.GetOwner(ctx, report.ShopID)
	if err != nil {
		t.Fatalf("GetOwner() error = %v", err)
	}
	oldHash := owner.PasswordHash

	session := createActiveSession(ctx, t, q, report.ShopID, owner.ID)

	const newPassword = "owner-dev-pass-2"
	result, err := seed.ResetOwnerPassword(ctx, pool, seed.DefaultShopSlug, newPassword)
	if err != nil {
		t.Fatalf("ResetOwnerPassword() error = %v", err)
	}
	if result.RevokedSessions != 1 {
		t.Fatalf("RevokedSessions = %d, want 1", result.RevokedSessions)
	}

	updatedOwner, err := q.GetOwner(ctx, report.ShopID)
	if err != nil {
		t.Fatalf("GetOwner() after reset error = %v", err)
	}
	if updatedOwner.PasswordHash == oldHash {
		t.Fatal("ResetOwnerPassword() did not change the owner's password hash")
	}

	oldOK, err := auth.Verify(updatedOwner.PasswordHash, seed.DevOwnerPassword)
	if err != nil {
		t.Fatalf("Verify(old password) error = %v", err)
	}
	if oldOK {
		t.Fatal("the old dev password still verifies against the new hash")
	}

	newOK, err := auth.Verify(updatedOwner.PasswordHash, newPassword)
	if err != nil {
		t.Fatalf("Verify(new password) error = %v", err)
	}
	if !newOK {
		t.Fatal("the new password does not verify against the new hash")
	}

	sessions, err := q.ListUserSessions(ctx, db.ListUserSessionsParams{ShopID: report.ShopID, UserID: owner.ID})
	if err != nil {
		t.Fatalf("ListUserSessions() error = %v", err)
	}
	var revoked bool
	for _, s := range sessions {
		if s.ID == session.ID {
			revoked = s.RevokedAt != nil
		}
	}
	if !revoked {
		t.Fatal("the owner's active session was not revoked")
	}
}

func TestResetOwnerPasswordRejectsAShortPassword(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()

	if _, err := seed.Seed(ctx, pool, seed.DefaultShopSlug); err != nil {
		t.Fatalf("Seed() error = %v", err)
	}

	if _, err := seed.ResetOwnerPassword(ctx, pool, seed.DefaultShopSlug, "short"); err == nil {
		t.Fatal("ResetOwnerPassword() with a < 8 char password: want an error, got nil")
	}
}

func TestResetOwnerPasswordFailsWhenTheShopDoesNotExist(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()

	if _, err := seed.ResetOwnerPassword(ctx, pool, "no-such-shop", "a-fine-password"); err == nil {
		t.Fatal("ResetOwnerPassword() for a missing shop: want an error, got nil")
	}
}
