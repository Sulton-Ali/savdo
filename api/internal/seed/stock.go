package seed

// This file: opening stock for the demo shop (docs/00-DECISIONS.md D-49).
// Three suppliers, a handful of received purchases that give every demo
// variant opening stock in the main location, and one transfer to the
// storeroom — entirely through crm.Service and stock.Service, exactly as
// catalog.go routes every catalogue write through catalog.Service (never
// a raw INSERT into suppliers/purchases/purchase_items, and never a write
// to stock_movements/stock_levels outside stock.Service.Move, ADR-006).

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/crm"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/money"
	"github.com/Sulton-Ali/savdo/api/internal/stock"
)

// transferQty is the fixed quantity moved to the storeroom for each of
// transferVariantCount variants (task spec: "a few units (e.g. 2)").
const transferQty = 2

// transferVariantCount is how many variants (the first, by sku) get a
// transfer to the storeroom, so the levels grid shows two non-empty
// location columns without every variant needing one.
const transferVariantCount = 10

// invoiceNoFormat is the supplierInvoiceNo pattern every seeded purchase
// uses (task spec: "like INV-2026-0001").
const invoiceNoFormat = "INV-2026-%04d"

// StockReport summarizes what Stock did, for the CLI's summary line and
// for tests. Skipped is true when the shop already had at least one
// purchase — the whole step is then a no-op and every count is 0.
type StockReport struct {
	Skipped              bool
	SuppliersCreated     int
	PurchasesCreated     int
	PurchaseItemsCreated int
	TransfersCreated     int
}

// Stock seeds suppliers, opening-stock purchases (drafted then received)
// and one transfer to the storeroom for shopID (D-49). Idempotent at the
// purchase level: if the shop already has any purchase, this returns
// immediately with Skipped: true and creates nothing — re-running `savdo
// seed` never duplicates a supplier, a purchase or a movement. crmHandler
// and stockHandler must already be wired against pool/q (mirrors Catalog's
// own catalogHandler/mediaSvc parameters); the actor for every write is
// ownerID, the shop's owner user, so the audit_log rows purchase receive
// writes (D-47) have a real actor.
func Stock(ctx context.Context, pool *pgxpool.Pool, q *db.Queries, crmHandler *crm.Handler, stockHandler *stock.Handler, shopID, ownerID uuid.UUID) (StockReport, error) {
	anyPurchase, err := q.ListPurchases(ctx, db.ListPurchasesParams{ShopID: shopID, Limit: 1})
	if err != nil {
		return StockReport{}, fmt.Errorf("seed stock: list purchases: %w", err)
	}
	if len(anyPurchase) > 0 {
		return StockReport{Skipped: true}, nil
	}

	authCtx := catalogAuthContext(ctx, shopID, ownerID)

	mainLocationID, err := locationIDByName(ctx, q, shopID, LocationStoreName)
	if err != nil {
		return StockReport{}, err
	}
	storeroomLocationID, err := locationIDByName(ctx, q, shopID, LocationWarehouseName)
	if err != nil {
		return StockReport{}, err
	}

	supplierIDs, suppliersCreated, err := seedSuppliers(authCtx, q, crmHandler, shopID)
	if err != nil {
		return StockReport{}, fmt.Errorf("seed stock: suppliers: %w", err)
	}

	products, err := productRowsBySlug(ctx, q, shopID)
	if err != nil {
		return StockReport{}, fmt.Errorf("seed stock: products: %w", err)
	}

	purchasesCreated, itemsCreated, transferable, err := seedPurchases(authCtx, pool, q, stockHandler, shopID, mainLocationID, supplierIDs, products)
	if err != nil {
		return StockReport{}, fmt.Errorf("seed stock: purchases: %w", err)
	}

	transfersCreated, err := seedTransfer(authCtx, stockHandler, mainLocationID, storeroomLocationID, transferable)
	if err != nil {
		return StockReport{}, fmt.Errorf("seed stock: transfer: %w", err)
	}

	return StockReport{
		SuppliersCreated:     suppliersCreated,
		PurchasesCreated:     purchasesCreated,
		PurchaseItemsCreated: itemsCreated,
		TransfersCreated:     transfersCreated,
	}, nil
}

// locationIDByName finds shopID's location named name (Seed's own
// upsertLocations already creates both LocationStoreName and
// LocationWarehouseName, so this only errors if Stock is somehow called
// before Seed).
func locationIDByName(ctx context.Context, q *db.Queries, shopID uuid.UUID, name string) (uuid.UUID, error) {
	locations, err := q.ListLocations(ctx, db.ListLocationsParams{ShopID: shopID, Limit: 1000})
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("seed stock: list locations: %w", err)
	}
	for _, l := range locations {
		if l.Name == name {
			return l.ID, nil
		}
	}
	return uuid.UUID{}, fmt.Errorf("seed stock: location %q not found (run Seed first)", name)
}

