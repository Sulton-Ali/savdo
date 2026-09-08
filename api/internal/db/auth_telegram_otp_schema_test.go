package db_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

// TestTelegramAccounts_linkUnlinkAndCrossShopIsolation exercises
// LinkTelegramAccount's upsert-on-user_id shape, the by-Telegram-user-id
// lookup (the entry point before a shop_id is known, same as
// GetSessionByTokenHash), and that a second shop's linked account never
// leaks into the first shop's own by-user-id read.
func TestTelegramAccounts_linkUnlinkAndCrossShopIsolation(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shopA := catalogShop(ctx, t, q, "shop-telegram-a")
	shopB := catalogShop(ctx, t, q, "shop-telegram-b")
	userA := salesUser(ctx, t, q, shopA.ID, "ownera", db.UserRoleOwner)
	userB := salesUser(ctx, t, q, shopB.ID, "ownerb", db.UserRoleOwner)

	username := "alice_tg"
	linked, err := q.LinkTelegramAccount(ctx, db.LinkTelegramAccountParams{
		UserID: userA.ID, ShopID: shopA.ID, TelegramUserID: 111222333, TelegramUsername: &username,
	})
	if err != nil {
		t.Fatalf("LinkTelegramAccount: %v", err)
	}
	if linked.UserID != userA.ID || linked.TelegramUserID != 111222333 {
		t.Fatalf("LinkTelegramAccount = %+v, want user_id %s telegram_user_id 111222333", linked, userA.ID)
	}

	// Entry point before shop_id is known: only the Telegram user id.
	byTelegramID, err := q.GetTelegramAccountByTelegramUserID(ctx, 111222333)
	if err != nil {
		t.Fatalf("GetTelegramAccountByTelegramUserID: %v", err)
	}
	if byTelegramID.UserID != userA.ID || byTelegramID.ShopID != shopA.ID {
		t.Fatalf("GetTelegramAccountByTelegramUserID = %+v, want user %s / shop %s", byTelegramID, userA.ID, shopA.ID)
	}

	// Re-linking the same user (upsert on user_id) replaces the row in
	// place rather than erroring or creating a second one.
	newUsername := "alice_new_tg"
	relinked, err := q.LinkTelegramAccount(ctx, db.LinkTelegramAccountParams{
		UserID: userA.ID, ShopID: shopA.ID, TelegramUserID: 999888777, TelegramUsername: &newUsername,
	})
	if err != nil {
		t.Fatalf("LinkTelegramAccount (relink): %v", err)
	}
	if relinked.TelegramUserID != 999888777 {
		t.Fatalf("relinked TelegramUserID = %d, want 999888777", relinked.TelegramUserID)
	}
	if _, err := q.GetTelegramAccountByTelegramUserID(ctx, 111222333); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("old telegram_user_id should no longer resolve, got err=%v", err)
	}

	// A second shop's own account, same username, different Telegram id —
	// must not be visible through shopB's by-user-id read for userA, or
	// vice versa (hard rule 1).
	if _, err := q.LinkTelegramAccount(ctx, db.LinkTelegramAccountParams{
		UserID: userB.ID, ShopID: shopB.ID, TelegramUserID: 444555666,
	}); err != nil {
		t.Fatalf("LinkTelegramAccount (shop B): %v", err)
	}
	if _, err := q.GetTelegramAccountByUserID(ctx, db.GetTelegramAccountByUserIDParams{ShopID: shopA.ID, UserID: userB.ID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("shop A must not see shop B's account by user_id, got err=%v", err)
	}

	// Unlink removes the row; a second unlink affects zero rows.
	affected, err := q.UnlinkTelegramAccount(ctx, db.UnlinkTelegramAccountParams{ShopID: shopA.ID, UserID: userA.ID})
	if err != nil {
		t.Fatalf("UnlinkTelegramAccount: %v", err)
	}
	if affected != 1 {
		t.Fatalf("UnlinkTelegramAccount affected = %d, want 1", affected)
	}
	affected, err = q.UnlinkTelegramAccount(ctx, db.UnlinkTelegramAccountParams{ShopID: shopA.ID, UserID: userA.ID})
	if err != nil {
		t.Fatalf("UnlinkTelegramAccount (second time): %v", err)
	}
	if affected != 0 {
		t.Fatalf("UnlinkTelegramAccount (second time) affected = %d, want 0", affected)
	}
}

