package shop

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// Field length/pattern bounds not already expressed as JSON Schema
// constraints in contracts/openapi.yaml (which oapi-codegen does not
// enforce at runtime — see auth/handler.go's maxLoginUsernameLength
// comment) or that need a business-vocabulary reason
// (required/invalid/too_short/too_long) rather than a raw message.
const (
	minUsernameLength = 3
	maxUsernameLength = 64
	minPasswordLength = 8
	maxPasswordLength = 128
)

var (
	usernamePattern = regexp.MustCompile(`^[a-z0-9._-]+$`)
	phonePattern    = regexp.MustCompile(`^\+?[0-9]{7,15}$`)
)

// ListStaff lists the shop's staff. Requires staff.manage (owner only).
func (h *Handler) ListStaff(ctx context.Context, req gen.ListStaffRequestObject) (gen.ListStaffResponseObject, error) {
	if _, ok := auth.FromContext(ctx); !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermStaffManage); err != nil {
		return nil, err
	}
	authCtx, _ := auth.FromContext(ctx)

	limit := clampLimit(req.Params.Limit)
	createdAt, id, err := decodeCursor(req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	cursorCreatedAt, cursorID := cursorPtr(createdAt, id)

	rows, err := h.svc.ListStaff(ctx, authCtx.ShopID, limit+1, cursorCreatedAt, cursorID)
	if err != nil {
		return nil, fmt.Errorf("shop: list staff: %w", err)
	}

	items, nextCursor := paginateT(rows, limit, func(u db.User) (time.Time, uuid.UUID) { return u.CreatedAt, u.ID })
	genItems := make([]gen.User, len(items))
	for i, u := range items {
		genItems[i] = toGenUser(u)
	}
	return gen.ListStaff200JSONResponse(gen.UserList{Items: genItems, NextCursor: nextCursor}), nil
}

// validateUsername trims, lowercases and length/pattern-checks a
// candidate username, returning the normalized value and, when invalid,
// the vocabulary reason (required/too_short/too_long/invalid). Usernames
// are stored lowercase — login matches case-insensitively anyway
// (users.username is citext) — so normalizing here keeps what's stored
// and what a client displays in sync.
func validateUsername(raw string) (string, string) {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case trimmed == "":
		return "", "required"
	case utf8.RuneCountInString(trimmed) < minUsernameLength:
		return "", "too_short"
	case utf8.RuneCountInString(trimmed) > maxUsernameLength:
		return "", "too_long"
	case !usernamePattern.MatchString(trimmed):
		return "", "invalid"
	default:
		return trimmed, ""
	}
}

// validatePassword length-checks a candidate password against the
// vocabulary reasons; auth.Hash is only ever called once this passes, so
// its own (differently-worded) minimum-length error never reaches a
// client.
func validatePassword(raw string) string {
	switch n := utf8.RuneCountInString(raw); {
	case n == 0:
		return "required"
	case n < minPasswordLength:
		return "too_short"
	case n > maxPasswordLength:
		return "too_long"
	default:
		return ""
	}
}

// validateFullName length-checks a candidate full name.
func validateFullName(raw string) (string, string) {
	trimmed := strings.TrimSpace(raw)
	switch {
	case trimmed == "":
		return "", "required"
	case utf8.RuneCountInString(trimmed) > maxNameLength:
		return "", "too_long"
	default:
		return trimmed, ""
	}
}

// validatePhone reports the vocabulary reason for a non-empty phone that
// fails the E.164-ish pattern, or "" when phone is nil/empty (optional)
// or valid.
func validatePhone(phone *string) string {
	if phone == nil || *phone == "" {
		return ""
	}
	if !phonePattern.MatchString(*phone) {
		return "invalid"
	}
	return ""
}

