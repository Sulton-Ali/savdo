package sales_test

// Shared fixtures for this package's tests: a fresh testcontainers
// Postgres per test (newTestQueries, mirrors stock_test.go's own), seed
// helpers for shop/user/unit/product/variant/location/customer, ctxAs for
// building an authenticated context without a real HTTP request, and
// createSale — the direct-transaction shape httpx.Idempotent gives
// CreateSaleTx in production (this package's own tests call the handler
// method directly, not the httpx wrapper — that wrapper's own
// idempotency-replay tests live in internal/httpx/sales_test.go, mirrors
// stock_test.go/purchases_test.go's own split).

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	oapitypes "github.com/oapi-codegen/runtime/types"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
	"github.com/Sulton-Ali/savdo/api/internal/money"
	"github.com/Sulton-Ali/savdo/api/internal/sales"
	"github.com/Sulton-Ali/savdo/api/internal/stock"
)

func newTestQueries(t *testing.T) (*pgxpool.Pool, *db.Queries) {
	t.Helper()
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	return pool, db.New(pool)
}

func numeric(t *testing.T, s string) pgtype.Numeric {
	t.Helper()
	var n pgtype.Numeric
	if err := n.Scan(s); err != nil {
		t.Fatalf("numeric(%q): %v", s, err)
	}
	return n
}

func d(t *testing.T, s string) decimal.Decimal {
	t.Helper()
	dec, err := decimal.NewFromString(s)
	if err != nil {
		t.Fatalf("decimal(%q): %v", s, err)
	}
	return dec
}

func seedShop(ctx context.Context, t *testing.T, q *db.Queries, slug string) db.Shop {
	t.Helper()
	s, err := q.CreateShop(ctx, db.CreateShopParams{ID: uuid.New(), Slug: slug, Name: "Sales Shop " + slug})
	if err != nil {
		t.Fatalf("seedShop(%q): %v", slug, err)
	}
	return s
}

func seedUser(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, username string, role db.UserRole) db.User {
	t.Helper()
	hash, err := auth.Hash("correct-horse-battery")
	if err != nil {
		t.Fatalf("seedUser: Hash: %v", err)
	}
	user, err := q.CreateUser(ctx, db.CreateUserParams{
		ID: uuid.New(), ShopID: shopID, Username: username, PasswordHash: hash,
		FullName: "Sales Test User " + username, Role: role, Locale: "uz",
	})
	if err != nil {
		t.Fatalf("seedUser(%q): %v", username, err)
	}
	return user
}

func seedUnit(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, code string) db.Unit {
	t.Helper()
	u, err := q.UpsertUnit(ctx, db.UpsertUnitParams{ID: uuid.New(), ShopID: shopID, Code: code, Precision: 0})
	if err != nil {
		t.Fatalf("seedUnit(%q): %v", code, err)
	}
	return u
}

func seedLocation(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, name string) db.Location {
	t.Helper()
	loc, err := q.CreateLocation(ctx, db.CreateLocationParams{
		ID: uuid.New(), ShopID: shopID, Name: name, Kind: db.LocationKindStore, IsActive: true,
	})
	if err != nil {
		t.Fatalf("seedLocation(%q): %v", name, err)
	}
	return loc
}

func seedCustomer(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, name string) db.Customer {
	t.Helper()
	c, err := q.CreateCustomer(ctx, db.CreateCustomerParams{ID: uuid.New(), ShopID: shopID, FullName: name})
	if err != nil {
		t.Fatalf("seedCustomer(%q): %v", name, err)
	}
	return c
}

// productOpts configures seedProduct's optional cost/promo fields — a
// plain struct rather than functional options, since every test that
// needs any of them needs at most two or three at once.
type productOpts struct {
	costPrice          *string
	promoPrice         *string
	promoFrom, promoTo *time.Time
	isActive           *bool // nil means true, the common case
}

func seedProduct(ctx context.Context, t *testing.T, q *db.Queries, shopID, unitID uuid.UUID, slug, basePrice string, opts productOpts) db.Product {
	t.Helper()
	isActive := true
	if opts.isActive != nil {
		isActive = *opts.isActive
	}
	params := db.CreateProductParams{
		ID: uuid.New(), ShopID: shopID, UnitID: unitID, Slug: slug,
		BasePrice: numeric(t, basePrice), IsActive: isActive,
	}
	if opts.costPrice != nil {
		params.CostPrice = numeric(t, *opts.costPrice)
	}
	if opts.promoPrice != nil {
		params.PromoPrice = numeric(t, *opts.promoPrice)
	}
	params.PromoFrom = opts.promoFrom
	params.PromoTo = opts.promoTo
	p, err := q.CreateProduct(ctx, params)
	if err != nil {
		t.Fatalf("seedProduct(%q): %v", slug, err)
	}
	return p
}

