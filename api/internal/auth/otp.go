package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/netip"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// otpRequestRateLimit and otpRateWindow are RequestOtp's own per-IP/
// per-username budget (docs/03-ARCHITECTURE.md § Cross-cutting: "login and
// OTP endpoints per IP and per username"): at most 3 requests per 15
// minutes, keyed the same way login's own limiters are (ipKey,
// userLimiterKey). This is an in-memory limiter — correct for Savdo's
// single-process deployment (ratelimit.go's own doc comment); Phase 8 may
// move it to Caddy instead, per this task's brief.
//
// otpVerifyRateLimit is VerifyOtp's own, separate budget over the same
// otpRateWindow — deliberately not shared with RequestOtp's, and
// deliberately looser than otpRequestRateLimit: VerifyOtp already has its
// own, tighter per-code defense (otpMaxAttempts, checked against
// otp_codes.attempts, below), so a legitimate user who mistypes a couple
// of times before getting it right must not also burn through the same
// budget a single RequestOtp call already spent — otpVerifyRateLimit is
// sized with headroom above otpMaxAttempts (a full wrong-code lockout
// sequence plus the one correct submission that would follow it) so that
// path never collides with this one; it still bounds an attacker hammering
// GetActiveOTPCode across many different usernames/codes from one IP.
const (
	otpRequestRateLimit = 3
	otpVerifyRateLimit  = 10
	otpRateWindow       = 15 * time.Minute
)

// otpCodeDigits/otpTTL/otpMaxAttempts are the 6-digit/5-minute/5-attempt
// numbers ADR-005 pins for the code a user reads out of Telegram and types
// back. actionTokenTTL is the 10-minute window OtpVerifyResponse's own
// contract description promises for the actionToken VerifyOtp exchanges a
// good code for.
const (
	otpCodeDigits   = 6
	otpTTL          = 5 * time.Minute
	otpMaxAttempts  = 5
	actionTokenTTL  = 10 * time.Minute
	otpSaltLen      = 16 // bytes, hashOTPCode's per-code salt
	actionVerifierN = 20 // bytes of random verifier newSelectorToken draws for an actionToken
)

// otpAttemptLockStripes is how many mutexes otpAttemptLock spreads
// (shop, user, purpose) keys across. Both VerifyOtp's wrong-code branch
// and CompleteLink's wrong-verifier branch (telegram.go) used to read
// GetActiveOTPCode's otp.Attempts, compare it against otpMaxAttempts, and
// only then call IncrementOTPAttempts — a read-then-act sequence with no
// lock between the two: N concurrent guesses against the same code could
// each read the same stale, still-under-the-cap otp.Attempts before any
// of their own IncrementOTPAttempts committed, so each proceeded to
// increment independently. IncrementOTPAttempts's own UPDATE is atomic at
// the row level, and the code that decides whether to mark the row used
// already reads the freshly-updated value it returns (not the stale
// snapshot — VerifyOtp/CompleteLink's own doc comments), so the row does
// still end up locked once *some* increment crosses the cap; what wasn't
// bounded was the *total number of guesses* that got to run before that
// happened — up to as many as raced through the same window, not just
// otpMaxAttempts of them. otpAttemptLock closes that by serializing the
// whole read-check-increment sequence per key, so only one caller at a
// time can even read otp.Attempts for a given (shop, user, purpose).
//
// A lock per exact key (rather than this fixed stripe count) would avoid
// any unrelated-key contention, but would also need its own cleanup the
// same way loginLimiter's own map needs eviction (ratelimit.go) — striping
// across a fixed, small number of mutexes instead bounds memory with no
// cleanup needed at all, at the cost of occasionally serializing two
// unrelated keys that happen to hash to the same stripe. That's an
// acceptable trade here: VerifyOtp/CompleteLink traffic is already capped
// by otpVerifyRateLimit and the shared ipLimiter (D-118), so contention
// under this lock is never more than a handful of requests deep.
const otpAttemptLockStripes = 256

// otpAttemptLock is Service's own otpAttemptLock field's type — see
// otpAttemptLockStripes's doc comment above for what it's for. Callers
// acquire it with lock(key), hold it for the read-check-increment section,
// and release with the returned func — never call lock twice for
// overlapping keys on the same goroutine (there's only ever one such
// section per call in this package, so that never happens today).
type otpAttemptLock struct {
	stripes [otpAttemptLockStripes]sync.Mutex
}

