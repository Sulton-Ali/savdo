package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// telegramAuthMaxAge is how old a Login Widget payload's authDate may be
// before VerifyLoginWidget rejects it (ADR-005), matching Telegram's own
// documented advice ("you can additionally check the auth_date field ...
// to prevent the use of outdated data" —
// https://core.telegram.org/widgets/login-legacy#checking-authorization):
// 24 hours.
const telegramAuthMaxAge = 24 * time.Hour

// errTelegramAuthInvalid is VerifyLoginWidget's one failure value — a bad
// HMAC, a malformed id or a stale authDate are all the same "not valid"
// to a caller, exactly like Login's own single apierr.Unauthenticated()
// for every rejection reason.
var errTelegramAuthInvalid = errors.New("auth: telegram login widget payload invalid")

// widgetDataCheckString builds Telegram's data_check_string for payload:
// every field it actually carries except hash, formatted "key=value",
// sorted alphabetically by key, joined with "\n".
func widgetDataCheckString(payload gen.TelegramAuthRequest) string {
	fields := map[string]string{
		"id":        payload.Id,
		"auth_date": strconv.Itoa(payload.AuthDate),
	}
	if payload.FirstName != nil && *payload.FirstName != "" {
		fields["first_name"] = *payload.FirstName
	}
	if payload.LastName != nil && *payload.LastName != "" {
		fields["last_name"] = *payload.LastName
	}
	if payload.Username != nil && *payload.Username != "" {
		fields["username"] = *payload.Username
	}
	if payload.PhotoUrl != nil && *payload.PhotoUrl != "" {
		fields["photo_url"] = *payload.PhotoUrl
	}

	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+"="+fields[k])
	}
	return strings.Join(lines, "\n")
}

// widgetSignature computes hex(HMAC-SHA-256(SHA-256(botToken),
// widgetDataCheckString(payload))) — the signature Telegram's own
// documented algorithm says payload.Hash must equal. Split out of
// VerifyLoginWidget so a test can check this half (the HMAC math itself)
// against an externally computed vector without also having to satisfy
// VerifyLoginWidget's separate authDate-freshness check.
func widgetSignature(payload gen.TelegramAuthRequest, botToken string) string {
	secretKey := sha256.Sum256([]byte(botToken))
	mac := hmac.New(sha256.New, secretKey[:])
	mac.Write([]byte(widgetDataCheckString(payload)))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyLoginWidget checks payload's HMAC-SHA-256 signature against
// botToken per Telegram's documented algorithm
// (https://core.telegram.org/widgets/login-legacy#checking-authorization,
// the archived widget doc — the current core.telegram.org/widgets/login
// describes an unrelated OIDC flow this app does not use):
//
//   - data_check_string is every field the payload actually carries except
//     hash, formatted "key=value", sorted alphabetically by key, joined
//     with "\n" (widgetDataCheckString);
//   - secret_key is SHA-256(botToken);
//   - the signature is hex(HMAC-SHA-256(secret_key, data_check_string))
//     (widgetSignature), compared to payload.Hash in constant time.
//
// It also rejects a payload whose authDate is more than telegramAuthMaxAge
// old. On success it returns the Telegram user id (parsed from payload.Id,
// carried as a decimal string end to end for JS safety) and the Telegram
// username, if the widget sent one.
func VerifyLoginWidget(payload gen.TelegramAuthRequest, botToken string) (telegramUserID int64, username string, err error) {
	want := widgetSignature(payload, botToken)
	got := strings.ToLower(strings.TrimSpace(payload.Hash))
	if len(want) != len(got) || subtle.ConstantTimeCompare([]byte(want), []byte(got)) != 1 {
		return 0, "", errTelegramAuthInvalid
	}

	if time.Since(time.Unix(int64(payload.AuthDate), 0)) > telegramAuthMaxAge {
		return 0, "", errTelegramAuthInvalid
	}

	id, err := strconv.ParseInt(payload.Id, 10, 64)
	if err != nil {
		return 0, "", errTelegramAuthInvalid
	}

	uname := ""
	if payload.Username != nil {
		uname = *payload.Username
	}
	return id, uname, nil
}

// AuthenticateTelegram verifies payload (VerifyLoginWidget) and, only for
// a Telegram id already linked to a user (telegram_accounts), starts a
// session exactly like Login for client: web — same LoginResult shape,
// same startSession tail — since the Login Widget is a web-only flow
// (ADR-005; the bot deep-link handles mobile linking instead). It never
// creates a user. A bad HMAC, a stale authDate, an unlinked Telegram id
// and an inactive linked user's account all answer the same
// apierr.Unauthenticated(), so a client cannot tell them apart.
func (s *Service) AuthenticateTelegram(ctx context.Context, payload gen.TelegramAuthRequest, userAgent string, ip *netip.Addr) (LoginResult, error) {
	now := time.Now()
	if ok, retryAfter := s.ipLimiter.allow(ipKey(ip), now); !ok {
		return LoginResult{}, apierr.RateLimited(retryAfterSeconds(retryAfter))
	}

	telegramUserID, _, err := VerifyLoginWidget(payload, s.cfg.TelegramBotToken)
	if err != nil {
		return LoginResult{}, apierr.Unauthenticated()
	}

	account, err := s.q.GetTelegramAccountByTelegramUserID(ctx, telegramUserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return LoginResult{}, apierr.Unauthenticated()
		}
		return LoginResult{}, fmt.Errorf("auth: authenticate telegram: look up link: %w", err)
	}
	if account.ShopID != s.shopID {
		// Single-shop MVP: telegram_accounts.shop_id is always s.shopID in
		// practice, but this Service never trusts that without checking —
		// same posture as every other shop_id filter (hard rule 1).
		return LoginResult{}, apierr.Unauthenticated()
	}

	user, err := s.q.GetUserByID(ctx, db.GetUserByIDParams{ShopID: s.shopID, ID: account.UserID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return LoginResult{}, apierr.Unauthenticated()
		}
		return LoginResult{}, fmt.Errorf("auth: authenticate telegram: load user: %w", err)
	}
	if !user.IsActive {
		return LoginResult{}, apierr.Unauthenticated()
	}

	return s.startSession(ctx, user, db.SessionClientWeb, userAgent, ip)
}

