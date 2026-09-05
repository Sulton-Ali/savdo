package crm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// maxFullNameLength bounds customers.full_name. The contract has no JSON
// Schema maxLength for it, so — same situation as shop.maxNameLength
// (shop/handler.go) — this is the one place the limit is agreed on rather
// than left unbounded.
const maxFullNameLength = 120

// validateFullName trims raw and reports the required/too_long
// apierr.Validation failure (details.fields.fullName) if it's empty or
// over maxFullNameLength after trimming; otherwise returns the trimmed
// name with a nil error.
func validateFullName(raw string) (string, *apierr.Error) {
	trimmed := strings.TrimSpace(raw)
	switch {
	case trimmed == "":
		return "", apierr.Validation(map[string]string{"fullName": "required"})
	case utf8.RuneCountInString(trimmed) > maxFullNameLength:
		return "", apierr.Validation(map[string]string{"fullName": "too_long"})
	default:
		return trimmed, nil
	}
}

// trimmedPhone trims surrounding whitespace only. Phone numbers are
// otherwise stored exactly as given — no digit-only normalisation, no
// assumed country code — since docs/04-DATA-MODEL.md doesn't specify one
// and guessing wrong would silently corrupt a real phone number. nil
// stays nil.
func trimmedPhone(raw *string) *string {
	if raw == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*raw)
	return &trimmed
}

// ListCustomers lists the shop's customers. Any authenticated role (owner,
// manager, cashier — docs/04-DATA-MODEL.md § 7 "Customers CRUD": cashier
// gets create/read). Cursor-paginated; `?q` matches full_name or phone by
// substring (ILIKE), same convention as ListSuppliers.
func (h *Handler) ListCustomers(ctx context.Context, req gen.ListCustomersRequestObject) (gen.ListCustomersResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}

	limit := clampLimit(req.Params.Limit)
	cCreatedAt, cID, err := decodeCursor(req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	cursorCreatedAt, cursorID := cursorPtr(cCreatedAt, cID)

	rows, err := h.svc.q.ListCustomers(ctx, db.ListCustomersParams{
		ShopID: authCtx.ShopID, Q: req.Params.Q,
		CursorCreatedAt: cursorCreatedAt, CursorID: cursorID, Limit: limit + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("crm: list customers: %w", err)
	}

	items, nextCursor := paginateCustomers(rows, limit)
	genItems := make([]gen.Customer, len(items))
	for i, r := range items {
		genItems[i] = toGenCustomer(r)
	}
	return gen.ListCustomers200JSONResponse(gen.CustomerList{Items: genItems, NextCursor: nullableString(nextCursor)}), nil
}

// GetCustomer gets a customer by id. Any authenticated role. 404 for
// another shop's id or a soft-deleted customer.
func (h *Handler) GetCustomer(ctx context.Context, req gen.GetCustomerRequestObject) (gen.GetCustomerResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}

	row, err := h.svc.q.GetCustomer(ctx, db.GetCustomerParams{ShopID: authCtx.ShopID, ID: req.Id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("customer")
		}
		return nil, fmt.Errorf("crm: get customer: %w", err)
	}
	return gen.GetCustomer200JSONResponse(toGenCustomer(row)), nil
}

// CreateCustomer creates a customer. Any authenticated role (owner,
// manager, cashier). 409 CONFLICT details.field: phone for a duplicate
// active phone (unique (shop_id, phone) where deleted_at is null and
// phone is not null).
func (h *Handler) CreateCustomer(ctx context.Context, req gen.CreateCustomerRequestObject) (gen.CreateCustomerResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	body := req.Body

	fullName, valErr := validateFullName(body.FullName)
	if valErr != nil {
		return nil, valErr
	}

	var tags []string
	if body.Tags != nil {
		tags = *body.Tags
	}

	created, err := h.svc.q.CreateCustomer(ctx, db.CreateCustomerParams{
		ID: newID(), ShopID: authCtx.ShopID, FullName: fullName,
		Phone: trimmedPhone(body.Phone), TelegramUsername: body.TelegramUsername, Note: body.Note,
		Tags: tags,
	})
	if err != nil {
		if apiErr, ok := mapWriteError(err); ok {
			return nil, apiErr
		}
		return nil, fmt.Errorf("crm: create customer: %w", err)
	}
	return gen.CreateCustomer201JSONResponse(toGenCustomer(created)), nil
}

// UpdateCustomer updates a customer. Requires customers.write (manager+).
// Explicit `null` for phone/telegramUsername/note clears the field
// (D-35); absent leaves it unchanged. `tags`, when provided, replaces the
// full array.
func (h *Handler) UpdateCustomer(ctx context.Context, req gen.UpdateCustomerRequestObject) (gen.UpdateCustomerResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermCustomersWrite); err != nil {
		return nil, err
	}
	body := req.Body

	var fullName *string
	if body.FullName != nil {
		trimmed, valErr := validateFullName(*body.FullName)
		if valErr != nil {
			return nil, valErr
		}
		fullName = &trimmed
	}

	phone := optionalString(body.Phone)
	telegramUsername := optionalString(body.TelegramUsername)
	note := optionalString(body.Note)

	params := db.UpdateCustomerParams{FullName: fullName, ShopID: authCtx.ShopID, ID: req.Id}
	if phone != nil {
		params.ClearPhone = *phone == nil
		params.Phone = trimmedPhone(*phone)
	}
	if telegramUsername != nil {
		params.ClearTelegramUsername = *telegramUsername == nil
		params.TelegramUsername = *telegramUsername
	}
	if note != nil {
		params.ClearNote = *note == nil
		params.Note = *note
	}
	if body.Tags != nil {
		params.Tags = *body.Tags
	}

	updated, err := h.svc.q.UpdateCustomer(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("customer")
		}
		if apiErr, ok := mapWriteError(err); ok {
			return nil, apiErr
		}
		return nil, fmt.Errorf("crm: update customer: %w", err)
	}
	return gen.UpdateCustomer200JSONResponse(toGenCustomer(updated)), nil
}

// DeleteCustomer soft-deletes a customer. Requires customers.write
// (manager+). A sale referencing this customer keeps working — the FK is
// to customers(id), independent of deleted_at (same reasoning as
// DeleteSupplier).
func (h *Handler) DeleteCustomer(ctx context.Context, req gen.DeleteCustomerRequestObject) (gen.DeleteCustomerResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermCustomersWrite); err != nil {
		return nil, err
	}

	if _, err := h.svc.q.GetCustomer(ctx, db.GetCustomerParams{ShopID: authCtx.ShopID, ID: req.Id}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("customer")
		}
		return nil, fmt.Errorf("crm: get customer: %w", err)
	}

	if err := h.svc.q.SoftDeleteCustomer(ctx, db.SoftDeleteCustomerParams{ShopID: authCtx.ShopID, ID: req.Id}); err != nil {
		return nil, fmt.Errorf("crm: soft delete customer: %w", err)
	}
	return gen.DeleteCustomer204Response{}, nil
}
