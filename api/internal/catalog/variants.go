package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oapi-codegen/nullable"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/money"
)

// maxAttributeValueLength bounds one attribute value (e.g. a size or
// colour name) per the task spec.
const maxAttributeValueLength = 64

// implicitAttributes is the JSON every product's auto-created "no
// options" variant stores (docs/04-DATA-MODEL.md § 2: "a product without
// options gets one variant with attributes = '{}'").
const implicitAttributes = "{}"

// toGenVariant builds a gen.Variant from a variant's normalized fields.
// costOverride is nil when the caller lacks cost.read (the field is left
// entirely unset, so `omitempty` drops it — ADR-010: "absent, not null");
// non-nil (even pointing at an invalid/NULL Numeric) when the caller may
// see it.
func toGenVariant(id uuid.UUID, sku, barcode *string, attrs json.RawMessage, priceOverride pgtype.Numeric, costOverride *pgtype.Numeric, isActive bool) (gen.Variant, error) {
	var attrMap gen.AttributeValues
	if err := json.Unmarshal(attrs, &attrMap); err != nil {
		return gen.Variant{}, fmt.Errorf("catalog: unmarshal variant attributes: %w", err)
	}

	g := gen.Variant{
		Id:         id,
		Attributes: attrMap,
		Sku:        nullableString(sku),
		Barcode:    nullableString(barcode),
		IsActive:   isActive,
	}

	if priceOverride.Valid {
		d, err := money.FromNumeric(priceOverride)
		if err != nil {
			return gen.Variant{}, err
		}
		g.PriceOverride = nullable.NewNullableWithValue(money.String(d))
	} else {
		g.PriceOverride = nullable.NewNullNullable[string]()
	}

	if costOverride != nil {
		if costOverride.Valid {
			d, err := money.FromNumeric(*costOverride)
			if err != nil {
				return gen.Variant{}, err
			}
			g.CostOverride = nullable.NewNullableWithValue(money.String(d))
		} else {
			g.CostOverride = nullable.NewNullNullable[string]()
		}
	}

	return g, nil
}

// genVariantFromStaffRow builds a gen.Variant from the ForStaff row shape
// (ProductVariant — GetVariantForStaff/ListVariantsForStaff/CreateVariant/
// UpdateVariant all return this), including costOverride only when
// includeCost.
func genVariantFromStaffRow(v db.ProductVariant, includeCost bool) (gen.Variant, error) {
	var cost *pgtype.Numeric
	if includeCost {
		c := v.CostOverride
		cost = &c
	}
	return toGenVariant(v.ID, v.Sku, v.Barcode, v.Attributes, v.PriceOverride, cost, v.IsActive)
}

// validateAttributes checks a variant's attribute map against the shop's
// attribute definitions: every key must be a known code, every value
// non-empty and at most maxAttributeValueLength characters. Returns the
// canonical JSON to store — encoding/json.Marshal of a Go map always
// serializes keys in sorted order, which is exactly the canonicalization
// the (product_id, attributes) unique index needs, so no separate sort
// step is required.
func (s *Service) validateAttributes(ctx context.Context, shopID uuid.UUID, attrs gen.AttributeValues) (json.RawMessage, bool, error) {
	codes, err := s.attributeDefinitionCodes(ctx, shopID)
	if err != nil {
		return nil, false, err
	}
	for code, value := range attrs {
		if !codes[code] {
			return nil, false, nil
		}
		if value == "" || utf8.RuneCountInString(value) > maxAttributeValueLength {
			return nil, false, nil
		}
	}
	canonical, err := json.Marshal(map[string]string(attrs))
	if err != nil {
		return nil, false, err
	}
	return canonical, true, nil
}

// variantsFor lists productID's variants as gen.Variant, for embedding
// into a full Product response (buildFullProduct in products.go).
func (s *Service) variantsFor(ctx context.Context, shopID, productID uuid.UUID, includeCost bool) ([]gen.Variant, error) {
	if includeCost {
		rows, err := s.q.ListVariantsForStaff(ctx, db.ListVariantsForStaffParams{ShopID: shopID, ProductID: productID})
		if err != nil {
			return nil, err
		}
		out := make([]gen.Variant, len(rows))
		for i, r := range rows {
			g, err := genVariantFromStaffRow(r, true)
			if err != nil {
				return nil, err
			}
			out[i] = g
		}
		return out, nil
	}
	rows, err := s.q.ListVariantsForCashier(ctx, db.ListVariantsForCashierParams{ShopID: shopID, ProductID: productID})
	if err != nil {
		return nil, err
	}
	out := make([]gen.Variant, len(rows))
	for i, r := range rows {
		g, err := toGenVariant(r.ID, r.Sku, r.Barcode, r.Attributes, r.PriceOverride, nil, r.IsActive)
		if err != nil {
			return nil, err
		}
		out[i] = g
	}
	return out, nil
}