// lock acquires the mutex key hashes to and returns a func that releases
// it — sha256, not a weaker hash, only because this package already
// imports it for everything else; collision resistance itself is not the
// point (any deterministic spread of keys across stripes would do).
func (l *otpAttemptLock) lock(key string) (unlock func()) {
	sum := sha256.Sum256([]byte(key))
	idx := binary.BigEndian.Uint32(sum[:4]) % otpAttemptLockStripes
	l.stripes[idx].Lock()
	return l.stripes[idx].Unlock
}

// otpAttemptLockKey builds the key VerifyOtp and CompleteLink each lock
// on: the same (shop, user, purpose) tuple GetActiveOTPCode itself is
// queried by, since that tuple is exactly what determines which
// otp_codes row two concurrent callers might race on.
func otpAttemptLockKey(shopID, userID uuid.UUID, purpose db.OtpPurpose) string {
	return shopID.String() + "|" + userID.String() + "|" + string(purpose)
}

// otpDeliveryTimeout bounds the goroutine RequestOtp spawns to actually
// call OTPSender.SendOTP (Review MAJOR 2) — long enough for a normal
// Telegram Bot API call, short enough that a hung sender can't leak
// goroutines forever. Derived from context.Background(), not the
// request's own ctx, since the request has already returned by the time
// this fires.
const otpDeliveryTimeout = 10 * time.Second

// OTPSender delivers a freshly generated OTP code to a user's linked
// Telegram account. Implemented by internal/bot (T4); RequestOtp (below)
// never has any other way to reach a chat — it only ever has the
// telegramUserID column resolved off telegram_accounts.
type OTPSender interface {
	SendOTP(ctx context.Context, telegramUserID int64, code string, locale string) error
}

// SetOTPSender wires the bot's delivery implementation in once cmd/api has
// built it. A Service with no sender set (the zero value, e.g. in a test
// that never calls RequestOtp, or before T4 wires the real one) treats a
// send as a no-op rather than a nil-pointer panic — RequestOtp's own
// contract is "202 either way", so a missing sender must never surface as
// a 500.
//
// s.otpSender carries no lock: RequestOtp's delivery goroutine (below)
// reads it unsynchronized, so SetOTPSender must be called once, before the
// Service starts serving requests — never concurrently with a RequestOtp
// call already in flight. cmd/api's own startup sequence (build the
// Service, then SetOTPSender, then start the HTTP server) already
// satisfies this; it is not safe to call again later to swap the sender
// while traffic is live.
func (s *Service) SetOTPSender(sender OTPSender) {
	s.otpSender = sender
}

// generateOTPCode draws a uniformly random 6-digit code (000000-999999,
// crypto/rand — never math/rand for anything that gates authentication),
// zero-padded so every code is exactly otpCodeDigits characters.
func generateOTPCode() (string, error) {
	upperBound := big.NewInt(1)
	for i := 0; i < otpCodeDigits; i++ {
		upperBound.Mul(upperBound, big.NewInt(10))
	}
	n, err := rand.Int(rand.Reader, upperBound)
	if err != nil {
		return "", fmt.Errorf("auth: generate otp code: %w", err)
	}
	return fmt.Sprintf("%0*d", otpCodeDigits, n.Int64()), nil
}

// hashOTPCode hashes a 6-digit code with a freshly generated random salt,
// returning salt||sha256(salt||code) as the single blob otp_codes.code_hash
// stores. SHA-256, not argon2id: unlike a password, an OTP code is
// deliberately short-lived (otpTTL), single use and capped at
// otpMaxAttempts online guesses (verifyOTPHash's caller, VerifyOtp,
// enforces that), so argon2id's per-hash cost buys nothing here — the salt
// exists only so a leaked otp_codes table can't be matched against one
// precomputed table of all 10^otpCodeDigits hashes shared across every row.
func hashOTPCode(code string) ([]byte, error) {
	salt := make([]byte, otpSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("auth: generate otp salt: %w", err)
	}
	sum := sha256.Sum256(append(append([]byte{}, salt...), code...))
	return append(salt, sum[:]...), nil
}

