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
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

// fakeOTPSender is an OTPSender that records every call instead of
// actually reaching Telegram — otp.go's own doc comment on OTPSender:
// "here a no-op/fake in tests". delay simulates a slow/blocking delivery
// (a real Telegram round trip, or a hung one) — RequestOtp now dispatches
// SendOTP from a goroutine (Review MAJOR 2), so a test asserting that
// delivery doesn't block the request needs a sender it can actually make
// slow. done, if set, receives one value after each SendOTP call has
// finished recording itself, so a test can wait for the async goroutine
// to complete instead of racing it.
type fakeOTPSender struct {
	mu    sync.Mutex
	calls []fakeOTPSend
	err   error
	delay time.Duration
	done  chan struct{}
}

type fakeOTPSend struct {
	telegramUserID int64
	code           string
	locale         string
}

func (f *fakeOTPSender) SendOTP(ctx context.Context, telegramUserID int64, code string, locale string) error {
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.mu.Lock()
	f.calls = append(f.calls, fakeOTPSend{telegramUserID, code, locale})
	f.mu.Unlock()
	if f.done != nil {
		f.done <- struct{}{}
	}
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

// waitForDone waits for one signal on done (a fakeOTPSender's own done
// channel), failing the test if it doesn't arrive within timeout — used
// by tests exercising RequestOtp's async delivery goroutine, which would
// otherwise race the assertions that follow.
func waitForDone(t *testing.T, done <-chan struct{}, timeout time.Duration) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatal("timed out waiting for the async OTP delivery goroutine to finish")
	}
}

// waitForCondition polls cond until it reports true or timeout elapses,
// failing the test in the latter case. Used after waitForDone: SendOTP
// having returned only proves delivery itself finished, not that the
// goroutine's own subsequent slog.Error call (which runs after SendOTP
// returns, otp.go's own RequestOtp) has happened yet.
func waitForCondition(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for condition")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// syncBuffer is a concurrency-safe bytes.Buffer wrapper: RequestOtp's
// delivery goroutine now logs on its own goroutine (Review MAJOR 2), so a
// test's slog output sink has to be safe for the main goroutine to read
// from concurrently — a bare bytes.Buffer is not.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
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

	callCount := func() int {
		sender.mu.Lock()
		defer sender.mu.Unlock()
		return len(sender.calls)
	}

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
	// Delivery runs off the request path (Review MAJOR 2), so give the
	// one goroutine it triggers a moment to complete before asserting on
	// what it recorded.
	waitForCondition(t, 2*time.Second, func() bool { return callCount() == 1 })
	got := sender.lastCall(t)
	if got.telegramUserID != 111 {
		t.Fatalf("delivered to telegramUserID = %d, want 111", got.telegramUserID)
	}
}

func TestRequestOtpDeliversExactlyOneCodeToTheLinkedAccount(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	svc := NewService(pool, q, testConfig(), shop.ID)
	done := make(chan struct{}, 1)
	sender := &fakeOTPSender{done: done}
	svc.SetOTPSender(sender)

	user := seedUser(ctx, t, q, shop.ID, "owner1", "correct-horse-battery", db.UserRoleOwner)
	linkTelegram(ctx, t, q, shop.ID, user.ID, 555)

	if err := svc.RequestOtp(ctx, "owner1", db.OtpPurposePasswordReset, nil); err != nil {
		t.Fatalf("RequestOtp() error = %v", err)
	}
	// Delivery runs off the request path (Review MAJOR 2).
	waitForDone(t, done, 2*time.Second)

	call := sender.lastCall(t)
	sender.mu.Lock()
	gotCalls := len(sender.calls)
	sender.mu.Unlock()
	if gotCalls != 1 {
		t.Fatalf("sender.calls = %d, want exactly 1", gotCalls)
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
	done := make(chan struct{}, 1)
	sender := &fakeOTPSender{err: errors.New("telegram: send failed"), done: done}
	svc.SetOTPSender(sender)

	user := seedUser(ctx, t, q, shop.ID, "owner1", "correct-horse-battery", db.UserRoleOwner)
	linkTelegram(ctx, t, q, shop.ID, user.ID, 555)

	buf := &syncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, nil)))
	defer slog.SetDefault(prev)

	if err := svc.RequestOtp(ctx, "owner1", db.OtpPurposePasswordReset, nil); err != nil {
		t.Fatalf("RequestOtp() error = %v, want nil even when delivery fails", err)
	}

	// Delivery (and the failure log it triggers) now happens on its own
	// goroutine (Review MAJOR 2) — wait for it rather than racing it.
	waitForDone(t, done, 2*time.Second)
	waitForCondition(t, time.Second, func() bool { return buf.String() != "" })

	code := sender.lastCall(t).code
	logged := buf.String()
	if strings.Contains(logged, code) {
		t.Fatalf("log output contains the OTP code (%q): %s", code, logged)
	}
}