// seedSuppliers creates whichever of supplierSpecs don't already exist for
// shopID (matched by name, the same skip-if-exists contract every other
// seed step uses), through crmHandler.CreateSupplier. Returns every
// supplier's id in supplierSpecs order, for seedPurchases to cycle
// through.
func seedSuppliers(ctx context.Context, q *db.Queries, h *crm.Handler, shopID uuid.UUID) ([]uuid.UUID, int, error) {
	existing, err := q.ListSuppliers(ctx, db.ListSuppliersParams{ShopID: shopID, Limit: 1000})
	if err != nil {
		return nil, 0, fmt.Errorf("list suppliers: %w", err)
	}
	byName := make(map[string]uuid.UUID, len(existing))
	for _, s := range existing {
		byName[s.Name] = s.ID
	}

	ids := make([]uuid.UUID, len(supplierSpecs))
	created := 0
	for i, spec := range supplierSpecs {
		if id, ok := byName[spec.name]; ok {
			ids[i] = id
			continue
		}

		contactName, phone, note := spec.contactName, spec.phone, spec.note
		body := gen.SupplierCreate{Name: spec.name, ContactName: &contactName, Phone: &phone, Note: &note}
		resp, err := h.CreateSupplier(ctx, gen.CreateSupplierRequestObject{Body: &body})
		if err != nil {
			return nil, 0, fmt.Errorf("create supplier %q: %w", spec.name, err)
		}
		created201, ok := resp.(gen.CreateSupplier201JSONResponse)
		if !ok {
			return nil, 0, fmt.Errorf("create supplier %q: unexpected response type %T", spec.name, resp)
		}
		ids[i] = created201.Id
		created++
	}
	return ids, created, nil
}

// productRowsBySlug maps shopID's current products (active and inactive)
// to their full row by slug, so seedPurchases can read each one's
// cost_price without a second query per product.
func productRowsBySlug(ctx context.Context, q *db.Queries, shopID uuid.UUID) (map[string]db.ListProductsForStaffRow, error) {
	rows, err := q.ListProductsForStaff(ctx, db.ListProductsForStaffParams{
		Locale: seedLocale, ShopID: shopID, IncludeInactive: true, Limit: 1000,
	})
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}
	bySlug := make(map[string]db.ListProductsForStaffRow, len(rows))
	for _, r := range rows {
		bySlug[r.Slug] = r
	}
	return bySlug, nil
}

// variantRef is one variant a purchase item was written for: its id and
// sku, for seedTransfer's "first 10 by sku" selection.
type variantRef struct {
	id  uuid.UUID
	sku string
}

// categoryOrder returns productSpecs' distinct categorySlug values, each
// once, in first-seen order — the demo catalogue's 6 leaf/top-level
// categories (men-shirts, men-trousers, men-jackets, women-dresses,
// women-blouses, kids), one purchase per category (task spec: "3-6
// purchases ... one purchase per supplier per category is fine").
func categoryOrder() []string {
	seen := make(map[string]bool)
	var order []string
	for _, spec := range productSpecs {
		if !seen[spec.categorySlug] {
			seen[spec.categorySlug] = true
			order = append(order, spec.categorySlug)
		}
	}
	return order
}

// stableQty derives a deterministic opening-stock quantity in [3, 12] from
// sku (task spec: "deterministic — derive from a stable hash of the SKU,
// no randomness"), via a non-cryptographic hash — sku uniqueness, not
// unpredictability, is all that's needed here.
func stableQty(sku string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(sku))
	return int(h.Sum32()%10) + 3
}

// effectiveCost is a variant's current cost: its own cost_override when
// set, else its product's cost_price (D-42's own "current effective
// cost" — matches catalog's own effective-price precedence). Receiving
// the purchase this becomes the item's unitCost then writes it right back
// as cost_override (when shops.update_cost_on_purchase is on, the
// default), so the catalogue's numbers are unchanged by this seed step.
func effectiveCost(productCostPrice, variantCostOverride pgtype.Numeric) (decimal.Decimal, error) {
	if variantCostOverride.Valid {
		return money.FromNumeric(variantCostOverride)
	}
	if !productCostPrice.Valid {
		return decimal.Decimal{}, fmt.Errorf("no cost price set")
	}
	return money.FromNumeric(productCostPrice)
}

// categoryPurchaseItems builds one purchase's items from every product in
// specs (all belonging to one category), reading each product's variants
// fresh from the database (so a variant's own sku/cost_override, not the
// spec's static data, drives qty/unitCost) and collecting a variantRef per
// item for seedTransfer's later use.
func categoryPurchaseItems(ctx context.Context, q *db.Queries, shopID uuid.UUID, products map[string]db.ListProductsForStaffRow, specs []productSpec) ([]gen.PurchaseItemCreate, []variantRef, error) {
	var items []gen.PurchaseItemCreate
	var refs []variantRef
	for _, spec := range specs {
		product, ok := products[spec.slug]
		if !ok {
			return nil, nil, fmt.Errorf("product %q was not seeded", spec.slug)
		}
		variants, err := q.ListVariantsForStaff(ctx, db.ListVariantsForStaffParams{ShopID: shopID, ProductID: product.ID})
		if err != nil {
			return nil, nil, fmt.Errorf("list variants for %q: %w", spec.slug, err)
		}
		for _, v := range variants {
			if v.Sku == nil {
				return nil, nil, fmt.Errorf("variant %s of %q has no sku", v.ID, spec.slug)
			}
			cost, err := effectiveCost(product.CostPrice, v.CostOverride)
			if err != nil {
				return nil, nil, fmt.Errorf("variant %s of %q: %w", v.ID, spec.slug, err)
			}
			qty := stableQty(*v.Sku)

			items = append(items, gen.PurchaseItemCreate{
				VariantId: v.ID,
				Qty:       fmt.Sprintf("%d", qty),
				UnitCost:  money.String(cost),
			})
			refs = append(refs, variantRef{id: v.ID, sku: *v.Sku})
		}
	}
	return items, refs, nil
}