// verifyOTPHash reports whether code matches stored, a hashOTPCode blob,
// comparing in constant time. A stored value of the wrong length (never
// produced by hashOTPCode, but defensive against a corrupted row) is
// simply "no match" rather than a panic.
func verifyOTPHash(stored []byte, code string) bool {
	if len(stored) != otpSaltLen+sha256.Size {
		return false
	}
	salt, want := stored[:otpSaltLen], stored[otpSaltLen:]
	sum := sha256.Sum256(append(append([]byte{}, salt...), code...))
	return subtle.ConstantTimeCompare(sum[:], want) == 1
}

// selectorEncoding is the base32 alphabet newSelectorToken/parseSelectorToken
// use for both halves of a selector token — uppercase letters and digits
// 2-7 only (RFC 4648 §6), which keeps a Telegram deep-link code
// (telegram.go's CreateTelegramLink) inside the character set Telegram's
// bot deep-link `start` payload is documented to accept, without needing
// to percent-encode anything.
var selectorEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// userSelectorLen is the fixed length of base32(a 16-byte UUID) with no
// padding (128 bits / 5 bits-per-char, rounded up): always exactly this
// many characters, which is what lets parseSelectorToken split a token
// into its selector and verifier halves positionally, no separator needed.
const userSelectorLen = 26

// newSelectorToken builds a "selector.verifier" token — no literal
// separator, just userID's base32 encoding (always userSelectorLen chars)
// immediately followed by a freshly random verifier's base32 encoding —
// for a case where the row it will let the caller find is looked up by
// (shopID, userID, purpose) via GetActiveOTPCode, not by a hash (there is
// no "find otp_codes by code_hash" query, and this package's file scope
// does not extend to api/db/**): CompleteLink (telegram.go) and
// ResetPassword (below) both already know, or can recover from the token
// itself, the userID GetActiveOTPCode needs — they only need the token to
// additionally prove the caller holds the matching secret, which is what
// the verifier half (hashed with sha256, not hashOTPCode's salted variant:
// verifierBytes*8 bits of entropy makes a precomputed table infeasible
// without a salt) is for. Returns the full token (given to the caller) and
// the hash of only its verifier half (what CreateOTPCode.CodeHash stores).
func newSelectorToken(userID uuid.UUID, verifierBytes int) (token string, hash []byte, err error) {
	raw := make([]byte, verifierBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("auth: generate selector token verifier: %w", err)
	}
	verifier := selectorEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	return selectorEncoding.EncodeToString(userID[:]) + verifier, sum[:], nil
}

// errSelectorTokenMalformed is returned by parseSelectorToken for a token
// that is too short or whose selector half doesn't decode to a 16-byte
// UUID — never wrapped into a client-visible message on its own; every
// caller maps it to the same apierr.Unauthenticated() a wrong/expired code
// gets, so a malformed token can't be told apart from a merely-wrong one.
var errSelectorTokenMalformed = errors.New("auth: selector token malformed")

// parseSelectorToken splits a newSelectorToken token back into the userID
// its first userSelectorLen characters encode and the verifier string
// (everything after) the caller still has to hash and compare against the
// stored otp_codes.code_hash — parseSelectorToken itself only decodes, it
// never touches the database or does the hash comparison.
func parseSelectorToken(token string) (userID uuid.UUID, verifier string, err error) {
	if len(token) <= userSelectorLen {
		return uuid.UUID{}, "", errSelectorTokenMalformed
	}
	idBytes, err := selectorEncoding.DecodeString(token[:userSelectorLen])
	if err != nil || len(idBytes) != 16 {
		return uuid.UUID{}, "", errSelectorTokenMalformed
	}
	id, err := uuid.FromBytes(idBytes)
	if err != nil {
		return uuid.UUID{}, "", errSelectorTokenMalformed
	}
	return id, token[userSelectorLen:], nil
}

