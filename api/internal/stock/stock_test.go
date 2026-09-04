package stock_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
	"github.com/Sulton-Ali/savdo/api/internal/money"
)

// numeric parses a decimal literal into a valid pgtype.Numeric, the way a
// caller supplying a NUMERIC parameter would — mirrors internal/db's own
// catalog_test.go helper of the same name.
func numeric(t *testing.T, s string) pgtype.Numeric {
	t.Helper()
	var n pgtype.Numeric
	if err := n.Scan(s); err != nil {
		t.Fatalf("numeric(%q): %v", s, err)
	}
	return n
}

// newTestQueries starts a fresh, truncated testcontainers Postgres and
// returns both the pool (for opening transactions the way Move's callers
// do) and *db.Queries bound directly to it (for setup/assertions outside
// any transaction).
func newTestQueries(t *testing.T) (*pgxpool.Pool, *db.Queries) {
	t.Helper()
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	return pool, db.New(pool)
}

func seedShop(ctx context.Context, t *testing.T, q *db.Queries, slug string) db.Shop {
	t.Helper()
	shopRow, err := q.CreateShop(ctx, db.CreateShopParams{ID: uuid.New(), Slug: slug, Name: "Stock Shop " + slug})
	if err != nil {
		t.Fatalf("seedShop(%q): %v", slug, err)
	}
	return shopRow
}

// seedShopAllowNegative is seedShop plus flipping allow_negative_stock on
// (D-41/D-48's second branch) — UpdateShop is the only writer of that
// column outside a migration default.
func seedShopAllowNegative(ctx context.Context, t *testing.T, q *db.Queries, slug string) db.Shop {
	t.Helper()
	shopRow := seedShop(ctx, t, q, slug)
	allow := true
	updated, err := q.UpdateShop(ctx, db.UpdateShopParams{ID: shopRow.ID, AllowNegativeStock: &allow})
	if err != nil {
		t.Fatalf("seedShopAllowNegative(%q): UpdateShop: %v", slug, err)
	}
	return updated
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

func seedVariant(ctx context.Context, t *testing.T, q *db.Queries, shopID, productID uuid.UUID, attrs string) db.ProductVariant {
	t.Helper()
	v, err := q.CreateVariant(ctx, db.CreateVariantParams{
		ID: uuid.New(), ShopID: shopID, ProductID: productID,
		Attributes: json.RawMessage(attrs), IsActive: true,
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

// seedUser creates a real users row — Move's ActorID and audit_log's
// actor_id both have FKs to users(id), so a test exercising a movement or
// an adjustment/transfer through the handler (which always sets
// created_by/actor_id to the authenticated caller) needs a real row, not
// just a random uuid.New() the way catalog_test.go's ctxAs gets away with
// for modules that never reference created_by.
func seedUser(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, username string, role db.UserRole) db.User {
	t.Helper()
	hash, err := auth.Hash("correct-horse-battery")
	if err != nil {
		t.Fatalf("seedUser: Hash: %v", err)
	}
	user, err := q.CreateUser(ctx, db.CreateUserParams{
		ID: uuid.New(), ShopID: shopID, Username: username, PasswordHash: hash,
		FullName: "Stock Test User " + username, Role: role, Locale: "uz",
	})
	if err != nil {
		t.Fatalf("seedUser(%q): %v", username, err)
	}
	return user
}

// readLevel reads the raw stock_levels row for (shopID, variantID,
// locationID) with a plain, non-locking SELECT — a test's own
// verification query, never the codepath under test (which always goes
// through GetLevelForUpdate inside a transaction). exists is false when no
// row was ever written for the combination (UpsertLevelRow never ran, or
// ran and then rolled back). Returned as decimal.Decimal, compared with
// .Equal rather than a rendered string: numeric addition can return a
// pgtype.Numeric whose Int/Exp round-trips to "0" rather than "0.000" for
// an exact-zero result (the same reason api/internal/db/stock_schema_test.go
// has its own normalizeScale3 helper) — decimal.Decimal.Equal is exponent-
// agnostic, so it is not fooled by that either way.
func readLevel(ctx context.Context, t *testing.T, pool *pgxpool.Pool, shopID, variantID, locationID uuid.UUID) (qty decimal.Decimal, exists bool) {
	t.Helper()
	var n pgtype.Numeric
	err := pool.QueryRow(ctx, `SELECT qty FROM stock_levels WHERE shop_id = $1 AND variant_id = $2 AND location_id = $3`,
		shopID, variantID, locationID).Scan(&n)
	if err != nil {
		return decimal.Decimal{}, false
	}
	d, err := money.FromNumeric(n)
	if err != nil {
		t.Fatalf("readLevel: money.FromNumeric: %v", err)
	}
	return d, true
}

// countMovements counts stock_movements rows for (shopID, variantID,
// locationID), optionally narrowed to one kind (pass "" for any kind).
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

// ctxAs builds a context carrying the auth.Context a real request would
// have after auth.Service.Middleware ran, for a real seeded user — mirrors
// catalog_test.go's ctxAs, except UserID is always a real users row's id
// (see seedUser's doc comment) rather than uuid.New().
func ctxAs(shopID uuid.UUID, user db.User) context.Context {
	return auth.WithContext(context.Background(), auth.Context{
		ShopID: shopID, UserID: user.ID, Role: user.Role, SessionID: uuid.New(), Client: db.SessionClientWeb,
	})
}
