package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

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

func newStockAdjustmentFixture(t *testing.T) stockAdjustmentFixture {
	t.Helper()
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	q := db.New(pool)
	ctx := context.Background()

	shopRow, err := q.CreateShop(ctx, db.CreateShopParams{ID: uuid.New(), Slug: "stock-adj-shop", Name: "Stock Adj Shop"})
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