// RequestOtp delivers a fresh OTP code to username's linked Telegram
// account for purpose (only db.OtpPurposePasswordReset is implemented in
// Phase 7 — handler_otp.go's validation rejects anything else before this
// is ever called). It always succeeds from the caller's point of view
// (handler_otp.go answers 202 regardless) unless the request is rate
// limited or a genuine infrastructure error happens: an unknown username,
// an inactive user or one with no linked Telegram account all silently do
// nothing, by design (docs/05-API.md § Auth spec: "no enumeration").
func (s *Service) RequestOtp(ctx context.Context, username string, purpose db.OtpPurpose, ip *netip.Addr) error {
	now := time.Now()

	if ok, retryAfter := s.otpRequestIPLimiter.allow(ipKey(ip), now); !ok {
		return apierr.RateLimited(retryAfterSeconds(retryAfter))
	}
	if ok, retryAfter := s.otpRequestUserLimiter.allow(userLimiterKey(username), now); !ok {
		return apierr.RateLimited(retryAfterSeconds(retryAfter))
	}

	user, err := s.q.GetUserByUsername(ctx, db.GetUserByUsernameParams{ShopID: s.shopID, Username: username})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("auth: request otp: look up user: %w", err)
	}
	if !user.IsActive {
		return nil
	}

	tgAccount, err := s.q.GetTelegramAccountByUserID(ctx, db.GetTelegramAccountByUserIDParams{ShopID: s.shopID, UserID: user.ID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("auth: request otp: look up telegram account: %w", err)
	}

	code, err := generateOTPCode()
	if err != nil {
		return fmt.Errorf("auth: request otp: %w", err)
	}
	hash, err := hashOTPCode(code)
	if err != nil {
		return fmt.Errorf("auth: request otp: %w", err)
	}

	if _, err := s.q.ExpireOTPCodes(ctx, db.ExpireOTPCodesParams{ShopID: s.shopID, UserID: user.ID, Purpose: purpose}); err != nil {
		return fmt.Errorf("auth: request otp: expire earlier codes: %w", err)
	}

	id, err := uuid.NewV7()
	if err != nil {
		id = uuid.New()
	}
	if _, err := s.q.CreateOTPCode(ctx, db.CreateOTPCodeParams{
		ID: id, ShopID: s.shopID, UserID: user.ID, Purpose: purpose,
		CodeHash: hash, ExpiresAt: now.Add(otpTTL),
	}); err != nil {
		return fmt.Errorf("auth: request otp: create code: %w", err)
	}

	if s.otpSender == nil {
		return nil
	}
	// Delivery runs off the request path (Review MAJOR 2): calling
	// SendOTP synchronously here made RequestOtp's own latency depend on
	// a Telegram round trip for a real, linked user while every other
	// case (unknown username, no linked account, ...) returned almost
	// immediately — a timing side channel that enumerates which
	// usernames are actually linked, on top of tying up the request for
	// as long as delivery (or a hung sender) took. This narrows that
	// channel; it does not remove it — a linked user's request still runs
	// two more synchronous queries (GetUserByUsername, then
	// GetTelegramAccountByUserID) than an unknown-username one before
	// this point, so some difference in latency remains observable, just
	// no longer the full width of a Telegram round trip. The goroutine gets its
	// own bounded context, deliberately derived from context.Background()
	// rather than ctx: ctx belongs to the HTTP request, which is already
	// on its way to completing by the time this runs, and must not cut
	// delivery short the moment the client sees its 202. Delivery failure
	// is logged as a class only — never the code itself (hard rule 9).
	go func() { //nolint:gosec // G118: context.Background() is deliberate here, not a mistake — see the comment above.
		sendCtx, cancel := context.WithTimeout(context.Background(), otpDeliveryTimeout)
		defer cancel()
		if err := s.otpSender.SendOTP(sendCtx, tgAccount.TelegramUserID, code, string(user.Locale)); err != nil {
			slog.Error("auth: otp delivery failed", "purpose", string(purpose), "reason", "send_error")
		}
	}()
	return nil
}

