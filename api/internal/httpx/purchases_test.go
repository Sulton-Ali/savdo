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

// purchaseFixture wires a bare server{} (pool + *stock.Handler only, same
// idiom stockAdjustmentFixture uses) against a real Postgres, with one
// shop, one manager, one supplier, one location and one variant seeded
// directly through sqlc.
type purchaseFixture struct {
	srv        server
	pool       *pgxpool.Pool
	q          *db.Queries
	shopID     uuid.UUID
	managerCtx context.Context
	supplierID uuid.UUID
	locationID uuid.UUID
	variantID  uuid.UUID
}

func newPurchaseFixture(t *testing.T) purchaseFixture {
	t.Helper()
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	q := db.New(pool)
	ctx := context.Background()

	shopRow, err := q.CreateShop(ctx, db.CreateShopParams{ID: uuid.New(), Slug: "purchase-idem-" + uuid.NewString(), Name: "Purchase Idem Shop"})
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
	supplier, err := q.CreateSupplier(ctx, db.CreateSupplierParams{ID: uuid.New(), ShopID: shopRow.ID, Name: "Idem Supplier"})
	if err != nil {
		t.Fatalf("CreateSupplier: %v", err)
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
		ID: uuid.New(), ShopID: shopRow.ID, UnitID: unit.ID, Slug: "purchase-idem-product",
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

	return purchaseFixture{
		srv: srv, pool: pool, q: q, shopID: shopRow.ID, managerCtx: managerCtx,
		supplierID: supplier.ID, locationID: loc.ID, variantID: variant.ID,
	}
}

// createDraftPurchase creates a one-item draft purchase through the real
// handler (not a direct sqlc insert), so its number/id are exactly what a
// client would see.
func (f purchaseFixture) createDraftPurchase(t *testing.T, qty, unitCost string) gen.Purchase {
	t.Helper()
	resp, err := f.srv.stock.CreatePurchase(f.managerCtx, gen.CreatePurchaseRequestObject{Body: &gen.PurchaseCreate{
		SupplierId: f.supplierID, LocationId: f.locationID,
		Items: []gen.PurchaseItemCreate{{VariantId: f.variantID, Qty: qty, UnitCost: unitCost}},
	}})
	if err != nil {
		t.Fatalf("CreatePurchase: %v", err)
	}
	created, ok := resp.(gen.CreatePurchase201JSONResponse)
	if !ok {
		t.Fatalf("CreatePurchase response type = %T", resp)
	}
	return gen.Purchase(created)
}

func receivePurchaseReq(id uuid.UUID, key *string) gen.ReceivePurchaseRequestObject {
	return gen.ReceivePurchaseRequestObject{Id: id, Params: gen.ReceivePurchaseParams{IdempotencyKey: key}}
}

func TestReceivePurchase_idempotentReplayReturnsIdenticalBodyNoSecondMovement(t *testing.T) {
	f := newPurchaseFixture(t)
	purchase := f.createDraftPurchase(t, "6.000", "40.00")
	key := "receive-key-1"

	resp1, err := f.srv.ReceivePurchase(f.managerCtx, receivePurchaseReq(purchase.Id, &key))
	if err != nil {
		t.Fatalf("first receive: %v", err)
	}
	resp2, err := f.srv.ReceivePurchase(f.managerCtx, receivePurchaseReq(purchase.Id, &key))
	if err != nil {
		t.Fatalf("replay receive: %v", err)
	}

	var p1, p2 gen.Purchase
	if err := json.Unmarshal(resp1.(rawJSONResponse).body, &p1); err != nil {
		t.Fatalf("unmarshal resp1: %v", err)
	}
	if err := json.Unmarshal(resp2.(rawJSONResponse).body, &p2); err != nil {
		t.Fatalf("unmarshal resp2: %v", err)
	}
	if p1.Id != p2.Id || p1.Status != p2.Status || p1.TotalCost != p2.TotalCost {
		t.Fatalf("replay body differs: %+v != %+v", p1, p2)
	}
	if p1.Status != gen.Received {
		t.Fatalf("Status = %q, want received", p1.Status)
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

func TestReceivePurchase_sameKeyDifferentPurchaseReturns409(t *testing.T) {
	f := newPurchaseFixture(t)
	purchaseA := f.createDraftPurchase(t, "1.000", "10.00")
	purchaseB := f.createDraftPurchase(t, "2.000", "20.00")
	key := "receive-key-shared"

	if _, err := f.srv.ReceivePurchase(f.managerCtx, receivePurchaseReq(purchaseA.Id, &key)); err != nil {
		t.Fatalf("first receive (purchase A): %v", err)
	}

	_, err := f.srv.ReceivePurchase(f.managerCtx, receivePurchaseReq(purchaseB.Id, &key))
	if err == nil {
		t.Fatal("want IDEMPOTENCY_KEY_REUSED for a different purchase reusing the key, got no error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 409 || apiErr.Code != gen.IDEMPOTENCYKEYREUSED {
		t.Fatalf("error = %v, want 409 IDEMPOTENCY_KEY_REUSED", err)
	}

	// Purchase B must still be draft — the reused key must never have run
	// its receive.
	fresh, err := f.q.GetPurchase(context.Background(), db.GetPurchaseParams{ShopID: f.shopID, ID: purchaseB.Id})
	if err != nil {
		t.Fatalf("GetPurchase(B): %v", err)
	}
	if fresh.Status != db.PurchaseStatusDraft {
		t.Fatalf("purchase B status = %q, want draft (untouched)", fresh.Status)
	}
}

func TestReceivePurchase_cashierForbidden(t *testing.T) {
	f := newPurchaseFixture(t)
	purchase := f.createDraftPurchase(t, "1.000", "10.00")

	cashier, err := f.q.CreateUser(context.Background(), db.CreateUserParams{
		ID: uuid.New(), ShopID: f.shopID, Username: "cashier1", PasswordHash: "x",
		FullName: "Cashier One", Role: db.UserRoleCashier, Locale: "uz",
	})
	if err != nil {
		t.Fatalf("CreateUser(cashier): %v", err)
	}
	cashierCtx := auth.WithContext(context.Background(), auth.Context{
		ShopID: f.shopID, UserID: cashier.ID, Role: db.UserRoleCashier, SessionID: uuid.New(), Client: db.SessionClientWeb,
	})

	_, err = f.srv.ReceivePurchase(cashierCtx, receivePurchaseReq(purchase.Id, nil))
	if err == nil {
		t.Fatal("want 403 FORBIDDEN, got no error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 403 {
		t.Fatalf("error = %v, want 403 FORBIDDEN", err)
	}
}