// CreateStaff creates a manager or cashier. Requires staff.manage (owner
// only).
func (h *Handler) CreateStaff(ctx context.Context, req gen.CreateStaffRequestObject) (gen.CreateStaffResponseObject, error) {
	if _, ok := auth.FromContext(ctx); !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermStaffManage); err != nil {
		return nil, err
	}
	authCtx, _ := auth.FromContext(ctx)

	body := req.Body
	fields := map[string]string{}

	username, usernameReason := validateUsername(body.Username)
	if usernameReason != "" {
		fields["username"] = usernameReason
	}

	if reason := validatePassword(body.Password); reason != "" {
		fields["password"] = reason
	}

	fullName, fullNameReason := validateFullName(body.FullName)
	if fullNameReason != "" {
		fields["fullName"] = fullNameReason
	}

	if reason := validatePhone(body.Phone); reason != "" {
		fields["phone"] = reason
	}

	var role db.UserRole
	switch {
	case body.Role == "":
		fields["role"] = "required"
	case !body.Role.Valid():
		fields["role"] = "invalid"
	default:
		role = db.UserRole(body.Role)
	}

	var locale *string
	if body.Locale != nil {
		if !body.Locale.Valid() {
			fields["locale"] = "invalid"
		} else {
			s := string(*body.Locale)
			locale = &s
		}
	}

	if len(fields) > 0 {
		return nil, apierr.Validation(fields)
	}

	user, err := h.svc.CreateStaff(ctx, authCtx.ShopID, CreateStaffInput{
		Username: username,
		Password: body.Password,
		FullName: fullName,
		Phone:    body.Phone,
		Role:     role,
		Locale:   locale,
	})
	if err != nil {
		return nil, err
	}
	return gen.CreateStaff201JSONResponse(toGenUser(user)), nil
}

// UpdateStaff updates a staff member. Requires staff.manage (owner only).
// 404 when id doesn't belong to this shop; owner and self protections are
// enforced by the Service (it needs the target's current role to do so).
func (h *Handler) UpdateStaff(ctx context.Context, req gen.UpdateStaffRequestObject) (gen.UpdateStaffResponseObject, error) {
	if _, ok := auth.FromContext(ctx); !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermStaffManage); err != nil {
		return nil, err
	}
	authCtx, _ := auth.FromContext(ctx)

	body := req.Body
	fields := map[string]string{}

	var fullName *string
	if body.FullName != nil {
		trimmed, reason := validateFullName(*body.FullName)
		if reason != "" {
			fields["fullName"] = reason
		} else {
			fullName = &trimmed
		}
	}

	if reason := validatePhone(body.Phone); reason != "" {
		fields["phone"] = reason
	}

	var role *db.UserRole
	if body.Role != nil {
		if !body.Role.Valid() {
			fields["role"] = "invalid"
		} else {
			r := db.UserRole(*body.Role)
			role = &r
		}
	}

	var locale *string
	if body.Locale != nil {
		if !body.Locale.Valid() {
			fields["locale"] = "invalid"
		} else {
			s := string(*body.Locale)
			locale = &s
		}
	}

	if len(fields) > 0 {
		return nil, apierr.Validation(fields)
	}

	updated, err := h.svc.UpdateStaff(ctx, authCtx.ShopID, authCtx.UserID, req.Id, StaffPatchInput{
		FullName: fullName, Phone: body.Phone, Role: role, IsActive: body.IsActive, Locale: locale,
	})
	if err != nil {
		return nil, err
	}
	return gen.UpdateStaff200JSONResponse(toGenUser(updated)), nil
}

// SetStaffPassword sets a staff member's password. Requires staff.manage
// (owner only); the owner may reset their own password here too (D-28).
func (h *Handler) SetStaffPassword(ctx context.Context, req gen.SetStaffPasswordRequestObject) (gen.SetStaffPasswordResponseObject, error) {
	if _, ok := auth.FromContext(ctx); !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermStaffManage); err != nil {
		return nil, err
	}
	authCtx, _ := auth.FromContext(ctx)

	if reason := validatePassword(req.Body.Password); reason != "" {
		return nil, apierr.Validation(map[string]string{"password": reason})
	}

	if err := h.svc.SetStaffPassword(ctx, authCtx.ShopID, req.Id, req.Body.Password); err != nil {
		return nil, err
	}
	return gen.SetStaffPassword204Response{}, nil
}
