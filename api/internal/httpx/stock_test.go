package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
	"github.com/Sulton-Ali/savdo/api/internal/stock"
)

// stockAdjustmentFixture wires a bare server{} (pool + *stock.Handler only
// — every other field this test never touches stays nil, same idiom
// testAuthService/testCatalogService use for their own unused
// dependencies) against a real Postgres, with one shop, one manager user,
// one variant and one location seeded directly through sqlc.
type stockAdjustmentFixture struct {
	srv        server
	pool       *pgxpool.Pool
	q          *db.Queries
	shopID     uuid.UUID
	managerCtx context.Context
	variantID  uuid.UUID
	locationID uuid.UUID
}

// seedStockAdjustmentFixture is newStockAdjustmentFixture's setup, split
// out so TestCreateStockAdjustment_concurrentDistinctKeysUnderSmallPoolCompletes
// (BLOCKER 1's own test) can reuse it against a pool it configures itself
// (a small MaxConns) rather than testdb's shared, normally-sized one.
func seedStockAdjustmentFixture(t *testing.T, pool *pgxpool.Pool) stockAdjustmentFixture {
	t.Helper()
	q := db.New(pool)
	ctx := context.Background()

	shopRow, err := q.CreateShop(ctx, db.CreateShopParams{ID: uuid.New(), Slug: "stock-adj-shop-" + uuid.NewString(), Name: "Stock Adj Shop"})
	if err != nil {
		t.Fatalf("CreateShop: %v", err)
	}
	hash, err := auth.Hash("correct-horse-battery")
	if err != nil {
		t.Fatalf("auth.Hash: %v", err)
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
	if err := basePrice.Scan("125000.00"); err != nil {
		t.Fatalf("basePrice.Scan: %v", err)
	}
	product, err := q.CreateProduct(ctx, db.CreateProductParams{
		ID: uuid.New(), ShopID: shopRow.ID, UnitID: unit.ID, Slug: "stock-adj-product",
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
	srv := server{pool: pool, stock: stock.NewHandler(stockSvc)}

	managerCtx := auth.WithContext(context.Background(), auth.Context{
		ShopID: shopRow.ID, UserID: manager.ID, Role: db.UserRoleManager, SessionID: uuid.New(), Client: db.SessionClientWeb,
	})

	return stockAdjustmentFixture{
		srv: srv, pool: pool, q: q, shopID: shopRow.ID, managerCtx: managerCtx,
		variantID: variant.ID, locationID: loc.ID,
	}
}

func newStockAdjustmentFixture(t *testing.T) stockAdjustmentFixture {
	t.Helper()
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	return seedStockAdjustmentFixture(t, pool)
}

// newSmallPool opens a second pgxpool.Pool against the same (already
// migrated) test database testdb.New manages, capped at maxConns —
// testdb's own shared pool is not reused here because its size is not
// something a single test should change out from under every other test
// in the package. Closed automatically at the end of the test.
func newSmallPool(t *testing.T, maxConns int32) *pgxpool.Pool {
	t.Helper()
	testdb.New(t) // ensures the container is up and migrated first
	dsn := testdb.DSN(t)

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("newSmallPool: parse config: %v", err)
	}
	cfg.MaxConns = maxConns

	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("newSmallPool: open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func adjustmentReq(variantID, locationID uuid.UUID, qty string, reason gen.AdjustmentReason, note *string, key *string) gen.CreateStockAdjustmentRequestObject {
	return gen.CreateStockAdjustmentRequestObject{
		Params: gen.CreateStockAdjustmentParams{IdempotencyKey: key},
		Body: &gen.StockAdjustmentCreate{
			VariantId: variantID, LocationId: locationID, Qty: qty, Reason: reason, Note: note,
		},
	}
}

func TestCreateStockAdjustment_qtyZeroIs400(t *testing.T) {
	f := newStockAdjustmentFixture(t)
	_, err := f.srv.CreateStockAdjustment(f.managerCtx, adjustmentReq(f.variantID, f.locationID, "0.000", gen.CountCorrection, nil, nil))
	if err == nil {
		t.Fatal("want an error for qty 0, got none")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 400 {
		t.Fatalf("error = %v, want 400 VALIDATION_FAILED", err)
	}
	if apiErr.Details["fields"].(map[string]string)["qty"] != "invalid" {
		t.Fatalf("details = %v, want fields.qty=invalid", apiErr.Details)
	}
}

// TestCreateStockAdjustment_writesAuditRow also pins NIT 11's audit shape:
// before = {qty: <level before>}, after = {qty: <level after>, movement:
// <the movement>}.
func TestCreateStockAdjustment_writesAuditRow(t *testing.T) {
	f := newStockAdjustmentFixture(t)
	resp, err := f.srv.CreateStockAdjustment(f.managerCtx, adjustmentReq(f.variantID, f.locationID, "5.000", gen.Found, nil, nil))
	if err != nil {
		t.Fatalf("CreateStockAdjustment: %v", err)
	}
	if _, ok := resp.(rawJSONResponse); !ok {
		t.Fatalf("response type = %T, want rawJSONResponse", resp)
	}

	var mv gen.StockMovement
	if err := json.Unmarshal(resp.(rawJSONResponse).body, &mv); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}

	var count int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE shop_id = $1 AND entity_type = 'stock_movement' AND entity_id = $2 AND action = 'stock.adjust'`,
		f.shopID, mv.Id).Scan(&count); err != nil {
		t.Fatalf("count audit_log: %v", err)
	}
	if count != 1 {
		t.Fatalf("audit_log rows for the adjustment = %d, want 1", count)
	}

	var before, after []byte
	if err := f.pool.QueryRow(context.Background(),
		`SELECT before, after FROM audit_log WHERE shop_id = $1 AND entity_type = 'stock_movement' AND entity_id = $2 AND action = 'stock.adjust'`,
		f.shopID, mv.Id).Scan(&before, &after); err != nil {
		t.Fatalf("read audit_log before/after: %v", err)
	}

	var beforeVal struct {
		Qty string `json:"qty"`
	}
	if err := json.Unmarshal(before, &beforeVal); err != nil {
		t.Fatalf("unmarshal before: %v", err)
	}
	if beforeVal.Qty != "0.000" {
		t.Fatalf("before.qty = %q, want 0.000 (no opening stock)", beforeVal.Qty)
	}

	var afterVal struct {
		Qty      string `json:"qty"`
		Movement struct {
			ID string `json:"id"`
		} `json:"movement"`
	}
	if err := json.Unmarshal(after, &afterVal); err != nil {
		t.Fatalf("unmarshal after: %v", err)
	}
	if afterVal.Qty != "5.000" {
		t.Fatalf("after.qty = %q, want 5.000", afterVal.Qty)
	}
	if afterVal.Movement.ID != mv.Id.String() {
		t.Fatalf("after.movement.id = %q, want %s", afterVal.Movement.ID, mv.Id)
	}
}

func TestCreateStockAdjustment_insufficientReturns409WithDetails(t *testing.T) {
	f := newStockAdjustmentFixture(t)
	// No opening stock: any negative adjustment is insufficient (D-41,
	// allow_negative_stock defaults false).
	_, err := f.srv.CreateStockAdjustment(f.managerCtx, adjustmentReq(f.variantID, f.locationID, "-1.000", gen.Damaged, nil, nil))
	if err == nil {
		t.Fatal("want STOCK_INSUFFICIENT, got no error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 409 || apiErr.Code != gen.STOCKINSUFFICIENT {
		t.Fatalf("error = %v, want 409 STOCK_INSUFFICIENT", err)
	}
	if apiErr.Details["variantId"] != f.variantID.String() || apiErr.Details["locationId"] != f.locationID.String() {
		t.Fatalf("details = %v, want variantId/locationId = %s/%s", apiErr.Details, f.variantID, f.locationID)
	}
	if apiErr.Details["available"] != "0.000" {
		t.Fatalf("details.available = %v, want 0.000", apiErr.Details["available"])
	}
}

func TestCreateStockAdjustment_variantFromAnotherShop404(t *testing.T) {
	f := newStockAdjustmentFixture(t)
	otherVariant := uuid.New() // belongs to no shop at all, a fortiori not this one
	_, err := f.srv.CreateStockAdjustment(f.managerCtx, adjustmentReq(otherVariant, f.locationID, "1.000", gen.Found, nil, nil))
	if err == nil {
		t.Fatal("want a 404, got none")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 404 {
		t.Fatalf("error = %v, want 404 NOT_FOUND", err)
	}
}

func TestCreateStockAdjustment_idempotentReplayReturnsIdenticalBody(t *testing.T) {
	f := newStockAdjustmentFixture(t)
	key := "adj-key-1"

	resp1, err := f.srv.CreateStockAdjustment(f.managerCtx, adjustmentReq(f.variantID, f.locationID, "2.000", gen.Found, nil, &key))
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	resp2, err := f.srv.CreateStockAdjustment(f.managerCtx, adjustmentReq(f.variantID, f.locationID, "2.000", gen.Found, nil, &key))
	if err != nil {
		t.Fatalf("replay call: %v", err)
	}

	var mv1, mv2 gen.StockMovement
	if err := json.Unmarshal(resp1.(rawJSONResponse).body, &mv1); err != nil {
		t.Fatalf("unmarshal resp1: %v", err)
	}
	if err := json.Unmarshal(resp2.(rawJSONResponse).body, &mv2); err != nil {
		t.Fatalf("unmarshal resp2: %v", err)
	}
	if mv1.Id != mv2.Id {
		t.Fatalf("replay returned a different movement id: %s != %s", mv1.Id, mv2.Id)
	}

	var count int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM stock_movements WHERE shop_id = $1 AND variant_id = $2 AND location_id = $3`,
		f.shopID, f.variantID, f.locationID).Scan(&count); err != nil {
		t.Fatalf("count movements: %v", err)
	}
	if count != 1 {
		t.Fatalf("stock_movements rows = %d, want 1 (the replay must not have written a second one)", count)
	}
}

func TestCreateStockAdjustment_sameKeyDifferentBodyReturns409(t *testing.T) {
	f := newStockAdjustmentFixture(t)
	key := "adj-key-2"

	if _, err := f.srv.CreateStockAdjustment(f.managerCtx, adjustmentReq(f.variantID, f.locationID, "2.000", gen.Found, nil, &key)); err != nil {
		t.Fatalf("first call: %v", err)
	}

	_, err := f.srv.CreateStockAdjustment(f.managerCtx, adjustmentReq(f.variantID, f.locationID, "3.000", gen.Found, nil, &key))
	if err == nil {
		t.Fatal("want IDEMPOTENCY_KEY_REUSED, got no error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 409 || apiErr.Code != gen.IDEMPOTENCYKEYREUSED {
		t.Fatalf("error = %v, want 409 IDEMPOTENCY_KEY_REUSED", err)
	}
}

// TestCreateStockAdjustment_sameKeyDifferentActorReturns409 is MINOR 7's
// end-to-end test: two different managers of the same shop reusing the
// exact same client-chosen Idempotency-Key and body must not let the
// second one replay a response computed (and role-shaped) for the first —
// RequestHash folds the actor id in, so this is a hash mismatch like any
// other.
func TestCreateStockAdjustment_sameKeyDifferentActorReturns409(t *testing.T) {
	f := newStockAdjustmentFixture(t)
	key := "adj-key-shared"

	otherManager, err := f.q.CreateUser(context.Background(), db.CreateUserParams{
		ID: uuid.New(), ShopID: f.shopID, Username: "manager2", PasswordHash: "x",
		FullName: "Manager Two", Role: db.UserRoleManager, Locale: "uz",
	})
	if err != nil {
		t.Fatalf("CreateUser(manager2): %v", err)
	}
	otherManagerCtx := auth.WithContext(context.Background(), auth.Context{
		ShopID: f.shopID, UserID: otherManager.ID, Role: db.UserRoleManager, SessionID: uuid.New(), Client: db.SessionClientWeb,
	})

	if _, err := f.srv.CreateStockAdjustment(f.managerCtx, adjustmentReq(f.variantID, f.locationID, "2.000", gen.Found, nil, &key)); err != nil {
		t.Fatalf("first call (manager 1): %v", err)
	}

	_, err = f.srv.CreateStockAdjustment(otherManagerCtx, adjustmentReq(f.variantID, f.locationID, "2.000", gen.Found, nil, &key))
	if err == nil {
		t.Fatal("want IDEMPOTENCY_KEY_REUSED for a different actor reusing the key, got no error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 409 || apiErr.Code != gen.IDEMPOTENCYKEYREUSED {
		t.Fatalf("error = %v, want 409 IDEMPOTENCY_KEY_REUSED", err)
	}
}

// TestCreateStockAdjustment_idempotencyKeyTooLongIs400 is NIT 10's test:
// contracts/openapi.yaml bounds Idempotency-Key at 128 characters; the
// strict server does not enforce that JSON Schema constraint on a header
// parameter at runtime, so httpx.ValidateIdempotencyKey does.
func TestCreateStockAdjustment_idempotencyKeyTooLongIs400(t *testing.T) {
	f := newStockAdjustmentFixture(t)
	key := strings.Repeat("k", 129)

	_, err := f.srv.CreateStockAdjustment(f.managerCtx, adjustmentReq(f.variantID, f.locationID, "1.000", gen.Found, nil, &key))
	if err == nil {
		t.Fatal("want 400 VALIDATION_FAILED for a 129-character key, got no error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 400 {
		t.Fatalf("error = %v, want 400 VALIDATION_FAILED", err)
	}
}

// TestCreateStockAdjustment_concurrentDistinctKeysUnderSmallPoolCompletes
// is BLOCKER 1's own reproduction, run against the fix: 12 concurrent
// CreateStockAdjustment calls, each with its own Idempotency-Key (so none
// of them replay each other — every one must actually run Idempotent's
// full begin/lock/fn/store/commit sequence), against a pool capped at 4
// connections. Before the fix (fn opening a second, separate transaction
// while Idempotent's own transaction sat idle-in-transaction holding a
// connection), enough concurrent keyed requests against a small pool could
// deadlock the whole pool; this test would hang (and eventually fail on
// context deadline) on that version. It must complete well within the
// bounded wait below.
func TestCreateStockAdjustment_concurrentDistinctKeysUnderSmallPoolCompletes(t *testing.T) {
	const maxConns = 4
	const n = 12

	pool := newSmallPool(t, maxConns)
	testdb.Truncate(t, pool)
	f := seedStockAdjustmentFixture(t, pool)

	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("concurrent-key-%d", i)
			_, err := f.srv.CreateStockAdjustment(f.managerCtx, adjustmentReq(f.variantID, f.locationID, "1.000", gen.Found, nil, &key))
			errs[i] = err
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("12 concurrent Idempotency-Key'd adjustments against a 4-connection pool did not complete within 20s — the pool likely deadlocked")
	}

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}

	var count int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM stock_movements WHERE shop_id = $1 AND variant_id = $2 AND location_id = $3`,
		f.shopID, f.variantID, f.locationID).Scan(&count); err != nil {
		t.Fatalf("count movements: %v", err)
	}
	if count != n {
		t.Fatalf("stock_movements rows = %d, want %d (one per distinct key)", count, n)
	}
}