func seedVariant(ctx context.Context, t *testing.T, q *db.Queries, shopID, productID uuid.UUID) db.ProductVariant {
	t.Helper()
	return seedVariantWithID(ctx, t, q, shopID, productID, uuid.New())
}

// seedVariantWithID is seedVariant with a caller-chosen id — for a test
// that needs to control the (variant_id) ascending sort sortedSaleLines
// (create.go) uses, since a plain uuid.New() (v4, random) gives no
// relationship at all between creation order and sort order.
func seedVariantWithID(ctx context.Context, t *testing.T, q *db.Queries, shopID, productID, id uuid.UUID) db.ProductVariant {
	t.Helper()
	v, err := q.CreateVariant(ctx, db.CreateVariantParams{
		ID: id, ShopID: shopID, ProductID: productID, Attributes: json.RawMessage(`{}`), IsActive: true,
	})
	if err != nil {
		t.Fatalf("seedVariantWithID: %v", err)
	}
	return v
}

// seedInactiveVariant is seedVariant with is_active = false — a variant
// that is not for sale even though it resolves (deleted_at IS NULL).
func seedInactiveVariant(ctx context.Context, t *testing.T, q *db.Queries, shopID, productID uuid.UUID) db.ProductVariant {
	t.Helper()
	v, err := q.CreateVariant(ctx, db.CreateVariantParams{
		ID: uuid.New(), ShopID: shopID, ProductID: productID, Attributes: json.RawMessage(`{}`), IsActive: false,
	})
	if err != nil {
		t.Fatalf("seedInactiveVariant: %v", err)
	}
	return v
}

// seedVariantWithCostOverride is seedVariant plus a cost_override —
// effectiveUnitCost's own precedence (variant cost_override beats the
// product's cost_price).
func seedVariantWithCostOverride(ctx context.Context, t *testing.T, q *db.Queries, shopID, productID uuid.UUID, costOverride string) db.ProductVariant {
	t.Helper()
	v, err := q.CreateVariant(ctx, db.CreateVariantParams{
		ID: uuid.New(), ShopID: shopID, ProductID: productID, Attributes: json.RawMessage(`{}`),
		CostOverride: numeric(t, costOverride), IsActive: true,
	})
	if err != nil {
		t.Fatalf("seedVariantWithCostOverride: %v", err)
	}
	return v
}

// ctxAs builds a context carrying the auth.Context a real request would
// have after auth.Service.Middleware ran, for a real seeded user —
// mirrors stock_test.go's own ctxAs.
func ctxAs(shopID uuid.UUID, user db.User) context.Context {
	return auth.WithContext(context.Background(), auth.Context{
		ShopID: shopID, UserID: user.ID, Role: user.Role, SessionID: uuid.New(), Client: db.SessionClientWeb,
	})
}

// stockIn gives qty units of stock at locationID for variantID, via a
// purchase_in movement — every CreateSaleTx test needs existing stock,
// else the very first line would 409 STOCK_INSUFFICIENT (allow_negative_
// stock defaults off).
func stockIn(ctx context.Context, t *testing.T, pool *pgxpool.Pool, q *db.Queries, shopID, variantID, locationID uuid.UUID, qty string) {
	t.Helper()
	svc := stock.NewService(pool, q)
	if _, err := svc.MoveInTx(ctx, stock.MoveParams{
		ShopID: shopID, VariantID: variantID, LocationID: locationID,
		Kind: db.StockMovementKindPurchaseIn, Qty: d(t, qty),
	}); err != nil {
		t.Fatalf("stockIn: %v", err)
	}
}

// createSale runs CreateSaleTx on its own transaction, committing on
// success and rolling back on error — the shape httpx.Idempotent gives it
// in production (see this file's own doc comment).
func createSale(ctx context.Context, t *testing.T, h *sales.Handler, pool *pgxpool.Pool, q *db.Queries, body *gen.SaleCreate) (gen.Sale, error) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("createSale: begin: %v", err)
	}
	qtx := q.WithTx(tx)
	resp, err := h.CreateSaleTx(ctx, qtx, body)
	if err != nil {
		_ = tx.Rollback(ctx)
		return gen.Sale{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("createSale: commit: %v", err)
	}
	return resp, nil
}

