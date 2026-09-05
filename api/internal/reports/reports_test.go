// Package reports_test exercises reports.Handler directly (not through
// internal/httpx's router) against a real testcontainers Postgres — the
// same style stock_test/crm_test use. Fixtures here insert sales straight
// through the sqlc queries (InsertSale/InsertSaleItem/NextSaleNumber) the
// way sales.Service itself will, per this task's own instruction not to
// depend on the sales module (built in a parallel task); a handful of
// tests that need a specific `completed_at` (the shop-timezone boundary,
// "yesterday" exclusion) insert the header row with a direct SQL INSERT
// instead — the same escape hatch api/internal/db/sales_schema_test.go
// itself uses to exercise cases InsertSale's own fixed `now()` default
// cannot reach; INSERT never runs the sales_immutable trigger (it only
// fires on UPDATE/DELETE), so this is not a way around ADR-014.
package reports_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

// dateParam builds a `*openapi_types.Date` for t's year/month/day (the
// component that matters — a request parameter is always a bare
// `YYYY-MM-DD`), the way oapi-codegen's own generated binder would parse
// one off the wire.
func dateParam(t time.Time) *openapi_types.Date {
	d := openapi_types.Date{Time: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)}
	return &d
}

// assertDecimal compares a wire decimal string field against its expected
// literal — every money/quantity field in this package's tests goes
// through this, never a float comparison (§ 04-DATA-MODEL.md rule 3).
func assertDecimal(t *testing.T, label, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %q, want %q", label, got, want)
	}
}

// numeric parses a decimal literal into a valid pgtype.Numeric — mirrors
// stock_test's/db_test's helper of the same name.
func numeric(t *testing.T, s string) pgtype.Numeric {
	t.Helper()
	var n pgtype.Numeric
	if err := n.Scan(s); err != nil {
		t.Fatalf("numeric(%q): %v", s, err)
	}
	return n
}

// newTestQueries starts a fresh, truncated testcontainers Postgres and
// returns both the pool (for the raw-completed_at INSERTs above) and
// *db.Queries bound to it.
func newTestQueries(t *testing.T) (*pgxpool.Pool, *db.Queries) {
	t.Helper()
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	return pool, db.New(pool)
}

// seedShop creates a shop, leaving timezone/default_locale at their
// migration defaults (Asia/Tashkent, uz — 04-DATA-MODEL.md § 1), which is
// exactly the timezone the shop-timezone-boundary tests need.
func seedShop(ctx context.Context, t *testing.T, q *db.Queries, slug string) db.Shop {
	t.Helper()
	shopRow, err := q.CreateShop(ctx, db.CreateShopParams{ID: uuid.New(), Slug: slug, Name: "Reports Shop " + slug})
	if err != nil {
		t.Fatalf("seedShop(%q): %v", slug, err)
	}
	return shopRow
}

