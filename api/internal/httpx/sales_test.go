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
	"github.com/Sulton-Ali/savdo/api/internal/money"
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
	managerCtx context.Context
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
	manager, err := q.CreateUser(ctx, db.CreateUserParams{
		ID: uuid.New(), ShopID: shopRow.ID, Username: "manager1", PasswordHash: hash,
		FullName: "Manager One", Role: db.UserRoleManager, Locale: "uz",
	})
	if err != nil {
		t.Fatalf("CreateUser(manager): %v", err)
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
	managerCtx := auth.WithContext(context.Background(), auth.Context{
		ShopID: shopRow.ID, UserID: manager.ID, Role: db.UserRoleManager, SessionID: uuid.New(), Client: db.SessionClientWeb,
	})

	return saleFixture{
		srv: srv, pool: pool, q: q, shopID: shopRow.ID, cashierCtx: cashierCtx, managerCtx: managerCtx,
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

// mustCreateSale runs a plain (no Idempotency-Key) CreateSale through the
// server and unmarshals its response — a fixture step the return
// idempotency tests below need (an existing completed sale to return
// against), not itself the behaviour under test.
func mustCreateSale(t *testing.T, f saleFixture, qty string) gen.Sale {
	t.Helper()
	resp, err := f.srv.CreateSale(f.cashierCtx, createSaleReq(f.locationID, f.variantID, qty, nil))
	if err != nil {
		t.Fatalf("CreateSale (fixture): %v", err)
	}
	var sale gen.Sale
	if err := json.Unmarshal(resp.(rawJSONResponse).body, &sale); err != nil {
		t.Fatalf("unmarshal fixture sale: %v", err)
	}
	return sale
}

func createSaleReturnReq(saleID uuid.UUID, items []gen.SaleReturnItemCreate, key *string) gen.CreateSaleReturnRequestObject {
	return gen.CreateSaleReturnRequestObject{
		Id:     saleID,
		Params: gen.CreateSaleReturnParams{IdempotencyKey: key},
		Body:   &gen.SaleReturnCreate{Items: items},
	}
}

// TestCreateSaleReturn_idempotentReplayReturnsIdenticalBodyOneMovementSet
// mirrors TestCreateSale_idempotentReplayReturnsIdenticalBodyNoSecondMovement:
// a replayed Idempotency-Key must return the first response unchanged and
// must never write a second return_in movement or a second return sale.
func TestCreateSaleReturn_idempotentReplayReturnsIdenticalBodyOneMovementSet(t *testing.T) {
	f := newSaleFixture(t)
	sale := mustCreateSale(t, f, "2.000")
	key := "return-key-1"
	items := []gen.SaleReturnItemCreate{{SaleItemId: sale.Items[0].Id, Qty: "1.000"}}

	resp1, err := f.srv.CreateSaleReturn(f.managerCtx, createSaleReturnReq(sale.Id, items, &key))
	if err != nil {
		t.Fatalf("first return: %v", err)
	}
	resp2, err := f.srv.CreateSaleReturn(f.managerCtx, createSaleReturnReq(sale.Id, items, &key))
	if err != nil {
		t.Fatalf("replay return: %v", err)
	}

	var r1, r2 gen.Sale
	if err := json.Unmarshal(resp1.(rawJSONResponse).body, &r1); err != nil {
		t.Fatalf("unmarshal resp1: %v", err)
	}
	if err := json.Unmarshal(resp2.(rawJSONResponse).body, &r2); err != nil {
		t.Fatalf("unmarshal resp2: %v", err)
	}
	if r1.Id != r2.Id || r1.Number != r2.Number || r1.Total != r2.Total {
		t.Fatalf("replay body differs: %+v != %+v", r1, r2)
	}
	if r1.Kind != gen.SaleKind(db.SaleKindReturn) {
		t.Fatalf("Kind = %q, want return", r1.Kind)
	}

	var movementCount int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM stock_movements WHERE shop_id = $1 AND variant_id = $2 AND location_id = $3 AND kind = 'return_in'`,
		f.shopID, f.variantID, f.locationID).Scan(&movementCount); err != nil {
		t.Fatalf("count movements: %v", err)
	}
	if movementCount != 1 {
		t.Fatalf("return_in movements = %d, want 1 (the replay must not have written a second one)", movementCount)
	}

	var returnsCount int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM sales WHERE shop_id = $1 AND kind = 'return'`, f.shopID).Scan(&returnsCount); err != nil {
		t.Fatalf("count returns: %v", err)
	}
	if returnsCount != 1 {
		t.Fatalf("return sales = %d, want 1 (the replay must not have created a second one)", returnsCount)
	}
}

// TestCreateSaleReturn_sameKeyDifferentBodyReturns409 mirrors
// TestCreateSale_sameKeyDifferentBodyReturns409.
func TestCreateSaleReturn_sameKeyDifferentBodyReturns409(t *testing.T) {
	f := newSaleFixture(t)
	sale := mustCreateSale(t, f, "2.000")
	key := "return-key-shared"
	items1 := []gen.SaleReturnItemCreate{{SaleItemId: sale.Items[0].Id, Qty: "1.000"}}
	items2 := []gen.SaleReturnItemCreate{{SaleItemId: sale.Items[0].Id, Qty: "2.000"}}

	if _, err := f.srv.CreateSaleReturn(f.managerCtx, createSaleReturnReq(sale.Id, items1, &key)); err != nil {
		t.Fatalf("first return: %v", err)
	}

	// Same key, a different qty — a different canonical body, so this
	// must be rejected, not replayed and not run again.
	_, err := f.srv.CreateSaleReturn(f.managerCtx, createSaleReturnReq(sale.Id, items2, &key))
	if err == nil {
		t.Fatal("want 409 IDEMPOTENCY_KEY_REUSED for a different body reusing the key, got no error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 409 || apiErr.Code != gen.IDEMPOTENCYKEYREUSED {
		t.Fatalf("error = %v, want 409 IDEMPOTENCY_KEY_REUSED", err)
	}

	// Only the first return exists — the reused key must never have run a
	// second write.
	var returnsCount int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM sales WHERE shop_id = $1 AND kind = 'return'`, f.shopID).Scan(&returnsCount); err != nil {
		t.Fatalf("count returns: %v", err)
	}
	if returnsCount != 1 {
		t.Fatalf("return sales = %d, want 1", returnsCount)
	}
}

// readStockLevel reads the raw stock_levels row for (shopID, variantID,
// locationID) directly off the pool — used by the VoidSale end-to-end
// tests below to prove the levels a successful server.VoidSale restores
// are visible afterwards on a plain read, i.e. actually committed, not
// just returned in the response body.
func readStockLevel(t *testing.T, f saleFixture) string {
	t.Helper()
	var n pgtype.Numeric
	if err := f.pool.QueryRow(context.Background(),
		`SELECT qty FROM stock_levels WHERE shop_id = $1 AND variant_id = $2 AND location_id = $3`,
		f.shopID, f.variantID, f.locationID).Scan(&n); err != nil {
		t.Fatalf("readStockLevel: %v", err)
	}
	d, err := money.FromNumeric(n)
	if err != nil {
		t.Fatalf("readStockLevel: money.FromNumeric: %v", err)
	}
	return d.StringFixed(3)
}

func countSaleVoidInMovements(t *testing.T, f saleFixture) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM stock_movements WHERE shop_id = $1 AND variant_id = $2 AND location_id = $3 AND kind = 'sale_void_in'`,
		f.shopID, f.variantID, f.locationID).Scan(&n); err != nil {
		t.Fatalf("countSaleVoidInMovements: %v", err)
	}
	return n
}

// TestVoidSale_managerVoidsCommitsAndRestoresLevels is server.VoidSale's
// own end-to-end test (review MAJOR 1): it is the only handler with its
// own transaction lifecycle (no httpx.Idempotent), so nothing else
// exercises it through the real server method. A manager's void must
// answer 200 with the sale voided, and the restored level must already be
// visible on a plain read afterwards — proof the transaction this method
// opened was actually committed, not left open or rolled back.
func TestVoidSale_managerVoidsCommitsAndRestoresLevels(t *testing.T) {
	f := newSaleFixture(t)
	// newSaleFixture stocks 10.000 (saleFixture's own doc comment); selling
	// 3.000 leaves 7.000, so a full restore must land back on exactly
	// 10.000, not merely "higher than after the sale".
	levelBeforeSale := readStockLevel(t, f)
	if levelBeforeSale != "10.000" {
		t.Fatalf("level before sale = %s, want 10.000 (fixture precondition)", levelBeforeSale)
	}
	sale := mustCreateSale(t, f, "3.000")
	levelAfterSale := readStockLevel(t, f)
	if levelAfterSale != "7.000" {
		t.Fatalf("level after sale = %s, want 7.000", levelAfterSale)
	}

	resp, err := f.srv.VoidSale(f.managerCtx, gen.VoidSaleRequestObject{Id: sale.Id})
	if err != nil {
		t.Fatalf("VoidSale: %v", err)
	}
	voided, ok := resp.(gen.VoidSale200JSONResponse)
	if !ok {
		t.Fatalf("VoidSale response type = %T", resp)
	}
	if voided.Status != gen.SaleStatus(db.SaleStatusVoided) {
		t.Fatalf("Status = %q, want voided", voided.Status)
	}

	if got := readStockLevel(t, f); got != levelBeforeSale {
		t.Fatalf("level after void = %s, want it restored to exactly %s (the pre-sale level)", got, levelBeforeSale)
	}
	if got := countSaleVoidInMovements(t, f); got != 1 {
		t.Fatalf("sale_void_in movements = %d, want 1", got)
	}

	// A plain read after VoidSale returned must already see the voided
	// status and the restored level — both committed, not just present in
	// the response this call happened to build.
	var status db.SaleStatus
	if err := f.pool.QueryRow(context.Background(), `SELECT status FROM sales WHERE id = $1`, sale.Id).Scan(&status); err != nil {
		t.Fatalf("read sale status: %v", err)
	}
	if status != db.SaleStatusVoided {
		t.Fatalf("committed sale status = %q, want voided", status)
	}
}

// TestVoidSale_alreadyVoidedIs409WithNoSideEffect proves a failing void
// (SALE_ALREADY_VOIDED here) leaves no side effect — no extra movement,
// no level change — the same "roll back everything on any error" rule
// VoidSaleTx's own transaction gives every other failure path.
func TestVoidSale_alreadyVoidedIs409WithNoSideEffect(t *testing.T) {
	f := newSaleFixture(t)
	sale := mustCreateSale(t, f, "1.000")

	if _, err := f.srv.VoidSale(f.managerCtx, gen.VoidSaleRequestObject{Id: sale.Id}); err != nil {
		t.Fatalf("first void: %v", err)
	}
	levelAfterFirstVoid := readStockLevel(t, f)

	_, err := f.srv.VoidSale(f.managerCtx, gen.VoidSaleRequestObject{Id: sale.Id})
	if err == nil {
		t.Fatal("want 409 SALE_ALREADY_VOIDED for a second void, got no error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 409 || apiErr.Code != gen.SALEALREADYVOIDED {
		t.Fatalf("error = %v, want 409 SALE_ALREADY_VOIDED", err)
	}

	if got := countSaleVoidInMovements(t, f); got != 1 {
		t.Fatalf("sale_void_in movements = %d, want 1 (the failing second void must not have written another)", got)
	}
	if got := readStockLevel(t, f); got != levelAfterFirstVoid {
		t.Fatalf("level after failed second void = %s, want unchanged %s", got, levelAfterFirstVoid)
	}
}

// TestVoidSale_cashierForbiddenWithNoSideEffect proves a cashier's void
// is rejected before any write — 403, no movement, no status change.
func TestVoidSale_cashierForbiddenWithNoSideEffect(t *testing.T) {
	f := newSaleFixture(t)
	sale := mustCreateSale(t, f, "1.000")

	_, err := f.srv.VoidSale(f.cashierCtx, gen.VoidSaleRequestObject{Id: sale.Id})
	if err == nil {
		t.Fatal("want 403 FORBIDDEN for a cashier's void, got no error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 403 || apiErr.Code != gen.FORBIDDEN {
		t.Fatalf("error = %v, want 403 FORBIDDEN", err)
	}

	if got := countSaleVoidInMovements(t, f); got != 0 {
		t.Fatalf("sale_void_in movements = %d, want 0", got)
	}
	var status db.SaleStatus
	if err := f.pool.QueryRow(context.Background(), `SELECT status FROM sales WHERE id = $1`, sale.Id).Scan(&status); err != nil {
		t.Fatalf("read sale status: %v", err)
	}
	if status != db.SaleStatusCompleted {
		t.Fatalf("sale status = %q, want completed (unchanged)", status)
	}
}

// createSaleDraftReq builds a one-line CreateSaleDraftRequestObject —
// mirrors createSaleReq, minus payment/Idempotency-Key (POST
// /sales/drafts carries none, D-87/D-89).
func createSaleDraftReq(locationID, variantID uuid.UUID, qty string) gen.CreateSaleDraftRequestObject {
	return gen.CreateSaleDraftRequestObject{
		Body: &gen.SaleDraftCreate{
			LocationId: locationID,
			Items:      []gen.SaleItemCreate{{VariantId: variantID, Qty: qty}},
		},
	}
}

// mustCreateSaleDraft runs a plain CreateSaleDraft through the server and
// unmarshals its response — a fixture step the completion idempotency
// tests below need (an existing draft to complete), not itself the
// behaviour under test. Mirrors mustCreateSale.
func mustCreateSaleDraft(t *testing.T, f saleFixture, qty string) gen.SaleDraft {
	t.Helper()
	resp, err := f.srv.CreateSaleDraft(f.cashierCtx, createSaleDraftReq(f.locationID, f.variantID, qty))
	if err != nil {
		t.Fatalf("CreateSaleDraft (fixture): %v", err)
	}
	draft, ok := resp.(gen.CreateSaleDraft201JSONResponse)
	if !ok {
		t.Fatalf("CreateSaleDraft response type = %T", resp)
	}
	return gen.SaleDraft(draft)
}

func completeSaleDraftReq(id uuid.UUID, method gen.PaymentMethod, key *string) gen.CompleteSaleDraftRequestObject {
	return gen.CompleteSaleDraftRequestObject{
		Id:     id,
		Params: gen.CompleteSaleDraftParams{IdempotencyKey: key},
		Body:   &gen.SaleDraftComplete{PaymentMethod: method},
	}
}

// TestCompleteSaleDraft_idempotentReplayReturnsIdenticalSaleNoSecondMovementDraftGone
// mirrors TestCreateSale_idempotentReplayReturnsIdenticalBodyNoSecondMovement:
// a replayed Idempotency-Key must return the first response unchanged,
// must never write a second sale_out movement or a second sale, and must
// not attempt to touch the draft a second time — it is already gone after
// the first, successful completion, and the replay must still succeed
// (Idempotent's own "return the stored response, never re-run fn" rule).
func TestCompleteSaleDraft_idempotentReplayReturnsIdenticalSaleNoSecondMovementDraftGone(t *testing.T) {
	f := newSaleFixture(t)
	draft := mustCreateSaleDraft(t, f, "1.000")
	key := "complete-draft-key-1"

	resp1, err := f.srv.CompleteSaleDraft(f.cashierCtx, completeSaleDraftReq(draft.Id, gen.Cash, &key))
	if err != nil {
		t.Fatalf("first complete: %v", err)
	}
	resp2, err := f.srv.CompleteSaleDraft(f.cashierCtx, completeSaleDraftReq(draft.Id, gen.Cash, &key))
	if err != nil {
		t.Fatalf("replay complete: %v", err)
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

	var movementCount int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM stock_movements WHERE shop_id = $1 AND variant_id = $2 AND location_id = $3 AND kind = 'sale_out'`,
		f.shopID, f.variantID, f.locationID).Scan(&movementCount); err != nil {
		t.Fatalf("count movements: %v", err)
	}
	if movementCount != 1 {
		t.Fatalf("sale_out movements = %d, want 1 (the replay must not have written a second one)", movementCount)
	}

	var salesCount int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM sales WHERE shop_id = $1`, f.shopID).Scan(&salesCount); err != nil {
		t.Fatalf("count sales: %v", err)
	}
	if salesCount != 1 {
		t.Fatalf("sales rows = %d, want 1 (the replay must not have created a second sale)", salesCount)
	}

	var draftCount int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM sale_drafts WHERE shop_id = $1`, f.shopID).Scan(&draftCount); err != nil {
		t.Fatalf("count sale_drafts: %v", err)
	}
	if draftCount != 0 {
		t.Fatalf("sale_drafts rows = %d, want 0 (completion deletes the draft; the replay must not error over its absence)", draftCount)
	}
}

// TestCompleteSaleDraft_sameKeyDifferentBodyReturns409 mirrors
// TestCreateSale_sameKeyDifferentBodyReturns409: the same draft id and
// key with a different payment method is a different canonical request
// and must be rejected outright, not replayed and not run again.
func TestCompleteSaleDraft_sameKeyDifferentBodyReturns409(t *testing.T) {
	f := newSaleFixture(t)
	draft := mustCreateSaleDraft(t, f, "1.000")
	key := "complete-draft-key-shared"

	if _, err := f.srv.CompleteSaleDraft(f.cashierCtx, completeSaleDraftReq(draft.Id, gen.Cash, &key)); err != nil {
		t.Fatalf("first complete: %v", err)
	}

	_, err := f.srv.CompleteSaleDraft(f.cashierCtx, completeSaleDraftReq(draft.Id, gen.Card, &key))
	if err == nil {
		t.Fatal("want 409 IDEMPOTENCY_KEY_REUSED for a different body reusing the key, got no error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 409 || apiErr.Code != gen.IDEMPOTENCYKEYREUSED {
		t.Fatalf("error = %v, want 409 IDEMPOTENCY_KEY_REUSED", err)
	}

	var salesCount int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM sales WHERE shop_id = $1`, f.shopID).Scan(&salesCount); err != nil {
		t.Fatalf("count sales: %v", err)
	}
	if salesCount != 1 {
		t.Fatalf("sales rows = %d, want 1 (the reused key must never have run a second write)", salesCount)
	}
}

// TestCompleteSaleDraft_noKeyEachCallCompletesItsOwnDraft mirrors
// TestCreateSale_noKeyEachCallWritesItsOwnSale: with no Idempotency-Key,
// completing two separate drafts is two separate writes.
func TestCompleteSaleDraft_noKeyEachCallCompletesItsOwnDraft(t *testing.T) {
	f := newSaleFixture(t)
	draftA := mustCreateSaleDraft(t, f, "1.000")
	draftB := mustCreateSaleDraft(t, f, "1.000")

	if _, err := f.srv.CompleteSaleDraft(f.cashierCtx, completeSaleDraftReq(draftA.Id, gen.Cash, nil)); err != nil {
		t.Fatalf("complete draftA: %v", err)
	}
	if _, err := f.srv.CompleteSaleDraft(f.cashierCtx, completeSaleDraftReq(draftB.Id, gen.Cash, nil)); err != nil {
		t.Fatalf("complete draftB: %v", err)
	}

	var salesCount int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM sales WHERE shop_id = $1`, f.shopID).Scan(&salesCount); err != nil {
		t.Fatalf("count sales: %v", err)
	}
	if salesCount != 2 {
		t.Fatalf("sales rows = %d, want 2", salesCount)
	}
}

// TestCompleteSaleDraft_sameKeyDifferentDraftIdReturns409SecondDraftUntouched
// mirrors TestCompleteSaleDraft_sameKeyDifferentBodyReturns409, naming the
// draft id itself (part of RequestHash's path input, completeSaleDraftPath)
// rather than the body as the thing that differs: completing two distinct
// drafts under the same Idempotency-Key must reject the second outright,
// leaving it completely untouched — not replay draftA's stored sale for
// draftB, and not run draftB's own completion either.
func TestCompleteSaleDraft_sameKeyDifferentDraftIdReturns409SecondDraftUntouched(t *testing.T) {
	f := newSaleFixture(t)
	draftA := mustCreateSaleDraft(t, f, "1.000")
	draftB := mustCreateSaleDraft(t, f, "1.000")
	key := "complete-draft-key-two-drafts"

	if _, err := f.srv.CompleteSaleDraft(f.cashierCtx, completeSaleDraftReq(draftA.Id, gen.Cash, &key)); err != nil {
		t.Fatalf("first complete (draftA): %v", err)
	}

	_, err := f.srv.CompleteSaleDraft(f.cashierCtx, completeSaleDraftReq(draftB.Id, gen.Cash, &key))
	if err == nil {
		t.Fatal("want 409 IDEMPOTENCY_KEY_REUSED for the same key against a different draft id, got no error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 409 || apiErr.Code != gen.IDEMPOTENCYKEYREUSED {
		t.Fatalf("error = %v, want 409 IDEMPOTENCY_KEY_REUSED", err)
	}

	// draftB must still exist, untouched — the reused key must never
	// have run CompleteSaleDraftTx against it.
	var draftBCount int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM sale_drafts WHERE id = $1`, draftB.Id).Scan(&draftBCount); err != nil {
		t.Fatalf("count draftB: %v", err)
	}
	if draftBCount != 1 {
		t.Fatalf("sale_drafts rows for draftB = %d, want 1 (untouched)", draftBCount)
	}
	var salesCount int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM sales WHERE shop_id = $1`, f.shopID).Scan(&salesCount); err != nil {
		t.Fatalf("count sales: %v", err)
	}
	if salesCount != 1 {
		t.Fatalf("sales rows = %d, want 1 (only draftA's completion)", salesCount)
	}
}