// telegramLinkVerifierBytes is how much random entropy a CreateTelegramLink
// code's verifier half carries (telegram.go's own newSelectorToken call) —
// 20 bytes (160 bits), the same as the action token's (otp.go's
// actionVerifierN), comfortably more than a brute-force attempt at
// CompleteLink's own otpMaxAttempts cap could ever need to be safe from.
const telegramLinkVerifierBytes = 20

// TelegramLinkResult is what a successful CreateTelegramLink returns: the
// code the caller passes back (handler_telegram.go's TelegramLinkCode) and
// when it expires.
type TelegramLinkResult struct {
	Code      string
	ExpiresAt time.Time
}

// CreateTelegramLink starts linking userID's account to a Telegram
// account: a single-use, 10-minute code (docs/05-API.md's own description
// of POST /auth/telegram/link) whose hash lands in otp_codes under
// db.OtpPurposeLinkTelegram, keyed the same "selector.verifier" way
// otp.go's action token is (newSelectorToken's own doc comment) — the bot
// (T4) calls CompleteLink with whatever it received after Telegram's own
// "link_" deep-link prefix, and CompleteLink has to resolve that back to a
// user without a database lookup keyed by hash.
func (s *Service) CreateTelegramLink(ctx context.Context, shopID, userID uuid.UUID) (TelegramLinkResult, error) {
	if _, err := s.q.ExpireOTPCodes(ctx, db.ExpireOTPCodesParams{ShopID: shopID, UserID: userID, Purpose: db.OtpPurposeLinkTelegram}); err != nil {
		return TelegramLinkResult{}, fmt.Errorf("auth: create telegram link: expire earlier codes: %w", err)
	}

	code, hash, err := newSelectorToken(userID, telegramLinkVerifierBytes)
	if err != nil {
		return TelegramLinkResult{}, fmt.Errorf("auth: create telegram link: %w", err)
	}

	id, err := uuid.NewV7()
	if err != nil {
		id = uuid.New()
	}
	expiresAt := time.Now().Add(actionTokenTTL)
	if _, err := s.q.CreateOTPCode(ctx, db.CreateOTPCodeParams{
		ID: id, ShopID: shopID, UserID: userID, Purpose: db.OtpPurposeLinkTelegram,
		CodeHash: hash, ExpiresAt: expiresAt,
	}); err != nil {
		return TelegramLinkResult{}, fmt.Errorf("auth: create telegram link: create code: %w", err)
	}

	return TelegramLinkResult{Code: code, ExpiresAt: expiresAt}, nil
}

// GetTelegramLink reports userID's current Telegram link status.
func (s *Service) GetTelegramLink(ctx context.Context, shopID, userID uuid.UUID) (linked bool, telegramUsername *string, err error) {
	account, err := s.q.GetTelegramAccountByUserID(ctx, db.GetTelegramAccountByUserIDParams{ShopID: shopID, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil, nil
		}
		return false, nil, fmt.Errorf("auth: get telegram link: %w", err)
	}
	return true, account.TelegramUsername, nil
}