// TestRequestOtpDeliveryIsAsyncAndDoesNotBlockOnASlowOrFailingSender is
// Review MAJOR 2's own regression test: a synchronous SendOTP call used
// to make RequestOtp's own latency track a real Telegram round trip
// (here simulated with a deliberately slow, failing sender) — a timing
// side channel on top of tying up the request for as long as delivery
// took. RequestOtp must now return in about the same time regardless,
// with delivery (and its failure log) completing afterwards on its own
// goroutine.
func TestRequestOtpDeliveryIsAsyncAndDoesNotBlockOnASlowOrFailingSender(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	svc := NewService(pool, q, testConfig(), shop.ID)
	done := make(chan struct{}, 1)
	const sendDelay = 300 * time.Millisecond
	sender := &fakeOTPSender{err: errors.New("telegram: send failed"), delay: sendDelay, done: done}
	svc.SetOTPSender(sender)

	user := seedUser(ctx, t, q, shop.ID, "owner1", "correct-horse-battery", db.UserRoleOwner)
	linkTelegram(ctx, t, q, shop.ID, user.ID, 555)

	buf := &syncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, nil)))
	defer slog.SetDefault(prev)

	start := time.Now()
	if err := svc.RequestOtp(ctx, "owner1", db.OtpPurposePasswordReset, nil); err != nil {
		t.Fatalf("RequestOtp() error = %v, want nil", err)
	}
	if elapsed := time.Since(start); elapsed >= sendDelay {
		t.Fatalf("RequestOtp() took %v, want it to return well before the sender's own %v delay (delivery must be async)", elapsed, sendDelay)
	}

	// Delivery must still actually happen (and its failure still logged
	// as a class only), just off the request path.
	waitForDone(t, done, 2*time.Second)
	waitForCondition(t, time.Second, func() bool { return buf.String() != "" })

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
	done := make(chan struct{}, 1)
	sender := &fakeOTPSender{done: done}
	svc.SetOTPSender(sender)

	user := seedUser(ctx, t, q, shop.ID, "owner1", "correct-horse-battery", db.UserRoleOwner)
	linkTelegram(ctx, t, q, shop.ID, user.ID, 555)

	if err := svc.RequestOtp(ctx, "owner1", db.OtpPurposePasswordReset, nil); err != nil {
		t.Fatalf("RequestOtp() error = %v", err)
	}
	// Delivery runs off the request path (Review MAJOR 2).
	waitForDone(t, done, 2*time.Second)
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
	done := make(chan struct{}, 1)
	sender := &fakeOTPSender{done: done}
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
	// Delivery runs off the request path (Review MAJOR 2).
	waitForDone(t, done, 2*time.Second)
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

	if err := svc.ResetPassword(ctx, actionToken, "new-password-2", nil); err != nil {
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
	if err := svc.ResetPassword(ctx, actionToken, "yet-another-password-3", nil); err == nil {
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

	if err := svc.ResetPassword(ctx, "not-a-real-token", "some-new-password", nil); err == nil || errStatus(t, err) != 401 {
		t.Fatalf("ResetPassword() error = %v, want 401", err)
	}
}

// TestResetPasswordRateLimitedByIP mirrors
// TestAuthenticateTelegramRateLimitedByIP (telegram_test.go): ResetPassword
// shares Login's own ipLimiter (D-118), so it must be rate limited the same
// way, checked before the (here, malformed) action token is ever parsed.
func TestResetPasswordRateLimitedByIP(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	cfg := testConfig()
	cfg.LoginRateIPPerMin = 2
	svc := NewService(pool, q, cfg, shop.ID)

	for i := 0; i < 2; i++ {
		err := svc.ResetPassword(ctx, "not-a-real-token", "some-new-password", nil)
		if err == nil || errStatus(t, err) != 401 {
			t.Fatalf("attempt %d: want 401 (still under the rate limit), got %v", i+1, err)
		}
	}

	err := svc.ResetPassword(ctx, "not-a-real-token", "some-new-password", nil)
	if err == nil {
		t.Fatal("3rd attempt: error = nil, want RateLimited")
	}
	if got := errStatus(t, err); got != 429 {
		t.Fatalf("3rd attempt status = %d, want 429", got)
	}
}

// TestVerifyOtpAttemptCapMarksCodeUsed is Review MAJOR 4's own regression
// test: reaching otpMaxAttempts must mark the code used in the database
// (db/queries/otp.sql's own doc comment on IncrementOTPAttempts already
// promised this; the service didn't do it), not just make VerifyOtp
// happen to keep returning 401.
func TestVerifyOtpAttemptCapMarksCodeUsed(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	svc := NewService(pool, q, testConfig(), shop.ID)
	done := make(chan struct{}, 1)
	sender := &fakeOTPSender{done: done}
	svc.SetOTPSender(sender)

	user := seedUser(ctx, t, q, shop.ID, "owner1", "correct-horse-battery", db.UserRoleOwner)
	linkTelegram(ctx, t, q, shop.ID, user.ID, 555)

	if err := svc.RequestOtp(ctx, "owner1", db.OtpPurposePasswordReset, nil); err != nil {
		t.Fatalf("RequestOtp() error = %v", err)
	}
	waitForDone(t, done, 2*time.Second)
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

	var usedAt *time.Time
	row := pool.QueryRow(ctx,
		`SELECT used_at FROM otp_codes WHERE shop_id = $1 AND user_id = $2 AND purpose = $3 ORDER BY created_at DESC LIMIT 1`,
		shop.ID, user.ID, db.OtpPurposePasswordReset)
	if err := row.Scan(&usedAt); err != nil {
		t.Fatalf("query otp_codes: %v", err)
	}
	if usedAt == nil {
		t.Fatal("otp_codes.used_at is still NULL after otpMaxAttempts wrong attempts, want it marked used")
	}

	// Even the real code must now fail: the code is used, not merely
	// "over the cap".
	if _, _, err := svc.VerifyOtp(ctx, "owner1", db.OtpPurposePasswordReset, realCode, nil); err == nil || errStatus(t, err) != 401 {
		t.Fatalf("VerifyOtp(real code, after max attempts) error = %v, want 401", err)
	}
}

// TestRequestOtpProducesNoCodeForAnotherShopsUsername is Review finding
// 6's second half: an OTP request against shop A's Service for a
// username that only exists in shop B must never deliver anything —
// GetUserByUsername is shop-scoped (hard rule 1), so shop A's service
// never even finds shop B's user.
func TestRequestOtpProducesNoCodeForAnotherShopsUsername(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shopA := seedShop(ctx, t, q, "shop-a")
	shopB := seedShop(ctx, t, q, "shop-b")

	shopBUser := seedUser(ctx, t, q, shopB.ID, "shared-username", "correct-horse-battery", db.UserRoleOwner)
	linkTelegram(ctx, t, q, shopB.ID, shopBUser.ID, 999)

	svcA := NewService(pool, q, testConfig(), shopA.ID)
	sender := &fakeOTPSender{}
	svcA.SetOTPSender(sender)

	if err := svcA.RequestOtp(ctx, "shared-username", db.OtpPurposePasswordReset, nil); err != nil {
		t.Fatalf("RequestOtp() error = %v, want nil (no enumeration, even across shops)", err)
	}
	// Shop A's RequestOtp returns before ever reaching the delivery
	// dispatch for a username it can't find (no user, no goroutine) —
	// this margin is only a defensive wait, not a requirement for
	// correctness here.
	time.Sleep(20 * time.Millisecond)
	sender.mu.Lock()
	gotForShopA := len(sender.calls)
	sender.mu.Unlock()
	if gotForShopA != 0 {
		t.Fatalf("sender.calls = %d, want 0 (shop A must never deliver a code for a username that only exists in shop B)", gotForShopA)
	}

	// The same username, against shop B's own Service, does deliver —
	// proving the absence above is really about shop scoping, not a
	// broken username/link.
	svcB := NewService(pool, q, testConfig(), shopB.ID)
	svcB.SetOTPSender(sender)
	if err := svcB.RequestOtp(ctx, "shared-username", db.OtpPurposePasswordReset, nil); err != nil {
		t.Fatalf("RequestOtp() (shop B, own user) error = %v, want nil", err)
	}
	waitForCondition(t, time.Second, func() bool {
		sender.mu.Lock()
		defer sender.mu.Unlock()
		return len(sender.calls) == 1
	})
}

// TestResetPasswordRejectsInactiveUser is Review MINOR 10's own
// regression test: an action token minted while the user was active but
// redeemed after an owner deactivated the account in between must not
// reset the password.
func TestResetPasswordRejectsInactiveUser(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "shop-a")
	svc := NewService(pool, q, testConfig(), shop.ID)
	done := make(chan struct{}, 1)
	sender := &fakeOTPSender{done: done}
	svc.SetOTPSender(sender)

	user := seedUser(ctx, t, q, shop.ID, "owner1", "old-password-1", db.UserRoleOwner)
	linkTelegram(ctx, t, q, shop.ID, user.ID, 555)

	if err := svc.RequestOtp(ctx, "owner1", db.OtpPurposePasswordReset, nil); err != nil {
		t.Fatalf("RequestOtp() error = %v", err)
	}
	waitForDone(t, done, 2*time.Second)
	code := sender.lastCall(t).code

	actionToken, _, err := svc.VerifyOtp(ctx, "owner1", db.OtpPurposePasswordReset, code, nil)
	if err != nil {
		t.Fatalf("VerifyOtp() error = %v", err)
	}

	isActive := false
	if _, err := q.UpdateUser(ctx, db.UpdateUserParams{ShopID: shop.ID, ID: user.ID, IsActive: &isActive}); err != nil {
		t.Fatalf("deactivate user: %v", err)
	}

	if err := svc.ResetPassword(ctx, actionToken, "new-password-2", nil); err == nil || errStatus(t, err) != 401 {
		t.Fatalf("ResetPassword() (inactive user) error = %v, want 401", err)
	}

	// Reactivate and prove the password was left alone: the old one
	// still works, the attempted new one was never applied.
	isActive = true
	if _, err := q.UpdateUser(ctx, db.UpdateUserParams{ShopID: shop.ID, ID: user.ID, IsActive: &isActive}); err != nil {
		t.Fatalf("reactivate user: %v", err)
	}
	if _, err := svc.Login(ctx, "owner1", "old-password-1", db.SessionClientWeb, "", nil); err != nil {
		t.Fatalf("Login(old password) error = %v, want nil (ResetPassword must not have changed it)", err)
	}
}
