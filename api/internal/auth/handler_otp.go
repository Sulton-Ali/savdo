package auth

import (
	"context"
	"fmt"
	"unicode/utf8"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// maxOtpUsernameLength mirrors OtpRequest.username/OtpVerifyRequest.username's
// `maxLength: 64` in contracts/openapi.yaml (oapi-codegen's generated
// types carry no runtime validation for it — same situation
// maxLoginUsernameLength's own doc comment explains).
const maxOtpUsernameLength = 64

// toDBOtpPurpose converts a validated gen.OtpPurpose to db.OtpPurpose — the
// two enums share the same string values (contracts/openapi.yaml's
// OtpPurpose and 0020_otp_codes.sql's otp_purpose), so this is a plain
// cast once validation has already confirmed purpose is one of the known
// values.
func toDBOtpPurpose(p gen.OtpPurpose) db.OtpPurpose {
	return db.OtpPurpose(p)
}

// validateOtpPurpose reports whether purpose is the one OtpPurpose value
// Phase 7 actually implements through these two endpoints
// (contracts/openapi.yaml: "Only password_reset is implemented in Phase
// 7; OtpPurpose's other values are reserved for flows that do not go
// through this endpoint") — link_telegram goes through
// POST /auth/telegram/link instead (handler_telegram.go), and
// confirm_action has no other flow yet.
func validateOtpPurpose(purpose gen.OtpPurpose) bool {
	return purpose == gen.PasswordReset
}

// validateOtpRequest returns a field->reason map for a malformed
// OtpRequest: empty/over-long username, or a purpose other than
// password_reset.
func validateOtpRequest(body *gen.OtpRequest) map[string]string {
	fields := map[string]string{}
	switch {
	case body.Username == "":
		fields["username"] = "required"
	case utf8.RuneCountInString(body.Username) > maxOtpUsernameLength:
		fields["username"] = fmt.Sprintf("must be at most %d characters", maxOtpUsernameLength)
	}
	if !validateOtpPurpose(body.Purpose) {
		fields["purpose"] = "unsupported"
	}
	return fields
}

// RequestOtp requests an OTP code for a sensitive action. Always answers
// 202, whether or not username exists, is active or has a linked
// Telegram account (docs/05-API.md § Auth spec: "no enumeration").
func (h *Handler) RequestOtp(ctx context.Context, req gen.RequestOtpRequestObject) (gen.RequestOtpResponseObject, error) {
	if fields := validateOtpRequest(req.Body); len(fields) > 0 {
		return nil, apierr.Validation(fields)
	}

	ri, _ := requestInfoFromContext(ctx)

	if err := h.svc.RequestOtp(ctx, req.Body.Username, toDBOtpPurpose(req.Body.Purpose), ri.ip); err != nil {
		return nil, err
	}

	return gen.RequestOtp202Response{}, nil
}

// validateOtpVerifyRequest returns a field->reason map for a malformed
// OtpVerifyRequest: OtpRequest's own checks plus code's exact length
// (otp.go's otpCodeDigits — the contract's own `minLength`/`maxLength: 6`
// on OtpVerifyRequest.code).
func validateOtpVerifyRequest(body *gen.OtpVerifyRequest) map[string]string {
	fields := validateOtpRequest(&gen.OtpRequest{Username: body.Username, Purpose: body.Purpose})
	if utf8.RuneCountInString(body.Code) != otpCodeDigits {
		fields["code"] = fmt.Sprintf("must be exactly %d characters", otpCodeDigits)
	}
	return fields
}

// VerifyOtp exchanges a valid OTP code for a short-lived action token.
func (h *Handler) VerifyOtp(ctx context.Context, req gen.VerifyOtpRequestObject) (gen.VerifyOtpResponseObject, error) {
	if fields := validateOtpVerifyRequest(req.Body); len(fields) > 0 {
		return nil, apierr.Validation(fields)
	}

	ri, _ := requestInfoFromContext(ctx)

	actionToken, expiresAt, err := h.svc.VerifyOtp(ctx, req.Body.Username, toDBOtpPurpose(req.Body.Purpose), req.Body.Code, ri.ip)
	if err != nil {
		return nil, err
	}

	return gen.VerifyOtp200JSONResponse(gen.OtpVerifyResponse{
		ActionToken: actionToken,
		ExpiresAt:   expiresAt,
	}), nil
}

// validatePasswordResetRequest returns a field->reason map for a malformed
// PasswordResetRequest: an empty actionToken, or newPassword outside
// auth.Hash's own [MinPasswordLength, MaxPasswordLength] bounds — checked
// here so a too-short/too-long password is a 400 VALIDATION_FAILED before
// ResetPassword ever runs, not folded into ResetPassword's 401 (which is
// reserved for a bad token, per the contract's own description).
func validatePasswordResetRequest(body *gen.PasswordResetRequest) map[string]string {
	fields := map[string]string{}
	if body.ActionToken == "" {
		fields["actionToken"] = "required"
	}
	switch n := utf8.RuneCountInString(body.NewPassword); {
	case n < MinPasswordLength:
		fields["newPassword"] = "too_short"
	case n > MaxPasswordLength:
		fields["newPassword"] = "too_long"
	}
	return fields
}

// ResetPassword resets a password using an action token from VerifyOtp.
func (h *Handler) ResetPassword(ctx context.Context, req gen.ResetPasswordRequestObject) (gen.ResetPasswordResponseObject, error) {
	if fields := validatePasswordResetRequest(req.Body); len(fields) > 0 {
		return nil, apierr.Validation(fields)
	}

	if err := h.svc.ResetPassword(ctx, req.Body.ActionToken, req.Body.NewPassword); err != nil {
		return nil, err
	}

	return gen.ResetPassword204Response{}, nil
}