// DeleteTelegramLink unlinks userID's Telegram account, if any. Idempotent:
// unlinking an account that was never linked is not an error.
func (s *Service) DeleteTelegramLink(ctx context.Context, shopID, userID uuid.UUID) error {
	if _, err := s.q.UnlinkTelegramAccount(ctx, db.UnlinkTelegramAccountParams{ShopID: shopID, UserID: userID}); err != nil {
		return fmt.Errorf("auth: delete telegram link: %w", err)
	}
	return nil
}

// TelegramLinker is what internal/bot (T4) needs to complete a
// CreateTelegramLink code once its owner starts a chat with the bot via
// the "link_<code>" deep link: CompleteLink is the one method of *Service
// that satisfies it.
type TelegramLinker interface {
	CompleteLink(ctx context.Context, code string, telegramUserID int64, username string) error
}

// ErrLinkCodeInvalid is CompleteLink's answer for a code that does not
// parse, has no matching active db.OtpPurposeLinkTelegram row, is
// expired, is already used, has hit otpMaxAttempts, or simply does not
// match the row it does resolve to — every one of those is the same
// "invalid or expired code" to the bot's user, so T4's handler can map
// this straight to one message without needing to distinguish further.
var ErrLinkCodeInvalid = errors.New("auth: telegram link code invalid or expired")

// ErrTelegramAlreadyLinked is CompleteLink's answer when telegramUserID is
// already linked to a different user (telegram_accounts.telegram_user_id
// is UNIQUE) — a distinct case from ErrLinkCodeInvalid because the code
// itself was fine; it's the Telegram account on the other end that can't
// be reused.
var ErrTelegramAlreadyLinked = errors.New("auth: telegram account already linked to a different user")

// CompleteLink implements TelegramLinker: it parses code (newSelectorToken's
// "selector.verifier" shape) to recover the userID that requested it, then
// verifies the verifier half against the active
// db.OtpPurposeLinkTelegram row for (shopID, userID) — attempts-limited to
// otpMaxAttempts the same way VerifyOtp's 6-digit code is — before linking
// telegramUserID/username to that user (LinkTelegramAccount) and marking
// the code used.
func (s *Service) CompleteLink(ctx context.Context, code string, telegramUserID int64, username string) error {
	userID, verifier, err := parseSelectorToken(code)
	if err != nil {
		return ErrLinkCodeInvalid
	}

	otp, err := s.q.GetActiveOTPCode(ctx, db.GetActiveOTPCodeParams{ShopID: s.shopID, UserID: userID, Purpose: db.OtpPurposeLinkTelegram})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrLinkCodeInvalid
		}
		return fmt.Errorf("auth: complete telegram link: look up code: %w", err)
	}

	if otp.Attempts >= otpMaxAttempts {
		return ErrLinkCodeInvalid
	}

	sum := sha256.Sum256([]byte(verifier))
	if subtle.ConstantTimeCompare(sum[:], otp.CodeHash) != 1 {
		if _, err := s.q.IncrementOTPAttempts(ctx, db.IncrementOTPAttemptsParams{ShopID: s.shopID, ID: otp.ID}); err != nil {
			return fmt.Errorf("auth: complete telegram link: increment attempts: %w", err)
		}
		return ErrLinkCodeInvalid
	}

	var uname *string
	if username != "" {
		uname = &username
	}

	linkID, err := uuid.NewV7()
	if err != nil {
		linkID = uuid.New()
	}
	if _, err := s.q.LinkTelegramAccount(ctx, db.LinkTelegramAccountParams{
		ID: linkID, UserID: userID, ShopID: s.shopID,
		TelegramUserID: telegramUserID, TelegramUsername: uname,
	}); err != nil {
		if field, ok := telegramConflictField(err); ok && field == "telegramUserID" {
			return ErrTelegramAlreadyLinked
		}
		return fmt.Errorf("auth: complete telegram link: link account: %w", err)
	}

	if _, err := s.q.MarkOTPUsed(ctx, db.MarkOTPUsedParams{ShopID: s.shopID, ID: otp.ID}); err != nil {
		return fmt.Errorf("auth: complete telegram link: mark code used: %w", err)
	}
	return nil
}

// telegramConflictField maps a unique-violation (pgx error code 23505) on
// telegram_accounts to the field that conflicted, by constraint name —
// never by parsing the driver's error message text (same reasoning as
// shop.conflictField/crm.conflictField). Only telegram_user_id can ever
// conflict here: LinkTelegramAccount's own INSERT ... ON CONFLICT
// (user_id) DO UPDATE already handles a re-link by the same user, so a
// 23505 reaching this function is always the *other* unique constraint.
func telegramConflictField(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return "", false
	}
	if pgErr.ConstraintName == "telegram_accounts_telegram_user_id_key" {
		return "telegramUserID", true
	}
	return "", false
}