func seedUser(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, username string, role db.UserRole) db.User {
	t.Helper()
	user, err := q.CreateUser(ctx, db.CreateUserParams{
		ID: uuid.New(), ShopID: shopID, Username: username, PasswordHash: "hash",
		FullName: "Reports Test User " + username, Role: role, Locale: "uz",
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

func seedProduct(ctx context.Context, t *testing.T, q *db.Queries, shopID, unitID uuid.UUID, slug string) db.Product {
	t.Helper()
	p, err := q.CreateProduct(ctx, db.CreateProductParams{
		ID: uuid.New(), ShopID: shopID, UnitID: unitID, Slug: slug,
		BasePrice: numeric(t, "125000.00"), IsActive: true,
	})
	if err != nil {
		t.Fatalf("seedProduct(%q): %v", slug, err)
	}
	return p
}

func seedVariant(ctx context.Context, t *testing.T, q *db.Queries, shopID, productID uuid.UUID) db.ProductVariant {
	t.Helper()
	v, err := q.CreateVariant(ctx, db.CreateVariantParams{
		ID: uuid.New(), ShopID: shopID, ProductID: productID,
		Attributes: json.RawMessage("{}"), IsActive: true,
	})
	if err != nil {
		t.Fatalf("seedVariant: %v", err)
	}
	return v
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

// ctxAs builds the auth.Context a real request would carry for user
// (ADR-004/ADR-010) — mirrors stock_test.ctxAs.
func ctxAs(shopID uuid.UUID, user db.User) context.Context {
	return auth.WithContext(context.Background(), auth.Context{
		ShopID: shopID, UserID: user.ID, Role: user.Role, SessionID: uuid.New(), Client: db.SessionClientWeb,
	})
}

// saleItemSpec is one line for newSale below — mirrors
// api/internal/db/sales_schema_test.go's own saleItemSpec.
type saleItemSpec struct {
	variantID          uuid.UUID
	qty                string
	unitPrice          string
	unitCost           string
	lineTotal          string
	originalSaleItemID *uuid.UUID
}

// newSale claims the next sale number and writes a complete sale (header +
// items) through the sqlc queries only — no raw SQL, no dependency on the
// (parallel-task) sales module. completed_at is whatever InsertSale's own
// default (`now()`) gives it; tests that need a specific date use
// insertSaleAt below instead.
func newSale(
	ctx context.Context, t *testing.T, q *db.Queries,
	shopID, locationID, cashierID uuid.UUID,
	kind db.SaleKind, originalSaleID *uuid.UUID,
	subtotal, discount, total string, items []saleItemSpec,
) (db.Sale, []db.SaleItem) {
	t.Helper()
	num, err := q.NextSaleNumber(ctx, shopID)
	if err != nil {
		t.Fatalf("NextSaleNumber: %v", err)
	}
	sale, err := q.InsertSale(ctx, db.InsertSaleParams{
		ID: uuid.New(), ShopID: shopID, Number: num, Kind: kind,
		LocationID: locationID, CashierID: cashierID, OriginalSaleID: originalSaleID,
		Subtotal: numeric(t, subtotal), DiscountAmount: numeric(t, discount), Total: numeric(t, total),
	})
	if err != nil {
		t.Fatalf("InsertSale: %v", err)
	}
	rows := make([]db.SaleItem, 0, len(items))
	for _, it := range items {
		row, err := q.InsertSaleItem(ctx, db.InsertSaleItemParams{
			ID: uuid.New(), ShopID: shopID, SaleID: sale.ID, VariantID: it.variantID,
			Qty: numeric(t, it.qty), UnitPrice: numeric(t, it.unitPrice), UnitCost: numeric(t, it.unitCost),
			LineTotal: numeric(t, it.lineTotal), OriginalSaleItemID: it.originalSaleItemID,
		})
		if err != nil {
			t.Fatalf("InsertSaleItem: %v", err)
		}
		rows = append(rows, row)
	}
	return sale, rows
}

// insertSaleAt writes a bare `sale`-kind, no-item sale header at an exact
// completed_at, via a direct SQL INSERT (see this file's own package doc
// comment on why: InsertSale always defaults completed_at to now(), and
// the sales_immutable trigger refuses to let a later UPDATE change it).
// Used only by the shop-timezone-boundary and "cashier's own day"
// exclusion tests, which care about which side of a date boundary a sale
// falls on, not about its items/cost — SalesSummaryForStaff/ForCashier's
// revenue/count/discount columns come entirely from the sales header, and
// a header with no items simply never produces a row in
// SalesSummaryForStaff's own sale_cost CTE (a plain JOIN against
// sale_items, not a LEFT JOIN — see reports.sql), so its cost contribution
// is 0 via that query's own COALESCE on the missing row, not a join
// artifact.
func insertSaleAt(ctx context.Context, t *testing.T, pool *pgxpool.Pool, q *db.Queries, shopID, locationID, cashierID uuid.UUID, completedAt time.Time) uuid.UUID {
	t.Helper()
	num, err := q.NextSaleNumber(ctx, shopID)
	if err != nil {
		t.Fatalf("NextSaleNumber: %v", err)
	}
	id := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO sales (id, shop_id, number, kind, location_id, cashier_id, subtotal, discount_amount, total, completed_at)
		VALUES ($1, $2, $3, 'sale', $4, $5, 10.00, 0.00, 10.00, $6)
	`, id, shopID, num, locationID, cashierID, completedAt)
	if err != nil {
		t.Fatalf("insertSaleAt: %v", err)
	}
	return id
}
