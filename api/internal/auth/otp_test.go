package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

// fakeOTPSender is an OTPSender that records every call instead of
// actually reaching Telegram — otp.go's own doc comment on OTPSender:
// "here a no-op/fake in tests".
type fakeOTPSender struct {
	mu    sync.Mutex
	calls []fakeOTPSend
	err   error
}

type fakeOTPSend struct {
	telegramUserID int64
	code           string
	locale         string
}

func (f *fakeOTPSender) SendOTP(_ context.Context, telegramUserID int64, code string, locale string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeOTPSend{telegramUserID, code, locale})
	return f.err
}

func (f *fakeOTPSender) lastCall(t *testing.T) fakeOTPSend {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		t.Fatal("SendOTP was never called")
	}
	return f.calls[len(f.calls)-1]
}

// linkTelegram links userID to a fake Telegram id, the precondition
// RequestOtp needs to actually deliver anything.
func linkTelegram(ctx context.Context, t *testing.T, q *db.Queries, shopID, userID uuid.UUID, telegramUserID int64) {
	t.Helper()
	if _, err := q.LinkTelegramAccount(ctx, db.LinkTelegramAccountParams{
		ID: uuid.New(), UserID: userID, ShopID: shopID, TelegramUserID: telegramUserID,
	}); err != nil {
		t.Fatalf("LinkTelegramAccount: %v", err)
	}
}

func TestRequestOtpAlways202EquivalentRegardlessOfUsername(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	svc := NewService(pool, q, testConfig(), shop.ID)
	sender := &fakeOTPSender{}
	svc.SetOTPSender(sender)

	linkedActive := seedUser(ctx, t, q, shop.ID, "linked-active", "correct-horse-battery", db.UserRoleOwner)
	linkTelegram(ctx, t, q, shop.ID, linkedActive.ID, 111)

	seedUser(ctx, t, q, shop.ID, "unlinked-active", "correct-horse-battery", db.UserRoleCashier)

	inactiveLinked := seedUser(ctx, t, q, shop.ID, "inactive-linked", "correct-horse-battery", db.UserRoleManager)
	linkTelegram(ctx, t, q, shop.ID, inactiveLinked.ID, 222)
	isActive := false
	if _, err := q.UpdateUser(ctx, db.UpdateUserParams{ShopID: shop.ID, ID: inactiveLinked.ID, IsActive: &isActive}); err != nil {
		t.Fatalf("deactivate user: %v", err)
	}

	tests := []string{"linked-active", "unlinked-active", "inactive-linked", "no-such-user-at-all"}
	for i, username := range tests {
		t.Run(username, func(t *testing.T) {
			// A distinct fake IP per subtest: otpRequestIPLimiter is
			// per-IP as well as per-username, so four calls from the same
			// (nil/"unknown") IP would legitimately 429 on the 4th
			// regardless of username — that is otpRequestRateLimit doing
			// its job, not what this test is about. Isolate the
			// per-username "no enumeration" property being tested here
			// from the separate per-IP one.
			ip := netip.MustParseAddr(fmt.Sprintf("10.0.0.%d", i+1))
			if err := svc.RequestOtp(ctx, username, db.OtpPurposePasswordReset, &ip); err != nil {
				t.Fatalf("RequestOtp(%q) error = %v, want nil (always succeeds unless rate limited)", username, err)
			}
		})
	}

	// Only the linked, active user actually got a code delivered.
	if len(sender.calls) != 1 {
		t.Fatalf("sender.calls = %d, want exactly 1 (only the linked active user)", len(sender.calls))
	}
	if sender.calls[0].telegramUserID != 111 {
		t.Fatalf("delivered to telegramUserID = %d, want 111", sender.calls[0].telegramUserID)
	}
}

