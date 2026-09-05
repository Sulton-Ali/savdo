package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
	"github.com/Sulton-Ali/savdo/api/internal/sales"
	"github.com/Sulton-Ali/savdo/api/internal/stock"
)

// saleFixture wires a bare server{} (pool + *sales.Handler only, same
// idiom purchaseFixture uses) against a real Postgres, with one shop, one
// cashier, one location and one stocked variant seeded directly through
// sqlc.
type saleFixture struct {
	srv        server
	pool       *pgxpool.Pool
	q          *db.Queries
	shopID     uuid.UUID
	cashierCtx context.Context
	locationID uuid.UUID
	variantID  uuid.UUID
}

func newSaleFixture(t *testing.T) saleFixture {
	t.Helper()
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	q := db.New(pool)
	ctx := context.Background()

	shopRow, err := q.CreateShop(ctx, db.CreateShopParams{ID: uuid.New(), Slug: "sale-idem-" + uuid.NewString(), Name: "Sale Idem Shop"})
	if err != nil {
		t.Fatalf("CreateShop: %v", err)
	}
	hash, err := auth.Hash("correct-horse-battery")
	if err != nil {
		t.Fatalf("auth.Hash: %v", err)
	}
	cashier, err := q.CreateUser(ctx, db.CreateUserParams{
		ID: uuid.New(), ShopID: shopRow.ID, Username: "cashier1", PasswordHash: hash,
		FullName: "Cashier One", Role: db.UserRoleCashier, Locale: "uz",
	})
	if err != nil {
		t.Fatalf("CreateUser(cashier): %v", err)
	}
	unit, err := q.UpsertUnit(ctx, db.UpsertUnitParams{ID: uuid.New(), ShopID: shopRow.ID, Code: "pcs", Precision: 0})
	if err != nil {
		t.Fatalf("UpsertUnit: %v", err)
	}
	var basePrice pgtype.Numeric
	if err := basePrice.Scan("100.00"); err != nil {
		t.Fatalf("basePrice.Scan: %v", err)
	}
	product, err := q.CreateProduct(ctx, db.CreateProductParams{
		ID: uuid.New(), ShopID: shopRow.ID, UnitID: unit.ID, Slug: "sale-idem-product",
		BasePrice: basePrice, IsActive: true,
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	variant, err := q.CreateVariant(ctx, db.CreateVariantParams{
		ID: uuid.New(), ShopID: shopRow.ID, ProductID: product.ID, Attributes: json.RawMessage(`{}`), IsActive: true,
	})
	if err != nil {
		t.Fatalf("CreateVariant: %v", err)
	}
	loc, err := q.CreateLocation(ctx, db.CreateLocationParams{
		ID: uuid.New(), ShopID: shopRow.ID, Name: "Main", Kind: db.LocationKindStore, IsActive: true,
	})
	if err != nil {
		t.Fatalf("CreateLocation: %v", err)
	}

	stockSvc := stock.NewService(pool, q)
	if _, err := stockSvc.MoveInTx(ctx, stock.MoveParams{
		ShopID: shopRow.ID, VariantID: variant.ID, LocationID: loc.ID,
		Kind: db.StockMovementKindPurchaseIn, Qty: decimal.RequireFromString("10.000"),
	}); err != nil {
		t.Fatalf("stock in: %v", err)
	}

	salesSvc := sales.NewService(q)
	srv := server{pool: pool, sales: sales.NewHandler(salesSvc)}

	cashierCtx := auth.WithContext(context.Background(), auth.Context{
		ShopID: shopRow.ID, UserID: cashier.ID, Role: db.UserRoleCashier, SessionID: uuid.New(), Client: db.SessionClientWeb,
	})

	return saleFixture{
		srv: srv, pool: pool, q: q, shopID: shopRow.ID, cashierCtx: cashierCtx,
		locationID: loc.ID, variantID: variant.ID,
	}
}

func createSaleReq(locationID, variantID uuid.UUID, qty string, key *string) gen.CreateSaleRequestObject {
	return gen.CreateSaleRequestObject{
		Params: gen.CreateSaleParams{IdempotencyKey: key},
		Body: &gen.SaleCreate{
			LocationId: locationID,
			Items:      []gen.SaleItemCreate{{VariantId: variantID, Qty: qty}},
			Payment:    gen.SalePaymentCreate{Method: gen.Cash},
		},
	}
}

func TestCreateSale_idempotentReplayReturnsIdenticalBodyNoSecondMovement(t *testing.T) {
	f := newSaleFixture(t)
	key := "create-sale-key-1"

	resp1, err := f.srv.CreateSale(f.cashierCtx, createSaleReq(f.locationID, f.variantID, "1.000", &key))
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	resp2, err := f.srv.CreateSale(f.cashierCtx, createSaleReq(f.locationID, f.variantID, "1.000", &key))
	if err != nil {
		t.Fatalf("replay create: %v", err)
	}

	var s1, s2 gen.Sale
	if err := json.Unmarshal(resp1.(rawJSONResponse).body, &s1); err != nil {
		t.Fatalf("unmarshal resp1: %v", err)
	}
	if err := json.Unmarshal(resp2.(rawJSONResponse).body, &s2); err != nil {
		t.Fatalf("unmarshal resp2: %v", err)
	}
	if s1.Id != s2.Id || s1.Number != s2.Number || s1.Total != s2.Total {
		t.Fatalf("replay body differs: %+v != %+v", s1, s2)
	}
	if s1.Status != gen.SaleStatus(db.SaleStatusCompleted) {
		t.Fatalf("Status = %q, want completed", s1.Status)
	}

	var count int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM stock_movements WHERE shop_id = $1 AND variant_id = $2 AND location_id = $3 AND kind = 'sale_out'`,
		f.shopID, f.variantID, f.locationID).Scan(&count); err != nil {
		t.Fatalf("count movements: %v", err)
	}
	if count != 1 {
		t.Fatalf("sale_out movements = %d, want 1 (the replay must not have written a second one)", count)
	}

	var salesCount int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM sales WHERE shop_id = $1`, f.shopID).Scan(&salesCount); err != nil {
		t.Fatalf("count sales: %v", err)
	}
	if salesCount != 1 {
		t.Fatalf("sales rows = %d, want 1 (the replay must not have created a second sale)", salesCount)
	}
}

func TestCreateSale_sameKeyDifferentBodyReturns409(t *testing.T) {
	f := newSaleFixture(t)
	key := "create-sale-key-shared"

	if _, err := f.srv.CreateSale(f.cashierCtx, createSaleReq(f.locationID, f.variantID, "1.000", &key)); err != nil {
		t.Fatalf("first create: %v", err)
	}

	// Same key, a different qty — a different canonical body, so this
	// must be rejected, not replayed and not run again.
	_, err := f.srv.CreateSale(f.cashierCtx, createSaleReq(f.locationID, f.variantID, "2.000", &key))
	if err == nil {
		t.Fatal("want 409 IDEMPOTENCY_KEY_REUSED for a different body reusing the key, got no error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 409 || apiErr.Code != gen.IDEMPOTENCYKEYREUSED {
		t.Fatalf("error = %v, want 409 IDEMPOTENCY_KEY_REUSED", err)
	}

	// Only the first sale exists — the reused key must never have run a
	// second write.
	var salesCount int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM sales WHERE shop_id = $1`, f.shopID).Scan(&salesCount); err != nil {
		t.Fatalf("count sales: %v", err)
	}
	if salesCount != 1 {
		t.Fatalf("sales rows = %d, want 1", salesCount)
	}
}

func TestCreateSale_noKeyEachCallWritesItsOwnSale(t *testing.T) {
	f := newSaleFixture(t)

	if _, err := f.srv.CreateSale(f.cashierCtx, createSaleReq(f.locationID, f.variantID, "1.000", nil)); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := f.srv.CreateSale(f.cashierCtx, createSaleReq(f.locationID, f.variantID, "1.000", nil)); err != nil {
		t.Fatalf("second create: %v", err)
	}

	var salesCount int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM sales WHERE shop_id = $1`, f.shopID).Scan(&salesCount); err != nil {
		t.Fatalf("count sales: %v", err)
	}
	if salesCount != 2 {
		t.Fatalf("sales rows = %d, want 2 (no Idempotency-Key: every call is a fresh write)", salesCount)
	}
}

// TestCreateSale_retryWithSameKeyAfterStockAddedSucceeds is MINOR 7's own
// test: a genuine failure (409 STOCK_INSUFFICIENT) is never stored
// against its key (Idempotent's own "only store on success" rule), so a
// client that fixes the underlying condition — here, more stock arrives —
// can retry with the very same Idempotency-Key and succeed.
func TestCreateSale_retryWithSameKeyAfterStockAddedSucceeds(t *testing.T) {
	f := newSaleFixture(t)
	key := "retry-after-restock"

	// The fixture stocks 10.000; ask for more than that.
	_, err := f.srv.CreateSale(f.cashierCtx, createSaleReq(f.locationID, f.variantID, "20.000", &key))
	if err == nil {
		t.Fatal("want 409 STOCK_INSUFFICIENT on the first attempt, got no error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 409 || apiErr.Code != gen.STOCKINSUFFICIENT {
		t.Fatalf("first attempt error = %v, want 409 STOCK_INSUFFICIENT", err)
	}

	// Idempotent's own "only store on success" rule: a genuine failure
	// must leave no idempotency_keys row at all, or the retry below would
	// hit a stored hash instead of actually running again.
	var keyCount int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM idempotency_keys WHERE shop_id = $1 AND key = $2`, f.shopID, key).Scan(&keyCount); err != nil {
		t.Fatalf("count idempotency_keys: %v", err)
	}
	if keyCount != 0 {
		t.Fatalf("idempotency_keys rows for %q = %d, want 0 (a failed attempt must not be stored)", key, keyCount)
	}

	stockSvc := stock.NewService(f.pool, f.q)
	if _, err := stockSvc.MoveInTx(context.Background(), stock.MoveParams{
		ShopID: f.shopID, VariantID: f.variantID, LocationID: f.locationID,
		Kind: db.StockMovementKindPurchaseIn, Qty: decimal.RequireFromString("20.000"),
	}); err != nil {
		t.Fatalf("restock: %v", err)
	}

	resp, err := f.srv.CreateSale(f.cashierCtx, createSaleReq(f.locationID, f.variantID, "20.000", &key))
	if err != nil {
		t.Fatalf("retry with the same key after restocking: %v", err)
	}
	var sale gen.Sale
	if err := json.Unmarshal(resp.(rawJSONResponse).body, &sale); err != nil {
		t.Fatalf("unmarshal retry response: %v", err)
	}
	if sale.Status != gen.SaleStatus(db.SaleStatusCompleted) {
		t.Fatalf("Status = %q, want completed", sale.Status)
	}

	var salesCount int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM sales WHERE shop_id = $1`, f.shopID).Scan(&salesCount); err != nil {
		t.Fatalf("count sales: %v", err)
	}
	if salesCount != 1 {
		t.Fatalf("sales rows = %d, want 1 (the failed first attempt must not have created one)", salesCount)
	}
}