// ListVariants lists a product's variants. Any authenticated role; not
// cursor-paginated (a product has at most a handful of variants).
// `costOverride` is present only for owner/manager.
func (h *Handler) ListVariants(ctx context.Context, req gen.ListVariantsRequestObject) (gen.ListVariantsResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	if _, err := h.svc.q.GetProductForCashier(ctx, db.GetProductForCashierParams{Locale: h.svc.defaultLocale, ShopID: authCtx.ShopID, ID: req.Id}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("product")
		}
		return nil, fmt.Errorf("catalog: get product: %w", err)
	}

	includeCost := auth.Require(ctx, auth.PermCostRead) == nil
	var items []gen.Variant
	if includeCost {
		rows, err := h.svc.q.ListVariantsForStaff(ctx, db.ListVariantsForStaffParams{ShopID: authCtx.ShopID, ProductID: req.Id})
		if err != nil {
			return nil, fmt.Errorf("catalog: list variants for staff: %w", err)
		}
		for _, r := range rows {
			g, err := genVariantFromStaffRow(r, true)
			if err != nil {
				return nil, fmt.Errorf("catalog: %w", err)
			}
			items = append(items, g)
		}
	} else {
		rows, err := h.svc.q.ListVariantsForCashier(ctx, db.ListVariantsForCashierParams{ShopID: authCtx.ShopID, ProductID: req.Id})
		if err != nil {
			return nil, fmt.Errorf("catalog: list variants for cashier: %w", err)
		}
		for _, r := range rows {
			g, err := toGenVariant(r.ID, r.Sku, r.Barcode, r.Attributes, r.PriceOverride, nil, r.IsActive)
			if err != nil {
				return nil, fmt.Errorf("catalog: %w", err)
			}
			items = append(items, g)
		}
	}
	if items == nil {
		items = []gen.Variant{}
	}
	return gen.ListVariants200JSONResponse(gen.VariantList{Items: items}), nil
}