func TestRequestOtpDeliversExactlyOneCodeToTheLinkedAccount(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	svc := NewService(pool, q, testConfig(), shop.ID)
	sender := &fakeOTPSender{}
	svc.SetOTPSender(sender)

	user := seedUser(ctx, t, q, shop.ID, "owner1", "correct-horse-battery", db.UserRoleOwner)
	linkTelegram(ctx, t, q, shop.ID, user.ID, 555)

	if err := svc.RequestOtp(ctx, "owner1", db.OtpPurposePasswordReset, nil); err != nil {
		t.Fatalf("RequestOtp() error = %v", err)
	}

	call := sender.lastCall(t)
	if len(sender.calls) != 1 {
		t.Fatalf("sender.calls = %d, want exactly 1", len(sender.calls))
	}
	if len(call.code) != otpCodeDigits {
		t.Fatalf("code = %q, want %d digits", call.code, otpCodeDigits)
	}
	for _, r := range call.code {
		if r < '0' || r > '9' {
			t.Fatalf("code = %q, want all digits", call.code)
		}
	}

	// VerifyOtp with the delivered code must succeed — proves what
	// otp_codes actually stored matches what the fake sender received.
	if _, _, err := svc.VerifyOtp(ctx, "owner1", db.OtpPurposePasswordReset, call.code, nil); err != nil {
		t.Fatalf("VerifyOtp(delivered code) error = %v, want nil", err)
	}
}

func TestRequestOtpDeliveryFailureIsLoggedAsAClassNeverTheCode(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	svc := NewService(pool, q, testConfig(), shop.ID)
	sender := &fakeOTPSender{err: errors.New("telegram: send failed")}
	svc.SetOTPSender(sender)

	user := seedUser(ctx, t, q, shop.ID, "owner1", "correct-horse-battery", db.UserRoleOwner)
	linkTelegram(ctx, t, q, shop.ID, user.ID, 555)

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	if err := svc.RequestOtp(ctx, "owner1", db.OtpPurposePasswordReset, nil); err != nil {
		t.Fatalf("RequestOtp() error = %v, want nil even when delivery fails", err)
	}

	code := sender.lastCall(t).code
	logged := buf.String()
	if logged == "" {
		t.Fatal("expected a log line for the delivery failure")
	}
	if strings.Contains(logged, code) {
		t.Fatalf("log output contains the OTP code (%q): %s", code, logged)
	}
}

func TestRequestOtpRateLimitsByIPAndUsername(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	svc := NewService(pool, q, testConfig(), shop.ID)

	for i := 0; i < otpRequestRateLimit; i++ {
		if err := svc.RequestOtp(ctx, "whoever", db.OtpPurposePasswordReset, nil); err != nil {
			t.Fatalf("attempt %d: RequestOtp() error = %v, want nil (still under the limit)", i+1, err)
		}
	}

	err := svc.RequestOtp(ctx, "whoever", db.OtpPurposePasswordReset, nil)
	if err == nil || errStatus(t, err) != 429 {
		t.Fatalf("RequestOtp() (over the limit) error = %v, want 429", err)
	}
}

func TestVerifyOtpWrongCodeFiveTimesThenLocksOut(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	svc := NewService(pool, q, testConfig(), shop.ID)
	sender := &fakeOTPSender{}
	svc.SetOTPSender(sender)

	user := seedUser(ctx, t, q, shop.ID, "owner1", "correct-horse-battery", db.UserRoleOwner)
	linkTelegram(ctx, t, q, shop.ID, user.ID, 555)

	if err := svc.RequestOtp(ctx, "owner1", db.OtpPurposePasswordReset, nil); err != nil {
		t.Fatalf("RequestOtp() error = %v", err)
	}
	realCode := sender.lastCall(t).code
	wrongCode := "000000"
	if wrongCode == realCode {
		wrongCode = "111111"
	}

	for i := 0; i < otpMaxAttempts; i++ {
		_, _, err := svc.VerifyOtp(ctx, "owner1", db.OtpPurposePasswordReset, wrongCode, nil)
		if err == nil || errStatus(t, err) != 401 {
			t.Fatalf("attempt %d: VerifyOtp() error = %v, want 401", i+1, err)
		}
	}

	// Even the real code must now fail: attempts exhausted.
	_, _, err := svc.VerifyOtp(ctx, "owner1", db.OtpPurposePasswordReset, realCode, nil)
	if err == nil || errStatus(t, err) != 401 {
		t.Fatalf("VerifyOtp(real code, after max attempts) error = %v, want 401", err)
	}
}