// TestTelegramAccounts_telegramUserIDUniqueAcrossUsers pins
// UNIQUE(telegram_user_id): one Telegram account must never link to two
// different users, even across shops.
func TestTelegramAccounts_telegramUserIDUniqueAcrossUsers(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-telegram-unique")
	user1 := salesUser(ctx, t, q, shop.ID, "user1", db.UserRoleOwner)
	user2 := salesUser(ctx, t, q, shop.ID, "user2", db.UserRoleManager)

	if _, err := q.LinkTelegramAccount(ctx, db.LinkTelegramAccountParams{
		UserID: user1.ID, ShopID: shop.ID, TelegramUserID: 12345,
	}); err != nil {
		t.Fatalf("LinkTelegramAccount(user1): %v", err)
	}

	_, err := q.LinkTelegramAccount(ctx, db.LinkTelegramAccountParams{
		UserID: user2.ID, ShopID: shop.ID, TelegramUserID: 12345,
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("want a unique_violation linking the same telegram_user_id to a second user, got: %v", err)
	}
}

// otpFixture: a shop and one user, for the OTP lifecycle tests below.
func otpFixture(t *testing.T, slug string) (context.Context, *pgxpool.Pool, *db.Queries, db.Shop, db.User) {
	t.Helper()
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)
	shop := catalogShop(ctx, t, q, slug)
	user := salesUser(ctx, t, q, shop.ID, "owner1", db.UserRoleOwner)
	return ctx, pool, q, shop, user
}

