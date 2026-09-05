package sales

// This file: GET /sales/{id} (docs/04-DATA-MODEL.md § 4; D-63) plus
// buildSaleResponse, the shared "load one full Sale" helper create.go's
// CreateSaleTx also calls (mirrors stock/purchases.go's
// loadPurchaseWithItems, split out for the same reason: one single-item
// response builder, one caller per write, one caller per read).

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// buildSaleResponse loads id's full Sale: the header (GetSaleForStaff when
// includeCost, else GetSaleForCashier — never the staff query filtered in
// Go, § 04-DATA-MODEL.md rule 8) and its items (the matching
// ListSaleItemsFor* query, in the same locale), 404 "sale" when id does
// not resolve in shopID (a soft-deleted-equivalent miss — sales have no
// soft delete, so this is simply "no row"). q is the caller's own
// *db.Queries — h.svc.q for GetSale's plain read, or qtx still inside
// CreateSaleTx's transaction.
func (h *Handler) buildSaleResponse(ctx context.Context, q *db.Queries, shopID, id uuid.UUID, locale string, includeCost bool) (gen.Sale, error) {
	var header saleHeaderRow
	if includeCost {
		row, err := q.GetSaleForStaff(ctx, db.GetSaleForStaffParams{ShopID: shopID, ID: id})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return gen.Sale{}, apierr.NotFound("sale")
			}
			return gen.Sale{}, fmt.Errorf("sales: get sale for staff: %w", err)
		}
		header = saleHeaderFromStaffRow(row)
	} else {
		row, err := q.GetSaleForCashier(ctx, db.GetSaleForCashierParams{ShopID: shopID, ID: id})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return gen.Sale{}, apierr.NotFound("sale")
			}
			return gen.Sale{}, fmt.Errorf("sales: get sale for cashier: %w", err)
		}
		header = saleHeaderFromCashierRow(row)
	}

	defs, err := q.ListAttributeDefinitions(ctx, db.ListAttributeDefinitionsParams{ShopID: shopID, Locale: locale})
	if err != nil {
		return gen.Sale{}, fmt.Errorf("sales: list attribute definitions: %w", err)
	}

	var items []gen.SaleItem
	if includeCost {
		rows, err := q.ListSaleItemsForStaff(ctx, db.ListSaleItemsForStaffParams{Locale: locale, ShopID: shopID, SaleID: id})
		if err != nil {
			return gen.Sale{}, fmt.Errorf("sales: list sale items for staff: %w", err)
		}
		items = make([]gen.SaleItem, len(rows))
		for i, r := range rows {
			g, err := toGenSaleItem(saleItemFromStaffRow(r), defs)
			if err != nil {
				return gen.Sale{}, err
			}
			items[i] = g
		}
	} else {
		rows, err := q.ListSaleItemsForCashier(ctx, db.ListSaleItemsForCashierParams{Locale: locale, ShopID: shopID, SaleID: id})
		if err != nil {
			return gen.Sale{}, fmt.Errorf("sales: list sale items for cashier: %w", err)
		}
		items = make([]gen.SaleItem, len(rows))
		for i, r := range rows {
			g, err := toGenSaleItem(saleItemFromCashierRow(r), defs)
			if err != nil {
				return gen.Sale{}, err
			}
			items[i] = g
		}
	}

	return toGenSale(header, items)
}

// GetSale gets a sale by id. Any authenticated staff — cashier, manager
// or owner — may view any sale for the whole shop (D-63); 404 for another
// shop's id. unitCost on items is present only for a caller with
// cost.read (ADR-010).
func (h *Handler) GetSale(ctx context.Context, req gen.GetSaleRequestObject) (gen.GetSaleResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}

	locale, err := requestLocaleFor(ctx, h.svc.q, authCtx.ShopID)
	if err != nil {
		return nil, err
	}
	includeCost := auth.Require(ctx, auth.PermCostRead) == nil
	sale, err := h.buildSaleResponse(ctx, h.svc.q, authCtx.ShopID, req.Id, locale, includeCost)
	if err != nil {
		return nil, err
	}
	return gen.GetSale200JSONResponse(sale), nil
}