// CreateVariant adds a variant to a product. Requires catalog.write
// (manager+). Creating a real variant on a product that only has the
// implicit `{}` variant replaces it (soft-deletes it) and sets
// has_variants; attributes keys must be codes of this shop's attribute
// definitions.
func (h *Handler) CreateVariant(ctx context.Context, req gen.CreateVariantRequestObject) (gen.CreateVariantResponseObject, error) {
	if _, ok := auth.FromContext(ctx); !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermCatalogWrite); err != nil {
		return nil, err
	}
	authCtx, _ := auth.FromContext(ctx)

	if _, err := h.svc.q.GetProductForStaff(ctx, db.GetProductForStaffParams{Locale: h.svc.defaultLocale, ShopID: authCtx.ShopID, ID: req.Id}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("product")
		}
		return nil, fmt.Errorf("catalog: get product: %w", err)
	}

	body := req.Body
	fields := map[string]string{}

	canonical, ok, err := h.svc.validateAttributes(ctx, authCtx.ShopID, body.Attributes)
	if err != nil {
		return nil, fmt.Errorf("catalog: validate attributes: %w", err)
	}
	if !ok {
		fields["attributes"] = "invalid"
	}

	var priceOverride, costOverride pgtype.Numeric
	if body.PriceOverride != nil {
		d, apiErr := money.ParseAmount(*body.PriceOverride)
		if apiErr != nil {
			fields["priceOverride"] = "invalid"
		} else {
			priceOverride = money.ToNumeric(d)
		}
	}
	if body.CostOverride != nil {
		d, apiErr := money.ParseAmount(*body.CostOverride)
		if apiErr != nil {
			fields["costOverride"] = "invalid"
		} else {
			costOverride = money.ToNumeric(d)
		}
	}

	if len(fields) > 0 {
		return nil, apierr.Validation(fields)
	}

	isActive := true
	if body.IsActive != nil {
		isActive = *body.IsActive
	}

	tx, err := h.svc.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("catalog: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := h.svc.q.WithTx(tx)

	existing, err := qtx.ListVariantsForStaff(ctx, db.ListVariantsForStaffParams{ShopID: authCtx.ShopID, ProductID: req.Id})
	if err != nil {
		return nil, fmt.Errorf("catalog: list existing variants: %w", err)
	}
	replacesImplicit := len(existing) == 1 && string(existing[0].Attributes) == implicitAttributes

	created, err := qtx.CreateVariant(ctx, db.CreateVariantParams{
		ID: newID(), ShopID: authCtx.ShopID, ProductID: req.Id, Sku: body.Sku, Barcode: body.Barcode,
		Attributes: canonical, PriceOverride: priceOverride, CostOverride: costOverride, IsActive: isActive,
	})
	if err != nil {
		if apiErr, ok := mapWriteError(err); ok {
			return nil, apiErr
		}
		return nil, fmt.Errorf("catalog: create variant: %w", err)
	}

	if replacesImplicit {
		if err := qtx.SoftDeleteVariant(ctx, db.SoftDeleteVariantParams{ShopID: authCtx.ShopID, ID: existing[0].ID}); err != nil {
			return nil, fmt.Errorf("catalog: soft delete implicit variant: %w", err)
		}
		if err := qtx.SetProductHasVariants(ctx, db.SetProductHasVariantsParams{ShopID: authCtx.ShopID, ID: req.Id, HasVariants: true}); err != nil {
			return nil, fmt.Errorf("catalog: set has_variants: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("catalog: commit create variant: %w", err)
	}

	resp, err := genVariantFromStaffRow(created, true)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	return gen.CreateVariant201JSONResponse(resp), nil
}

// UpdateVariant updates a variant. Requires catalog.write (manager+).
// `attributes`, when present, is validated exactly like CreateVariant
// (keys must be this shop's attribute definition codes, values non-empty
// and <= maxAttributeValueLength, canonicalized via encoding/json's
// sorted-map-key marshalling) and rewritten in place via
// db.UpdateVariantAttributes — the variant keeps its id, so any Phase 3+
// stock/sales reference to it survives; a duplicate combination is 409
// CONFLICT details.field: attributes, same as CreateVariant.
func (h *Handler) UpdateVariant(ctx context.Context, req gen.UpdateVariantRequestObject) (gen.UpdateVariantResponseObject, error) {
	if _, ok := auth.FromContext(ctx); !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermCatalogWrite); err != nil {
		return nil, err
	}
	authCtx, _ := auth.FromContext(ctx)

	current, err := h.svc.q.GetVariantForStaff(ctx, db.GetVariantForStaffParams{ShopID: authCtx.ShopID, ID: req.Id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("variant")
		}
		return nil, fmt.Errorf("catalog: get variant: %w", err)
	}

	body := req.Body
	fields := map[string]string{}

	var canonicalAttrs json.RawMessage
	if body.Attributes != nil {
		canonical, ok, err := h.svc.validateAttributes(ctx, authCtx.ShopID, *body.Attributes)
		if err != nil {
			return nil, fmt.Errorf("catalog: validate attributes: %w", err)
		}
		if !ok {
			fields["attributes"] = "invalid"
		} else {
			canonicalAttrs = canonical
		}
	}

	skuP := optionalString(body.Sku)
	barcodeP := optionalString(body.Barcode)

	var priceOverride pgtype.Numeric
	clearPrice := false
	if pp := optionalString(body.PriceOverride); pp != nil {
		if *pp == nil {
			clearPrice = true
		} else {
			d, apiErr := money.ParseAmount(**pp)
			if apiErr != nil {
				fields["priceOverride"] = "invalid"
			} else {
				priceOverride = money.ToNumeric(d)
			}
		}
	}

	var costOverride pgtype.Numeric
	clearCost := false
	if cp := optionalString(body.CostOverride); cp != nil {
		if *cp == nil {
			clearCost = true
		} else {
			d, apiErr := money.ParseAmount(**cp)
			if apiErr != nil {
				fields["costOverride"] = "invalid"
			} else {
				costOverride = money.ToNumeric(d)
			}
		}
	}

	if len(fields) > 0 {
		return nil, apierr.Validation(fields)
	}

	params := db.UpdateVariantParams{
		ShopID: authCtx.ShopID, ID: current.ID, IsActive: body.IsActive,
		ClearPriceOverride: clearPrice, PriceOverride: priceOverride,
		ClearCostOverride: clearCost, CostOverride: costOverride,
	}
	if skuP != nil {
		if *skuP == nil {
			params.ClearSku = true
		} else {
			params.Sku = *skuP
		}
	}
	if barcodeP != nil {
		if *barcodeP == nil {
			params.ClearBarcode = true
		} else {
			params.Barcode = *barcodeP
		}
	}

	tx, err := h.svc.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("catalog: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := h.svc.q.WithTx(tx)

	updated, err := qtx.UpdateVariant(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("variant")
		}
		if apiErr, ok := mapWriteError(err); ok {
			return nil, apiErr
		}
		return nil, fmt.Errorf("catalog: update variant: %w", err)
	}

	if canonicalAttrs != nil {
		updated, err = qtx.UpdateVariantAttributes(ctx, db.UpdateVariantAttributesParams{
			ShopID: authCtx.ShopID, ID: current.ID, Attributes: canonicalAttrs,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, apierr.NotFound("variant")
			}
			if apiErr, ok := mapWriteError(err); ok {
				return nil, apiErr
			}
			return nil, fmt.Errorf("catalog: update variant attributes: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("catalog: commit update variant: %w", err)
	}

	resp, err := genVariantFromStaffRow(updated, true)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	return gen.UpdateVariant200JSONResponse(resp), nil
}

// DeleteVariant soft-deletes a variant. Requires catalog.write
// (manager+). 400 fields.id: invalid when this is the product's only
// non-deleted variant, active or not (docs/04-DATA-MODEL.md § 2: every
// product keeps at least one) — guarding only on is_active would let the
// last variant be removed as long as it happened to be inactive, which
// still violates the invariant. The count-then-delete runs inside one
// transaction after LockShop(shopID), the same per-tenant serialization
// point CreateLocation/UpdateLocation use, so two concurrent deletes of a
// product's last two variants cannot both read "2 remain" and both
// proceed. has_variants is recomputed afterwards: false only when exactly
// one non-deleted variant remains and it is the implicit `{}` one.
func (h *Handler) DeleteVariant(ctx context.Context, req gen.DeleteVariantRequestObject) (gen.DeleteVariantResponseObject, error) {
	if _, ok := auth.FromContext(ctx); !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermCatalogWrite); err != nil {
		return nil, err
	}
	authCtx, _ := auth.FromContext(ctx)

	variant, err := h.svc.q.GetVariantForStaff(ctx, db.GetVariantForStaffParams{ShopID: authCtx.ShopID, ID: req.Id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("variant")
		}
		return nil, fmt.Errorf("catalog: get variant: %w", err)
	}

	tx, err := h.svc.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("catalog: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := h.svc.q.WithTx(tx)

	if _, err := qtx.LockShop(ctx, authCtx.ShopID); err != nil {
		return nil, fmt.Errorf("catalog: lock shop: %w", err)
	}

	remaining, err := qtx.ListVariantsForStaff(ctx, db.ListVariantsForStaffParams{ShopID: authCtx.ShopID, ProductID: variant.ProductID})
	if err != nil {
		return nil, fmt.Errorf("catalog: list variants: %w", err)
	}
	if len(remaining) <= 1 {
		return nil, apierr.Validation(map[string]string{"id": "invalid"})
	}

	if err := qtx.SoftDeleteVariant(ctx, db.SoftDeleteVariantParams{ShopID: authCtx.ShopID, ID: req.Id}); err != nil {
		return nil, fmt.Errorf("catalog: soft delete variant: %w", err)
	}

	afterCount := 0
	var soleRemaining db.ProductVariant
	for _, v := range remaining {
		if v.ID == req.Id {
			continue
		}
		afterCount++
		soleRemaining = v
	}
	hasVariants := afterCount != 1 || string(soleRemaining.Attributes) != implicitAttributes
	if err := qtx.SetProductHasVariants(ctx, db.SetProductHasVariantsParams{ShopID: authCtx.ShopID, ID: variant.ProductID, HasVariants: hasVariants}); err != nil {
		return nil, fmt.Errorf("catalog: set has_variants: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("catalog: commit delete variant: %w", err)
	}
	return gen.DeleteVariant204Response{}, nil
}