// seedPurchases creates and receives one draft purchase per
// categoryOrder() entry, cycling through supplierIDs, then receives each
// one inside its own transaction via stock.Handler.ReceivePurchaseTx
// (never httpx.Idempotent — this is a CLI/seed call, not an HTTP
// request). Returns the number of purchases and total items created, plus
// every item's variantRef (for seedTransfer's "first 10 by sku" pick).
func seedPurchases(ctx context.Context, pool *pgxpool.Pool, q *db.Queries, h *stock.Handler, shopID, mainLocationID uuid.UUID, supplierIDs []uuid.UUID, products map[string]db.ListProductsForStaffRow) (purchasesCreated, itemsCreated int, allRefs []variantRef, err error) {
	categories := categoryOrder()

	bySlug := make(map[string][]productSpec, len(categories))
	for _, spec := range productSpecs {
		bySlug[spec.categorySlug] = append(bySlug[spec.categorySlug], spec)
	}

	for i, categorySlug := range categories {
		specs := bySlug[categorySlug]
		items, refs, err := categoryPurchaseItems(ctx, q, shopID, products, specs)
		if err != nil {
			return 0, 0, nil, fmt.Errorf("category %q: %w", categorySlug, err)
		}
		if len(items) == 0 {
			continue
		}

		supplierID := supplierIDs[i%len(supplierIDs)]
		invoiceNo := fmt.Sprintf(invoiceNoFormat, i+1)
		body := gen.PurchaseCreate{
			SupplierId:        supplierID,
			LocationId:        mainLocationID,
			SupplierInvoiceNo: &invoiceNo,
			Items:             items,
		}
		created, err := h.CreatePurchase(ctx, gen.CreatePurchaseRequestObject{Body: &body})
		if err != nil {
			return 0, 0, nil, fmt.Errorf("category %q: create purchase: %w", categorySlug, err)
		}
		created201, ok := created.(gen.CreatePurchase201JSONResponse)
		if !ok {
			return 0, 0, nil, fmt.Errorf("category %q: create purchase: unexpected response type %T", categorySlug, created)
		}

		if err := receivePurchase(ctx, pool, q, h, created201.Id); err != nil {
			return 0, 0, nil, fmt.Errorf("category %q: receive purchase: %w", categorySlug, err)
		}

		purchasesCreated++
		itemsCreated += len(items)
		allRefs = append(allRefs, refs...)
	}
	return purchasesCreated, itemsCreated, allRefs, nil
}

// receivePurchase runs stock.Handler.ReceivePurchaseTx inside its own
// transaction — ReceivePurchaseTx needs a caller-managed qtx (it is not
// wrapped by httpx.Idempotent here, unlike the HTTP route, since this is a
// direct seed call with no Idempotency-Key involved).
func receivePurchase(ctx context.Context, pool *pgxpool.Pool, q *db.Queries, h *stock.Handler, purchaseID uuid.UUID) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := q.WithTx(tx)

	if _, err := h.ReceivePurchaseTx(ctx, qtx, purchaseID); err != nil {
		return fmt.Errorf("receive: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// seedTransfer moves transferQty of the first transferVariantCount
// variants, sorted by sku, from mainLocationID to storeroomLocationID —
// one stock.Handler.CreateStockTransfer call per variant, so the levels
// grid shows both locations for a handful of variants (task spec).
func seedTransfer(ctx context.Context, h *stock.Handler, mainLocationID, storeroomLocationID uuid.UUID, refs []variantRef) (int, error) {
	sorted := make([]variantRef, len(refs))
	copy(sorted, refs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].sku < sorted[j].sku })

	n := transferVariantCount
	if n > len(sorted) {
		n = len(sorted)
	}

	for i := 0; i < n; i++ {
		body := gen.StockTransferCreate{
			VariantId:      sorted[i].id,
			FromLocationId: mainLocationID,
			ToLocationId:   storeroomLocationID,
			Qty:            fmt.Sprintf("%d", transferQty),
		}
		if _, err := h.CreateStockTransfer(ctx, gen.CreateStockTransferRequestObject{Body: &body}); err != nil {
			return 0, fmt.Errorf("transfer variant %s: %w", sorted[i].id, err)
		}
	}
	return n, nil
}