// TestOTPCodes_lifecycle covers create -> active -> wrong-attempt ->
// used, and that a used or expired code is no longer "active".
func TestOTPCodes_lifecycle(t *testing.T) {
	ctx, _, q, shop, user := otpFixture(t, "shop-otp-lifecycle")

	code, err := q.CreateOTPCode(ctx, db.CreateOTPCodeParams{
		ID: uuid.New(), ShopID: shop.ID, UserID: user.ID, Purpose: db.OtpPurposePasswordReset,
		CodeHash: []byte("hashed-code"), ExpiresAt: time.Now().Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateOTPCode: %v", err)
	}
	if code.Attempts != 0 || code.UsedAt != nil {
		t.Fatalf("fresh code = %+v, want attempts 0 and used_at nil", code)
	}

	active, err := q.GetActiveOTPCode(ctx, db.GetActiveOTPCodeParams{ShopID: shop.ID, UserID: user.ID, Purpose: db.OtpPurposePasswordReset})
	if err != nil {
		t.Fatalf("GetActiveOTPCode: %v", err)
	}
	if active.ID != code.ID {
		t.Fatalf("GetActiveOTPCode = %s, want %s", active.ID, code.ID)
	}

	touched, err := q.IncrementOTPAttempts(ctx, db.IncrementOTPAttemptsParams{ShopID: shop.ID, ID: code.ID})
	if err != nil {
		t.Fatalf("IncrementOTPAttempts: %v", err)
	}
	if touched.Attempts != 1 {
		t.Fatalf("Attempts = %d, want 1", touched.Attempts)
	}

	used, err := q.MarkOTPUsed(ctx, db.MarkOTPUsedParams{ShopID: shop.ID, ID: code.ID})
	if err != nil {
		t.Fatalf("MarkOTPUsed: %v", err)
	}
	if used.UsedAt == nil {
		t.Fatal("MarkOTPUsed: want used_at set, got nil")
	}

	// A used code is no longer active.
	if _, err := q.GetActiveOTPCode(ctx, db.GetActiveOTPCodeParams{ShopID: shop.ID, UserID: user.ID, Purpose: db.OtpPurposePasswordReset}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("want no active code after MarkOTPUsed, got err=%v", err)
	}

	// Marking an already-used code used again touches zero rows (the
	// used_at IS NULL guard), so :one reports pgx.ErrNoRows.
	if _, err := q.MarkOTPUsed(ctx, db.MarkOTPUsedParams{ShopID: shop.ID, ID: code.ID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("want pgx.ErrNoRows re-marking an already-used code, got: %v", err)
	}
}

// TestOTPCodes_expiredExcludedFromActive pins that GetActiveOTPCode
// ignores an expired-but-unused code.
func TestOTPCodes_expiredExcludedFromActive(t *testing.T) {
	ctx, _, q, shop, user := otpFixture(t, "shop-otp-expired")

	if _, err := q.CreateOTPCode(ctx, db.CreateOTPCodeParams{
		ID: uuid.New(), ShopID: shop.ID, UserID: user.ID, Purpose: db.OtpPurposeLinkTelegram,
		CodeHash: []byte("hashed"), ExpiresAt: time.Now().Add(-1 * time.Minute),
	}); err != nil {
		t.Fatalf("CreateOTPCode: %v", err)
	}

	if _, err := q.GetActiveOTPCode(ctx, db.GetActiveOTPCodeParams{ShopID: shop.ID, UserID: user.ID, Purpose: db.OtpPurposeLinkTelegram}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("want an expired code excluded from GetActiveOTPCode, got err=%v", err)
	}
}

// TestOTPCodes_newCodeInvalidatesOld pins D-06's "a new code invalidates
// the old one": ExpireOTPCodes marks every unused code for (user,
// purpose) as used, so GetActiveOTPCode then only ever returns the
// newest one issued after it.
func TestOTPCodes_newCodeInvalidatesOld(t *testing.T) {
	ctx, _, q, shop, user := otpFixture(t, "shop-otp-invalidate")

	first, err := q.CreateOTPCode(ctx, db.CreateOTPCodeParams{
		ID: uuid.New(), ShopID: shop.ID, UserID: user.ID, Purpose: db.OtpPurposeConfirmAction,
		CodeHash: []byte("first"), ExpiresAt: time.Now().Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateOTPCode(first): %v", err)
	}

	affected, err := q.ExpireOTPCodes(ctx, db.ExpireOTPCodesParams{ShopID: shop.ID, UserID: user.ID, Purpose: db.OtpPurposeConfirmAction})
	if err != nil {
		t.Fatalf("ExpireOTPCodes: %v", err)
	}
	if affected != 1 {
		t.Fatalf("ExpireOTPCodes affected = %d, want 1", affected)
	}

	second, err := q.CreateOTPCode(ctx, db.CreateOTPCodeParams{
		ID: uuid.New(), ShopID: shop.ID, UserID: user.ID, Purpose: db.OtpPurposeConfirmAction,
		CodeHash: []byte("second"), ExpiresAt: time.Now().Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateOTPCode(second): %v", err)
	}

	active, err := q.GetActiveOTPCode(ctx, db.GetActiveOTPCodeParams{ShopID: shop.ID, UserID: user.ID, Purpose: db.OtpPurposeConfirmAction})
	if err != nil {
		t.Fatalf("GetActiveOTPCode: %v", err)
	}
	if active.ID != second.ID {
		t.Fatalf("GetActiveOTPCode = %s, want the newest code %s (first %s should be expired)", active.ID, second.ID, first.ID)
	}
}

// TestOTPCodes_purposeCheckedByEnum pins that purpose is a real enum, not
// free text — an unrecognized value is rejected by Postgres itself.
func TestOTPCodes_purposeCheckedByEnum(t *testing.T) {
	ctx, pool, _, shop, user := otpFixture(t, "shop-otp-badpurpose")

	_, err := pool.Exec(ctx, `
		INSERT INTO otp_codes (id, shop_id, user_id, purpose, code_hash, expires_at)
		VALUES ($1, $2, $3, 'not-a-real-purpose', 'x', now() + interval '5 minutes')
	`, uuid.New(), shop.ID, user.ID)
	if err == nil {
		t.Fatal("want an error inserting an unrecognized otp_purpose, got none")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || !strings.Contains(pgErr.Message, "otp_purpose") {
		t.Fatalf("want an otp_purpose enum error, got: %v", err)
	}
}
