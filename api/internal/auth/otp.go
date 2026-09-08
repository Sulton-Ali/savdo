package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/netip"
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
	// Delivery failure is logged as a class only — never the code itself
	// (hard rule 9) — and never fails the request: RequestOtp's contract
	// is "202 either way" (docs/05-API.md § Auth spec).
	if err := s.otpSender.SendOTP(ctx, tgAccount.TelegramUserID, code, string(user.Locale)); err != nil {
		slog.Error("auth: otp delivery failed", "purpose", string(purpose), "reason", "send_error")
	}
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
		if _, err := s.q.IncrementOTPAttempts(ctx, db.IncrementOTPAttemptsParams{ShopID: s.shopID, ID: otp.ID}); err != nil {
			return "", time.Time{}, fmt.Errorf("auth: verify otp: increment attempts: %w", err)
		}
		return "", time.Time{}, apierr.Unauthenticated()
	}

	if _, err := s.q.MarkOTPUsed(ctx, db.MarkOTPUsedParams{ShopID: s.shopID, ID: otp.ID}); err != nil {
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
func (s *Service) ResetPassword(ctx context.Context, actionToken, newPassword string) error {
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