func TestVerifyOtpThenResetPasswordFullFlow(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	svc := NewService(pool, q, testConfig(), shop.ID)
	sender := &fakeOTPSender{}
	svc.SetOTPSender(sender)

	user := seedUser(ctx, t, q, shop.ID, "owner1", "old-password-1", db.UserRoleOwner)
	linkTelegram(ctx, t, q, shop.ID, user.ID, 555)

	// A second, still-live session belonging to the same user — proves
	// ResetPassword revokes *every* session, not just the one (if any)
	// tied to the request.
	otherSession, err := svc.Login(ctx, "owner1", "old-password-1", db.SessionClientMobile, "", nil)
	if err != nil {
		t.Fatalf("Login() (seed a session to revoke) error = %v", err)
	}

	if err := svc.RequestOtp(ctx, "owner1", db.OtpPurposePasswordReset, nil); err != nil {
		t.Fatalf("RequestOtp() error = %v", err)
	}
	code := sender.lastCall(t).code

	actionToken, expiresAt, err := svc.VerifyOtp(ctx, "owner1", db.OtpPurposePasswordReset, code, nil)
	if err != nil {
		t.Fatalf("VerifyOtp() error = %v", err)
	}
	if actionToken == "" {
		t.Fatal("VerifyOtp() actionToken is empty")
	}
	if expiresAt.IsZero() {
		t.Fatal("VerifyOtp() expiresAt is zero")
	}

	// The password_reset code was single use: verifying again fails.
	if _, _, err := svc.VerifyOtp(ctx, "owner1", db.OtpPurposePasswordReset, code, nil); err == nil {
		t.Fatal("VerifyOtp() (code reused) error = nil, want 401")
	}

	if err := svc.ResetPassword(ctx, actionToken, "new-password-2"); err != nil {
		t.Fatalf("ResetPassword() error = %v", err)
	}

	// Old password no longer works, new one does.
	if _, err := svc.Login(ctx, "owner1", "old-password-1", db.SessionClientWeb, "", nil); err == nil {
		t.Fatal("Login(old password) error = nil, want 401")
	}
	if _, err := svc.Login(ctx, "owner1", "new-password-2", db.SessionClientWeb, "", nil); err != nil {
		t.Fatalf("Login(new password) error = %v, want nil", err)
	}

	// The action token is single use.
	if err := svc.ResetPassword(ctx, actionToken, "yet-another-password-3"); err == nil {
		t.Fatal("ResetPassword() (token reused) error = nil, want 401")
	}

	// The other session (mobile, created before the reset) was revoked —
	// ListSessions/ListUserSessions returns every session regardless of
	// revoked_at (session_test.go's own RevokeSession tests rely on the
	// same thing), so the assertion is on RevokedAt, not on the row being
	// absent.
	sessions, err := svc.ListSessions(ctx, shop.ID, user.ID)
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	found := false
	for _, s := range sessions {
		if s.ID == otherSession.Session.ID {
			found = true
			if s.RevokedAt == nil {
				t.Fatal("the pre-reset mobile session's RevokedAt is nil; ResetPassword must revoke every session")
			}
		}
	}
	if !found {
		t.Fatal("test setup: the pre-reset mobile session is not in ListSessions at all")
	}
}

func TestResetPasswordRejectsMalformedToken(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	svc := NewService(pool, q, testConfig(), shop.ID)

	if err := svc.ResetPassword(ctx, "not-a-real-token", "some-new-password"); err == nil || errStatus(t, err) != 401 {
		t.Fatalf("ResetPassword() error = %v, want 401", err)
	}
}