// VerifyOtp checks code against the newest active db.OtpPurposePasswordReset
// code for username, on success exchanging it for a single-use,
// actionTokenTTL action token (db.OtpPurposeConfirmAction) that
// ResetPassword accepts. Every rejection reason — unknown username, no
// active code, expired code, over-attempted code, wrong code — answers the
// exact same apierr.Unauthenticated(), so a client can't use the response
// to enumerate which one happened (docs/05-API.md's own description of
// this endpoint).
func (s *Service) VerifyOtp(ctx context.Context, username string, purpose db.OtpPurpose, code string, ip *netip.Addr) (actionToken string, expiresAt time.Time, err error) {
	now := time.Now()

	if ok, retryAfter := s.otpVerifyIPLimiter.allow(ipKey(ip), now); !ok {
		return "", time.Time{}, apierr.RateLimited(retryAfterSeconds(retryAfter))
	}
	if ok, retryAfter := s.otpVerifyUserLimiter.allow(userLimiterKey(username), now); !ok {
		return "", time.Time{}, apierr.RateLimited(retryAfterSeconds(retryAfter))
	}

	user, err := s.q.GetUserByUsername(ctx, db.GetUserByUsernameParams{ShopID: s.shopID, Username: username})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", time.Time{}, apierr.Unauthenticated()
		}
		return "", time.Time{}, fmt.Errorf("auth: verify otp: look up user: %w", err)
	}
	if !user.IsActive {
		return "", time.Time{}, apierr.Unauthenticated()
	}

	// otpAttemptLock (above): serializes the read-check-increment section
	// below per (shop, user, purpose) so concurrent guesses against the
	// same code can't each read the same stale otp.Attempts and all
	// increment past otpMaxAttempts.
	unlock := s.otpAttemptLock.lock(otpAttemptLockKey(s.shopID, user.ID, purpose))
	defer unlock()

	otp, err := s.q.GetActiveOTPCode(ctx, db.GetActiveOTPCodeParams{ShopID: s.shopID, UserID: user.ID, Purpose: purpose})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", time.Time{}, apierr.Unauthenticated()
		}
		return "", time.Time{}, fmt.Errorf("auth: verify otp: look up code: %w", err)
	}

	if otp.Attempts >= otpMaxAttempts {
		return "", time.Time{}, apierr.Unauthenticated()
	}

	if !verifyOTPHash(otp.CodeHash, code) {
		// Increment first, decide on the value the database actually
		// returned — never on the otp.Attempts snapshot read above, which
		// concurrent wrong-code submissions would all still see as
		// "under the cap" (Review MAJOR 4: read-then-write here let each
		// concurrent guess through for free). Burn the code the instant
		// the cap is reached so a delayed correct submission can't use it
		// either, and so GetActiveOTPCode's used_at IS NULL filter alone
		// answers every later attempt without a separate cap check.
		updated, err := s.q.IncrementOTPAttempts(ctx, db.IncrementOTPAttemptsParams{ShopID: s.shopID, ID: otp.ID})
		if err != nil {
			return "", time.Time{}, fmt.Errorf("auth: verify otp: increment attempts: %w", err)
		}
		if updated.Attempts >= otpMaxAttempts {
			if _, err := s.q.MarkOTPUsed(ctx, db.MarkOTPUsedParams{ShopID: s.shopID, ID: otp.ID}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return "", time.Time{}, fmt.Errorf("auth: verify otp: mark exhausted code used: %w", err)
			}
		}
		return "", time.Time{}, apierr.Unauthenticated()
	}

	if _, err := s.q.MarkOTPUsed(ctx, db.MarkOTPUsedParams{ShopID: s.shopID, ID: otp.ID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Already used — a replayed or racing second VerifyOtp call
			// for the same code (Review MINOR 5: this used to fall
			// through to the generic %w wrap below and surface as a 500;
			// it is exactly the same "code no longer valid" case
			// ResetPassword's own MarkOTPUsed call already treats as
			// Unauthenticated).
			return "", time.Time{}, apierr.Unauthenticated()
		}
		return "", time.Time{}, fmt.Errorf("auth: verify otp: mark used: %w", err)
	}

	token, hash, err := newSelectorToken(user.ID, actionVerifierN)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: verify otp: %w", err)
	}

	if _, err := s.q.ExpireOTPCodes(ctx, db.ExpireOTPCodesParams{ShopID: s.shopID, UserID: user.ID, Purpose: db.OtpPurposeConfirmAction}); err != nil {
		return "", time.Time{}, fmt.Errorf("auth: verify otp: expire earlier action tokens: %w", err)
	}

	tokenID, err := uuid.NewV7()
	if err != nil {
		tokenID = uuid.New()
	}
	tokenExpiresAt := now.Add(actionTokenTTL)
	if _, err := s.q.CreateOTPCode(ctx, db.CreateOTPCodeParams{
		ID: tokenID, ShopID: s.shopID, UserID: user.ID, Purpose: db.OtpPurposeConfirmAction,
		CodeHash: hash, ExpiresAt: tokenExpiresAt,
	}); err != nil {
		return "", time.Time{}, fmt.Errorf("auth: verify otp: create action token: %w", err)
	}

	return token, tokenExpiresAt, nil
}