// readLevel reads the raw stock_levels row for (shopID, variantID,
// locationID) — mirrors stock_test.go's own readLevel.
func readLevel(ctx context.Context, t *testing.T, pool *pgxpool.Pool, shopID, variantID, locationID uuid.UUID) (qty decimal.Decimal, exists bool) {
	t.Helper()
	var n pgtype.Numeric
	err := pool.QueryRow(ctx, `SELECT qty FROM stock_levels WHERE shop_id = $1 AND variant_id = $2 AND location_id = $3`,
		shopID, variantID, locationID).Scan(&n)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return decimal.Decimal{}, false
		}
		t.Fatalf("readLevel: %v", err)
	}
	dec, err := money.FromNumeric(n)
	if err != nil {
		t.Fatalf("readLevel: money.FromNumeric: %v", err)
	}
	return dec, true
}

// countMovements counts stock_movements rows for (shopID, variantID,
// locationID), optionally narrowed to one kind (pass "" for any kind) —
// mirrors stock_test.go's own countMovements.
func countMovements(ctx context.Context, t *testing.T, pool *pgxpool.Pool, shopID, variantID, locationID uuid.UUID, kind db.StockMovementKind) int {
	t.Helper()
	var n int
	var err error
	if kind == "" {
		err = pool.QueryRow(ctx, `SELECT count(*) FROM stock_movements WHERE shop_id = $1 AND variant_id = $2 AND location_id = $3`,
			shopID, variantID, locationID).Scan(&n)
	} else {
		err = pool.QueryRow(ctx, `SELECT count(*) FROM stock_movements WHERE shop_id = $1 AND variant_id = $2 AND location_id = $3 AND kind = $4`,
			shopID, variantID, locationID, kind).Scan(&n)
	}
	if err != nil {
		t.Fatalf("countMovements: %v", err)
	}
	return n
}

// countSales counts sales rows for shopID — used to assert a failed
// CreateSaleTx leaves no sale behind.
func countSales(ctx context.Context, t *testing.T, pool *pgxpool.Pool, shopID uuid.UUID) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sales WHERE shop_id = $1`, shopID).Scan(&n); err != nil {
		t.Fatalf("countSales: %v", err)
	}
	return n
}

// seedSaleRow inserts a minimal, valid `sales` row directly (bypassing
// CreateSaleTx entirely) with a caller-chosen completed_at — the only way
// to give two sales an identical completed_at for
// TestListSales_cursorPaginationBreaksTiesByIdDescending, since
// completed_at defaults to `now()` and (like every other column but the
// four void ones) can never be UPDATEd afterwards (sales_immutable
// trigger, ADR-014) — a plain INSERT is unaffected by that trigger, which
// only fires BEFORE UPDATE OR DELETE. No items/payment: list.go's own
// queries never join into sale_items/sale_payments for a list row (only
// location/customer/user and the optional payment LEFT JOIN), so a bare
// header is enough to exercise ordering.
func seedSaleRow(ctx context.Context, t *testing.T, pool *pgxpool.Pool, shopID, locationID, cashierID uuid.UUID, number int64, completedAt time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO sales (id, shop_id, number, kind, location_id, cashier_id, subtotal, discount_amount, total, completed_at)
		VALUES ($1, $2, $3, 'sale', $4, $5, '0.00', '0.00', '0.00', $6)
	`, id, shopID, number, locationID, cashierID, completedAt)
	if err != nil {
		t.Fatalf("seedSaleRow: %v", err)
	}
	return id
}

// saleBody builds a one-line SaleCreate paid by cash — the common case
// most tests in this package need, with room to override discount/
// customer/note per call site.
func saleBody(locationID, variantID uuid.UUID, qty string) *gen.SaleCreate {
	return &gen.SaleCreate{
		LocationId: locationID,
		Items:      []gen.SaleItemCreate{{VariantId: variantID, Qty: qty}},
		Payment:    gen.SalePaymentCreate{Method: gen.Cash},
	}
}

// mustNow returns the current instant in shopID's own timezone —
// list_test.go's date-filter test builds its `from`/`to` query params
// relative to this, the same way ListSales itself resolves them
// (list.go's own saleDateRange).
func mustNow(t *testing.T, q *db.Queries, shopID uuid.UUID) time.Time {
	t.Helper()
	shopRow, err := q.GetShop(context.Background(), shopID)
	if err != nil {
		t.Fatalf("mustNow: GetShop: %v", err)
	}
	loc, err := time.LoadLocation(shopRow.Timezone)
	if err != nil {
		t.Fatalf("mustNow: LoadLocation(%q): %v", shopRow.Timezone, err)
	}
	return time.Now().In(loc)
}

// openapiDate builds a `?from=`/`?to=` query value from when's calendar
// date (docs/05-API.md: `YYYY-MM-DD`) — the wire shape
// gen.ListSalesParams.From/To expect.
func openapiDate(t *testing.T, when time.Time) oapitypes.Date {
	t.Helper()
	return oapitypes.Date{Time: time.Date(when.Year(), when.Month(), when.Day(), 0, 0, 0, 0, time.UTC)}
}
