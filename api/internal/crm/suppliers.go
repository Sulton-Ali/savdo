package crm

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// ListSuppliers lists the shop's suppliers. Requires suppliers.manage
// (manager+). Cursor-paginated; `?q` is a plain ILIKE substring match on
// name.
func (h *Handler) ListSuppliers(ctx context.Context, req gen.ListSuppliersRequestObject) (gen.ListSuppliersResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermSuppliersManage); err != nil {
		return nil, err
	}

	limit := clampLimit(req.Params.Limit)
	cCreatedAt, cID, err := decodeCursor(req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	cursorCreatedAt, cursorID := cursorPtr(cCreatedAt, cID)

	rows, err := h.svc.q.ListSuppliers(ctx, db.ListSuppliersParams{
		ShopID: authCtx.ShopID, Q: req.Params.Q,
		CursorCreatedAt: cursorCreatedAt, CursorID: cursorID, Limit: limit + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("crm: list suppliers: %w", err)
	}

	items, nextCursor := paginateSuppliers(rows, limit)
	genItems := make([]gen.Supplier, len(items))
	for i, r := range items {
		genItems[i] = toGenSupplier(r)
	}
	return gen.ListSuppliers200JSONResponse(gen.SupplierList{Items: genItems, NextCursor: nullableString(nextCursor)}), nil
}

// GetSupplier gets a supplier by id. Requires suppliers.manage (manager+).
// 404 for another shop's id or a soft-deleted supplier.
func (h *Handler) GetSupplier(ctx context.Context, req gen.GetSupplierRequestObject) (gen.GetSupplierResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermSuppliersManage); err != nil {
		return nil, err
	}

	row, err := h.svc.q.GetSupplier(ctx, db.GetSupplierParams{ShopID: authCtx.ShopID, ID: req.Id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("supplier")
		}
		return nil, fmt.Errorf("crm: get supplier: %w", err)
	}
	return gen.GetSupplier200JSONResponse(toGenSupplier(row)), nil
}

// CreateSupplier creates a supplier. Requires suppliers.manage (manager+).
// 409 CONFLICT details.field: name for a duplicate active name (unique
// (shop_id, name) where deleted_at is null).
func (h *Handler) CreateSupplier(ctx context.Context, req gen.CreateSupplierRequestObject) (gen.CreateSupplierResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermSuppliersManage); err != nil {
		return nil, err
	}
	body := req.Body

	created, err := h.svc.q.CreateSupplier(ctx, db.CreateSupplierParams{
		ID: newID(), ShopID: authCtx.ShopID, Name: body.Name,
		ContactName: body.ContactName, Phone: body.Phone,
		TelegramUsername: body.TelegramUsername, Note: body.Note,
	})
	if err != nil {
		if apiErr, ok := mapWriteError(err); ok {
			return nil, apiErr
		}
		return nil, fmt.Errorf("crm: create supplier: %w", err)
	}
	return gen.CreateSupplier201JSONResponse(toGenSupplier(created)), nil
}

// UpdateSupplier updates a supplier. Requires suppliers.manage (manager+).
// Explicit `null` for contactName/phone/telegramUsername/note clears the
// field (D-35); absent leaves it unchanged.
func (h *Handler) UpdateSupplier(ctx context.Context, req gen.UpdateSupplierRequestObject) (gen.UpdateSupplierResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermSuppliersManage); err != nil {
		return nil, err
	}
	body := req.Body

	contactName := optionalString(body.ContactName)
	phone := optionalString(body.Phone)
	telegramUsername := optionalString(body.TelegramUsername)
	note := optionalString(body.Note)

	params := db.UpdateSupplierParams{Name: body.Name, ShopID: authCtx.ShopID, ID: req.Id}
	if contactName != nil {
		params.ClearContactName = *contactName == nil
		params.ContactName = *contactName
	}
	if phone != nil {
		params.ClearPhone = *phone == nil
		params.Phone = *phone
	}
	if telegramUsername != nil {
		params.ClearTelegramUsername = *telegramUsername == nil
		params.TelegramUsername = *telegramUsername
	}
	if note != nil {
		params.ClearNote = *note == nil
		params.Note = *note
	}

	updated, err := h.svc.q.UpdateSupplier(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("supplier")
		}
		if apiErr, ok := mapWriteError(err); ok {
			return nil, apiErr
		}
		return nil, fmt.Errorf("crm: update supplier: %w", err)
	}
	return gen.UpdateSupplier200JSONResponse(toGenSupplier(updated)), nil
}

// DeleteSupplier soft-deletes a supplier. Requires suppliers.manage
// (manager+). A purchase referencing this supplier keeps working — the FK
// is to suppliers(id), independent of deleted_at, and no rule in
// docs/04-DATA-MODEL.md blocks deleting a supplier with existing purchases
// (unlike categories/products, O-14).
func (h *Handler) DeleteSupplier(ctx context.Context, req gen.DeleteSupplierRequestObject) (gen.DeleteSupplierResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermSuppliersManage); err != nil {
		return nil, err
	}

	if _, err := h.svc.q.GetSupplier(ctx, db.GetSupplierParams{ShopID: authCtx.ShopID, ID: req.Id}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("supplier")
		}
		return nil, fmt.Errorf("crm: get supplier: %w", err)
	}

	if err := h.svc.q.SoftDeleteSupplier(ctx, db.SoftDeleteSupplierParams{ShopID: authCtx.ShopID, ID: req.Id}); err != nil {
		return nil, fmt.Errorf("crm: soft delete supplier: %w", err)
	}
	return gen.DeleteSupplier204Response{}, nil
}