// ResetPassword validates actionToken (a newSelectorToken minted by
// VerifyOtp for db.OtpPurposeConfirmAction), sets newPassword and revokes
// every session belonging to the user the token names — actionToken is
// itself the credential here (docs/05-API.md: "this endpoint needs no
// session"), so there is no "current" session to spare the way
// SetStaffPassword's caller-is-owner flow has none to spare either
// (shop.Service.SetStaffPassword's own doc comment): every session goes.
// A malformed, expired, already-used or mismatched token all answer the
// same apierr.Unauthenticated(), same reasoning as VerifyOtp.
//
// ResetPassword needs no session — actionToken is itself the credential —
// so unlike every other write in this package it has no username to key a
// per-account limiter on; it shares s.ipLimiter, Login's own per-IP budget,
// instead (D-118, 2026-09-09): the action token already carries
// actionVerifierN bytes of entropy, but that alone was the endpoint's only
// defense before this change, exactly the gap VerifyLoginWidget's authDate
// window closes for the widget (D-117's own reasoning).
func (s *Service) ResetPassword(ctx context.Context, actionToken, newPassword string, ip *netip.Addr) error {
	if ok, retryAfter := s.ipLimiter.allow(ipKey(ip), time.Now()); !ok {
		return apierr.RateLimited(retryAfterSeconds(retryAfter))
	}

	userID, verifier, err := parseSelectorToken(actionToken)
	if err != nil {
		return apierr.Unauthenticated()
	}

	otp, err := s.q.GetActiveOTPCode(ctx, db.GetActiveOTPCodeParams{ShopID: s.shopID, UserID: userID, Purpose: db.OtpPurposeConfirmAction})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.Unauthenticated()
		}
		return fmt.Errorf("auth: reset password: look up action token: %w", err)
	}

	sum := sha256.Sum256([]byte(verifier))
	if subtle.ConstantTimeCompare(sum[:], otp.CodeHash) != 1 {
		return apierr.Unauthenticated()
	}

	hash, err := Hash(newPassword)
	if err != nil {
		return err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("auth: reset password: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	if _, err := qtx.MarkOTPUsed(ctx, db.MarkOTPUsedParams{ShopID: s.shopID, ID: otp.ID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Already used — a replayed or racing second ResetPassword
			// call with the same token. Same outward answer as any other
			// invalid token.
			return apierr.Unauthenticated()
		}
		return fmt.Errorf("auth: reset password: mark action token used: %w", err)
	}

	// Re-check is_active (Review MINOR 10): the action token may have
	// been minted while the user was active and redeemed after an owner
	// deactivated their account in between — a deactivated user must not
	// be able to set a new password any more than they can log in.
	user, err := qtx.GetUserByID(ctx, db.GetUserByIDParams{ShopID: s.shopID, ID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.Unauthenticated()
		}
		return fmt.Errorf("auth: reset password: load user: %w", err)
	}
	if !user.IsActive {
		return apierr.Unauthenticated()
	}

	if err := qtx.SetUserPassword(ctx, db.SetUserPasswordParams{ShopID: s.shopID, ID: userID, PasswordHash: hash}); err != nil {
		return fmt.Errorf("auth: reset password: set password: %w", err)
	}
	if err := qtx.RevokeAllUserSessions(ctx, db.RevokeAllUserSessionsParams{ShopID: s.shopID, UserID: userID}); err != nil {
		return fmt.Errorf("auth: reset password: revoke sessions: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("auth: reset password: commit: %w", err)
	}
	return nil
}
