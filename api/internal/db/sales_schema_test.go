package db_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

// salesCashier creates a cashier (or any role) user for the sales tests
// below.
func salesUser(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, username string, role db.UserRole) db.User {
	t.Helper()
	u, err := q.CreateUser(ctx, db.CreateUserParams{
		ID: uuid.New(), ShopID: shopID, Username: username, PasswordHash: "hash",
		FullName: strings.ToUpper(username[:1]) + username[1:], Role: role, Locale: "uz",
	})
	if err != nil {
		t.Fatalf("CreateUser(%q): %v", username, err)
	}
	return u
}

// salesCustomer creates a customer for the sales tests below.
func salesCustomer(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, fullName string) db.Customer {
	t.Helper()
	c, err := q.CreateCustomer(ctx, db.CreateCustomerParams{ID: uuid.New(), ShopID: shopID, FullName: fullName})
	if err != nil {
		t.Fatalf("CreateCustomer(%q): %v", fullName, err)
	}
	return c
}

// saleItemSpec is one line for newSale below; amounts are decimal literals
// the caller has already worked out by hand (never float, § 04-DATA-MODEL.md
// rule 3), so line_total/subtotal/total always agree exactly with the
// sales table's CHECK (total = subtotal - discount_amount).
type saleItemSpec struct {
	variantID          uuid.UUID
	qty                string
	unitPrice          string
	unitCost           string
	lineTotal          string
	originalSaleItemID *uuid.UUID
}

// newSale claims the next sale number and writes a complete sale (header +
// items), the way sales.Service will: NextSaleNumber under row lock, then
// InsertSale, then one InsertSaleItem per line. No payment is attached
// here — call newPayment separately, the tests that do not need one don't
// pay the setup cost.
func newSale(
	ctx context.Context, t *testing.T, q *db.Queries,
	shopID, locationID, cashierID uuid.UUID, customerID *uuid.UUID,
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
		LocationID: locationID, CustomerID: customerID, CashierID: cashierID,
		OriginalSaleID: originalSaleID,
		Subtotal:       numeric(t, subtotal), DiscountAmount: numeric(t, discount), Total: numeric(t, total),
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

func newPayment(ctx context.Context, t *testing.T, q *db.Queries, shopID, saleID uuid.UUID, method db.PaymentMethod, amount string) db.SalePayment {
	t.Helper()
	p, err := q.InsertSalePayment(ctx, db.InsertSalePaymentParams{
		ID: uuid.New(), ShopID: shopID, SaleID: saleID, Method: method, Amount: numeric(t, amount),
	})
	if err != nil {
		t.Fatalf("InsertSalePayment: %v", err)
	}
	return p
}

// salesFixture is shared setup: a shop, a location, a cashier and one
// active variant, for tests that only need a minimal sale to exist.
type salesFixture struct {
	pool                                     *pgxpool.Pool
	q                                        *db.Queries
	shopID, locationID, cashierID, variantID uuid.UUID
}

func newSalesFixture(t *testing.T, shopSlug string) salesFixture {
	t.Helper()
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, shopSlug)
	unit := catalogUnit(ctx, t, q, shop.ID, "pcs")
	product := catalogProduct(ctx, t, q, shop.ID, unit.ID, "hoodie")
	variant := stockVariant(ctx, t, q, shop.ID, product.ID, "{}")
	loc := stockLocation(ctx, t, q, shop.ID, "Main")
	cashier := salesUser(ctx, t, q, shop.ID, "cashier1", db.UserRoleCashier)

	return salesFixture{pool: pool, q: q, shopID: shop.ID, locationID: loc.ID, cashierID: cashier.ID, variantID: variant.ID}
}

func TestSalesImmutable_updatingTotalOnCompletedSaleRejected(t *testing.T) {
	f := newSalesFixture(t, "shop-sales-immutable-total")
	ctx := context.Background()

	sale, _ := newSale(ctx, t, f.q, f.shopID, f.locationID, f.cashierID, nil, db.SaleKindSale, nil,
		"100.00", "0.00", "100.00",
		[]saleItemSpec{{variantID: f.variantID, qty: "1.000", unitPrice: "100.00", unitCost: "50.00", lineTotal: "100.00"}})

	_, err := f.pool.Exec(ctx, `UPDATE sales SET total = total + 1 WHERE id = $1`, sale.ID)
	if err == nil {
		t.Fatal("want updating total on a completed sale to be rejected by the immutability trigger, got no error")
	}
	if !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("want an immutability trigger error, got: %v", err)
	}
}

func TestSalesImmutable_voidTransitionOnceThenRejected(t *testing.T) {
	f := newSalesFixture(t, "shop-sales-immutable-void")
	ctx := context.Background()
	owner := salesUser(ctx, t, f.q, f.shopID, "owner1", db.UserRoleOwner)

	sale, _ := newSale(ctx, t, f.q, f.shopID, f.locationID, f.cashierID, nil, db.SaleKindSale, nil,
		"100.00", "0.00", "100.00",
		[]saleItemSpec{{variantID: f.variantID, qty: "1.000", unitPrice: "100.00", unitCost: "50.00", lineTotal: "100.00"}})

	// First void: only status/voided_at/voided_by/void_reason change — allowed.
	_, err := f.pool.Exec(ctx, `UPDATE sales SET status = 'voided', voided_at = now(), voided_by = $1, void_reason = $2 WHERE id = $3`,
		owner.ID, "customer changed mind", sale.ID)
	if err != nil {
		t.Fatalf("want the first completed -> voided transition to succeed, got: %v", err)
	}

	// Second void: OLD.status is now 'voided', not 'completed' — rejected.
	_, err = f.pool.Exec(ctx, `UPDATE sales SET status = 'voided', voided_at = now(), voided_by = $1, void_reason = $2 WHERE id = $3`,
		owner.ID, "again", sale.ID)
	if err == nil {
		t.Fatal("want a second void to be rejected by the immutability trigger, got no error")
	}
	if !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("want an immutability trigger error, got: %v", err)
	}
}

func TestSalesImmutable_deleteAlwaysRejected(t *testing.T) {
	f := newSalesFixture(t, "shop-sales-immutable-delete")
	ctx := context.Background()

	sale, _ := newSale(ctx, t, f.q, f.shopID, f.locationID, f.cashierID, nil, db.SaleKindSale, nil,
		"100.00", "0.00", "100.00",
		[]saleItemSpec{{variantID: f.variantID, qty: "1.000", unitPrice: "100.00", unitCost: "50.00", lineTotal: "100.00"}})

	_, err := f.pool.Exec(ctx, `DELETE FROM sales WHERE id = $1`, sale.ID)
	if err == nil {
		t.Fatal("want DELETE on sales to be rejected by the immutability trigger, got no error")
	}
	if !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("want an immutability trigger error, got: %v", err)
	}
}

func TestVoidSale_onlyFromCompleted(t *testing.T) {
	f := newSalesFixture(t, "shop-sales-void-query")
	ctx := context.Background()
	owner := salesUser(ctx, t, f.q, f.shopID, "owner1", db.UserRoleOwner)

	sale, _ := newSale(ctx, t, f.q, f.shopID, f.locationID, f.cashierID, nil, db.SaleKindSale, nil,
		"100.00", "0.00", "100.00",
		[]saleItemSpec{{variantID: f.variantID, qty: "1.000", unitPrice: "100.00", unitCost: "50.00", lineTotal: "100.00"}})

	reason := "wrong size"
	voided, err := f.q.VoidSale(ctx, db.VoidSaleParams{ShopID: f.shopID, ID: sale.ID, VoidedBy: &owner.ID, VoidReason: &reason})
	if err != nil {
		t.Fatalf("want VoidSale to affect a completed sale, got: %v", err)
	}
	if voided.Status != db.SaleStatusVoided {
		t.Fatalf("want status voided, got %s", voided.Status)
	}

	if _, err := f.q.VoidSale(ctx, db.VoidSaleParams{ShopID: f.shopID, ID: sale.ID, VoidedBy: &owner.ID, VoidReason: &reason}); err != pgx.ErrNoRows {
		t.Fatalf("want pgx.ErrNoRows voiding an already-voided sale, got: %v", err)
	}
}

// TestVoidSale_refusesReturnKind: D-59 voids apply to sales, not returns —
// a return is corrected by a further return, not a void. VoidSale's WHERE
// clause carries `AND kind = 'sale'`, so a return-kind row is left
// completed with 0 rows affected, exactly like an already-voided or
// nonexistent sale.
func TestVoidSale_refusesReturnKind(t *testing.T) {
	f := newSalesFixture(t, "shop-sales-void-return-kind")
	ctx := context.Background()
	owner := salesUser(ctx, t, f.q, f.shopID, "owner1", db.UserRoleOwner)

	original, items := newSale(ctx, t, f.q, f.shopID, f.locationID, f.cashierID, nil, db.SaleKindSale, nil,
		"100.00", "0.00", "100.00",
		[]saleItemSpec{{variantID: f.variantID, qty: "1.000", unitPrice: "100.00", unitCost: "50.00", lineTotal: "100.00"}})
	ret, _ := newSale(ctx, t, f.q, f.shopID, f.locationID, f.cashierID, nil, db.SaleKindReturn, &original.ID,
		"100.00", "0.00", "100.00",
		[]saleItemSpec{{variantID: f.variantID, qty: "1.000", unitPrice: "100.00", unitCost: "50.00", lineTotal: "100.00", originalSaleItemID: &items[0].ID}})

	reason := "should not apply"
	if _, err := f.q.VoidSale(ctx, db.VoidSaleParams{ShopID: f.shopID, ID: ret.ID, VoidedBy: &owner.ID, VoidReason: &reason}); err != pgx.ErrNoRows {
		t.Fatalf("want pgx.ErrNoRows voiding a return-kind row, got: %v", err)
	}

	stillCompleted, err := f.q.GetSaleForUpdate(ctx, db.GetSaleForUpdateParams{ShopID: f.shopID, ID: ret.ID})
	if err != nil {
		t.Fatalf("GetSaleForUpdate: %v", err)
	}
	if stillCompleted.Status != db.SaleStatusCompleted {
		t.Fatalf("want the return to remain completed after a refused void, got %s", stillCompleted.Status)
	}
}

// TestSales_voidColumnsMustMatchStatus proves the table-level CHECK (item
// 3, ADR-014): the sales_immutable trigger alone only restricts which
// columns an UPDATE may change, it does not require the void columns to
// actually be populated when status becomes 'voided' — that is this
// CHECK's job, both on UPDATE and on a direct INSERT.
func TestSales_voidColumnsMustMatchStatus(t *testing.T) {
	f := newSalesFixture(t, "shop-sales-void-columns-check")
	ctx := context.Background()

	sale, _ := newSale(ctx, t, f.q, f.shopID, f.locationID, f.cashierID, nil, db.SaleKindSale, nil,
		"100.00", "0.00", "100.00",
		[]saleItemSpec{{variantID: f.variantID, qty: "1.000", unitPrice: "100.00", unitCost: "50.00", lineTotal: "100.00"}})

	// The trigger allows this column change (only status differs), but the
	// CHECK must still reject it: voided_at/voided_by stay NULL.
	if _, err := f.pool.Exec(ctx, `UPDATE sales SET status = 'voided' WHERE id = $1`, sale.ID); err == nil {
		t.Fatal("want UPDATE ... SET status = 'voided' alone (no voided_at/voided_by) to be rejected by the CHECK, got no error")
	}

	// A direct INSERT of an already-voided row with no voided_at/voided_by:
	// also rejected. INSERT never runs the sales_immutable trigger (it only
	// fires BEFORE UPDATE OR DELETE), so this exercises the CHECK alone.
	num, err := f.q.NextSaleNumber(ctx, f.shopID)
	if err != nil {
		t.Fatalf("NextSaleNumber: %v", err)
	}
	_, err = f.pool.Exec(ctx, `
		INSERT INTO sales (id, shop_id, number, kind, status, location_id, cashier_id, subtotal, discount_amount, total)
		VALUES ($1, $2, $3, 'sale', 'voided', $4, $5, 100.00, 0.00, 100.00)
	`, uuid.New(), f.shopID, num, f.locationID, f.cashierID)
	if err == nil {
		t.Fatal("want inserting a status='voided' row with no voided_at/voided_by to be rejected by the CHECK, got no error")
	}
}

func TestSaleItems_appendOnlyRejectsUpdateAndDelete(t *testing.T) {
	f := newSalesFixture(t, "shop-sale-items-immutable")
	ctx := context.Background()

	_, items := newSale(ctx, t, f.q, f.shopID, f.locationID, f.cashierID, nil, db.SaleKindSale, nil,
		"100.00", "0.00", "100.00",
		[]saleItemSpec{{variantID: f.variantID, qty: "1.000", unitPrice: "100.00", unitCost: "50.00", lineTotal: "100.00"}})
	item := items[0]

	if _, err := f.pool.Exec(ctx, `UPDATE sale_items SET qty = qty + 1 WHERE id = $1`, item.ID); err == nil {
		t.Fatal("want UPDATE on sale_items to be rejected by the append-only trigger, got no error")
	} else if !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("want an append-only trigger error, got: %v", err)
	}

	if _, err := f.pool.Exec(ctx, `DELETE FROM sale_items WHERE id = $1`, item.ID); err == nil {
		t.Fatal("want DELETE on sale_items to be rejected by the append-only trigger, got no error")
	} else if !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("want an append-only trigger error, got: %v", err)
	}
}

func TestSalePayments_appendOnlyRejectsUpdateAndDeleteAndIsUniquePerSale(t *testing.T) {
	f := newSalesFixture(t, "shop-sale-payments-immutable")
	ctx := context.Background()

	sale, _ := newSale(ctx, t, f.q, f.shopID, f.locationID, f.cashierID, nil, db.SaleKindSale, nil,
		"100.00", "0.00", "100.00",
		[]saleItemSpec{{variantID: f.variantID, qty: "1.000", unitPrice: "100.00", unitCost: "50.00", lineTotal: "100.00"}})
	payment := newPayment(ctx, t, f.q, f.shopID, sale.ID, db.PaymentMethodCash, "100.00")

	// D-54: one payment per sale in MVP.
	if _, err := f.q.InsertSalePayment(ctx, db.InsertSalePaymentParams{
		ID: uuid.New(), ShopID: f.shopID, SaleID: sale.ID, Method: db.PaymentMethodCard, Amount: numeric(t, "0.00"),
	}); err == nil {
		t.Fatal("want a second payment on the same sale to be rejected by unique(sale_id), got none")
	}

	if _, err := f.pool.Exec(ctx, `UPDATE sale_payments SET amount = amount + 1 WHERE id = $1`, payment.ID); err == nil {
		t.Fatal("want UPDATE on sale_payments to be rejected by the append-only trigger, got no error")
	} else if !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("want an append-only trigger error, got: %v", err)
	}

	if _, err := f.pool.Exec(ctx, `DELETE FROM sale_payments WHERE id = $1`, payment.ID); err == nil {
		t.Fatal("want DELETE on sale_payments to be rejected by the append-only trigger, got no error")
	} else if !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("want an append-only trigger error, got: %v", err)
	}
}

func TestSales_returnRequiresOriginalSaleIdAndSaleMustNotHaveOne(t *testing.T) {
	f := newSalesFixture(t, "shop-sales-return-check")
	ctx := context.Background()

	num, err := f.q.NextSaleNumber(ctx, f.shopID)
	if err != nil {
		t.Fatalf("NextSaleNumber: %v", err)
	}
	// A return with no original_sale_id: rejected by the CHECK.
	if _, err := f.q.InsertSale(ctx, db.InsertSaleParams{
		ID: uuid.New(), ShopID: f.shopID, Number: num, Kind: db.SaleKindReturn,
		LocationID: f.locationID, CashierID: f.cashierID,
		Subtotal: numeric(t, "10.00"), DiscountAmount: numeric(t, "0.00"), Total: numeric(t, "10.00"),
	}); err == nil {
		t.Fatal("want a check-constraint error inserting a return with no original_sale_id, got none")
	}

	original, _ := newSale(ctx, t, f.q, f.shopID, f.locationID, f.cashierID, nil, db.SaleKindSale, nil,
		"100.00", "0.00", "100.00",
		[]saleItemSpec{{variantID: f.variantID, qty: "1.000", unitPrice: "100.00", unitCost: "50.00", lineTotal: "100.00"}})

	num2, err := f.q.NextSaleNumber(ctx, f.shopID)
	if err != nil {
		t.Fatalf("NextSaleNumber: %v", err)
	}
	// A sale (not a return) with original_sale_id set: also rejected.
	if _, err := f.q.InsertSale(ctx, db.InsertSaleParams{
		ID: uuid.New(), ShopID: f.shopID, Number: num2, Kind: db.SaleKindSale,
		LocationID: f.locationID, CashierID: f.cashierID, OriginalSaleID: &original.ID,
		Subtotal: numeric(t, "10.00"), DiscountAmount: numeric(t, "0.00"), Total: numeric(t, "10.00"),
	}); err == nil {
		t.Fatal("want a check-constraint error inserting a sale with an original_sale_id set, got none")
	}

	// A properly-shaped return: accepted.
	num3, err := f.q.NextSaleNumber(ctx, f.shopID)
	if err != nil {
		t.Fatalf("NextSaleNumber: %v", err)
	}
	if _, err := f.q.InsertSale(ctx, db.InsertSaleParams{
		ID: uuid.New(), ShopID: f.shopID, Number: num3, Kind: db.SaleKindReturn,
		LocationID: f.locationID, CashierID: f.cashierID, OriginalSaleID: &original.ID,
		Subtotal: numeric(t, "100.00"), DiscountAmount: numeric(t, "0.00"), Total: numeric(t, "100.00"),
	}); err != nil {
		t.Fatalf("want a well-formed return to succeed, got: %v", err)
	}
}

func TestSaleItems_qtyMustBePositive(t *testing.T) {
	f := newSalesFixture(t, "shop-sale-items-qty")
	ctx := context.Background()

	sale, _ := newSale(ctx, t, f.q, f.shopID, f.locationID, f.cashierID, nil, db.SaleKindSale, nil,
		"0.00", "0.00", "0.00", nil)

	newItem := func(qty string) error {
		_, err := f.q.InsertSaleItem(ctx, db.InsertSaleItemParams{
			ID: uuid.New(), ShopID: f.shopID, SaleID: sale.ID, VariantID: f.variantID,
			Qty: numeric(t, qty), UnitPrice: numeric(t, "100.00"), UnitCost: numeric(t, "50.00"), LineTotal: numeric(t, "100.00"),
		})
		return err
	}

	if err := newItem("0.000"); err == nil {
		t.Error("want a check-constraint error for qty = 0, got none")
	}
	if err := newItem("-1.000"); err == nil {
		t.Error("want a check-constraint error for a negative qty, got none")
	}
	if err := newItem("1.000"); err != nil {
		t.Errorf("want a positive qty to succeed, got: %v", err)
	}
}

func TestSales_subtotalDiscountTotalChecks(t *testing.T) {
	f := newSalesFixture(t, "shop-sales-amount-checks")
	ctx := context.Background()

	newHeader := func(subtotal, discount, total string) error {
		num, err := f.q.NextSaleNumber(ctx, f.shopID)
		if err != nil {
			t.Fatalf("NextSaleNumber: %v", err)
		}
		_, err = f.q.InsertSale(ctx, db.InsertSaleParams{
			ID: uuid.New(), ShopID: f.shopID, Number: num, Kind: db.SaleKindSale,
			LocationID: f.locationID, CashierID: f.cashierID,
			Subtotal: numeric(t, subtotal), DiscountAmount: numeric(t, discount), Total: numeric(t, total),
		})
		return err
	}

	if err := newHeader("100.00", "0.00", "90.00"); err == nil {
		t.Error("want a check-constraint error when total != subtotal - discount_amount, got none")
	}
	if err := newHeader("100.00", "150.00", "-50.00"); err == nil {
		t.Error("want a check-constraint error when discount_amount > subtotal, got none")
	}
	if err := newHeader("100.00", "10.00", "90.00"); err != nil {
		t.Errorf("want a consistent subtotal/discount/total to succeed, got: %v", err)
	}
}

func TestNextSaleNumber_gapFreeUnderConcurrencyWithBarrier(t *testing.T) {
	f := newSalesFixture(t, "shop-sale-number-concurrency")
	ctx := context.Background()

	const n = 10
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]int64, n)
	errs := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			<-start // barrier: every goroutine blocks here until released together
			tx, err := f.pool.Begin(ctx)
			if err != nil {
				errs[i] = err
				return
			}
			defer func() { _ = tx.Rollback(ctx) }() // no-op once committed
			qtx := db.New(tx)
			num, err := qtx.NextSaleNumber(ctx, f.shopID)
			if err != nil {
				errs[i] = err
				return
			}
			if err := tx.Commit(ctx); err != nil {
				errs[i] = err
				return
			}
			results[i] = num
		}(i)
	}
	close(start) // release every goroutine at once, forcing real contention on the row lock

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("NextSaleNumber goroutine %d: %v", i, err)
		}
	}

	sorted := append([]int64(nil), results...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	for i, got := range sorted {
		want := int64(1 + i) // shops.next_sale_number defaults to 1.
		if got != want {
			t.Fatalf("want %d consecutive numbers starting at 1 with no gaps or duplicates, got %v", n, sorted)
		}
	}
}

// TestGetSaleItemsForUpdate_secondCallerBlocksUntilFirstCommits proves the
// return-lock path GetSaleItemsForUpdate exists for: transaction A locks
// the original sale's items and inserts a return against them (the real
// return flow, uncommitted); transaction B, calling GetSaleItemsForUpdate
// for the same sale, must block on the same row lock until A commits —
// and once unblocked, must see A's now-committed return. Without the
// lock, two concurrent partial returns could both read "0 already
// returned" and jointly over-refund past what was sold (D-58).
//
// This does not compare wall-clock timestamps taken in different
// goroutines (that comparison is flaky under CPU load: two independent
// time.Now() calls a few dozen microseconds apart can land in either
// order regardless of which happened "first" from Postgres's point of
// view). Instead it proves blocking behaviourally: (1) B is still not
// done a bounded, generous interval after A has issued its uncommitted
// insert — bDone must NOT fire before that timeout; (2) once A commits,
// B completes within a separate, generous timeout and its own
// CountCompletedReturnsForSale call sees A's committed return. The
// ordering that makes step (1) meaningful — B's blocking call must
// actually have reached Postgres before A commits — is guaranteed by a
// handshake: B signals bAboutToCall right before invoking
// GetSaleItemsForUpdate, and the main goroutine waits to receive that
// signal (not a fixed sleep) before proceeding to the "still blocked"
// check and then A's commit.
func TestGetSaleItemsForUpdate_secondCallerBlocksUntilFirstCommits(t *testing.T) {
	f := newSalesFixture(t, "shop-sales-return-lock")
	ctx := context.Background()

	original, items := newSale(ctx, t, f.q, f.shopID, f.locationID, f.cashierID, nil, db.SaleKindSale, nil,
		"100.00", "0.00", "100.00",
		[]saleItemSpec{{variantID: f.variantID, qty: "2.000", unitPrice: "50.00", unitCost: "20.00", lineTotal: "100.00"}})

	txA, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin (A): %v", err)
	}
	qA := db.New(txA)
	if _, err := qA.GetSaleItemsForUpdate(ctx, db.GetSaleItemsForUpdateParams{ShopID: f.shopID, SaleID: original.ID}); err != nil {
		t.Fatalf("GetSaleItemsForUpdate (A): %v", err)
	}
	retNum, err := qA.NextSaleNumber(ctx, f.shopID)
	if err != nil {
		t.Fatalf("NextSaleNumber (A): %v", err)
	}
	ret, err := qA.InsertSale(ctx, db.InsertSaleParams{
		ID: uuid.New(), ShopID: f.shopID, Number: retNum, Kind: db.SaleKindReturn,
		LocationID: f.locationID, CashierID: f.cashierID, OriginalSaleID: &original.ID,
		Subtotal: numeric(t, "50.00"), DiscountAmount: numeric(t, "0.00"), Total: numeric(t, "50.00"),
	})
	if err != nil {
		t.Fatalf("InsertSale (A, return): %v", err)
	}
	if _, err := qA.InsertSaleItem(ctx, db.InsertSaleItemParams{
		ID: uuid.New(), ShopID: f.shopID, SaleID: ret.ID, VariantID: f.variantID,
		Qty: numeric(t, "1.000"), UnitPrice: numeric(t, "50.00"), UnitCost: numeric(t, "20.00"),
		LineTotal: numeric(t, "50.00"), OriginalSaleItemID: &items[0].ID,
	}); err != nil {
		t.Fatalf("InsertSaleItem (A, return): %v", err)
	}
	// A now holds the row lock on original's sale_items and has an
	// uncommitted return referencing them.

	bAboutToCall := make(chan struct{})
	bDone := make(chan error, 1)
	go func() {
		txB, err := f.pool.Begin(ctx)
		if err != nil {
			bDone <- err
			return
		}
		defer func() { _ = txB.Rollback(ctx) }() // no-op once committed
		qB := db.New(txB)

		close(bAboutToCall) // signals: about to issue the blocking call
		if _, err := qB.GetSaleItemsForUpdate(ctx, db.GetSaleItemsForUpdateParams{ShopID: f.shopID, SaleID: original.ID}); err != nil {
			bDone <- err
			return
		}

		// B must see A's committed return when it computes "already
		// returned" — proving no lost-update race between the two.
		count, err := qB.CountCompletedReturnsForSale(ctx, db.CountCompletedReturnsForSaleParams{ShopID: f.shopID, OriginalSaleID: &original.ID})
		if err != nil {
			bDone <- err
			return
		}
		if count != 1 {
			bDone <- fmt.Errorf("want B to see A's committed return once unblocked, count=%d", count)
			return
		}
		if err := txB.Commit(ctx); err != nil {
			bDone <- err
			return
		}
		bDone <- nil
	}()

	// Wait for B to actually reach the point of calling
	// GetSaleItemsForUpdate before checking that it is blocked. This is
	// an ordering guarantee (a channel receive), not a timing guess: it
	// only proves B has *started* its call, not that it has reached
	// Postgres and begun waiting on the row lock, so the "still blocked"
	// check below also needs a bounded wait of its own.
	<-bAboutToCall

	// B must still be blocked a good while after it began its call and
	// while A's return is still uncommitted. This does not prove B will
	// never finish without A's commit (that would require an infinite
	// wait), only that it does not finish within a generous window —
	// which is the behavioural signature of being blocked on the row
	// lock rather than racing through.
	select {
	case err := <-bDone:
		t.Fatalf("want B's GetSaleItemsForUpdate to still be blocked while A's return is uncommitted, but B finished (err=%v)", err)
	case <-time.After(300 * time.Millisecond):
		// expected: B is still blocked.
	}

	if err := txA.Commit(ctx); err != nil {
		t.Fatalf("Commit (A): %v", err)
	}

	select {
	case err := <-bDone:
		if err != nil {
			t.Fatalf("transaction B: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for transaction B — GetSaleItemsForUpdate did not unblock after A's commit")
	}
}

func TestGetSaleForStaffAndCashier_joinsAndHasReturns(t *testing.T) {
	f := newSalesFixture(t, "shop-sales-get")
	ctx := context.Background()
	customer := salesCustomer(ctx, t, f.q, f.shopID, "Nodira Karimova")

	sale, items := newSale(ctx, t, f.q, f.shopID, f.locationID, f.cashierID, &customer.ID, db.SaleKindSale, nil,
		"100.00", "0.00", "100.00",
		[]saleItemSpec{{variantID: f.variantID, qty: "1.000", unitPrice: "100.00", unitCost: "50.00", lineTotal: "100.00"}})
	newPayment(ctx, t, f.q, f.shopID, sale.ID, db.PaymentMethodCash, "100.00")

	staffRow, err := f.q.GetSaleForStaff(ctx, db.GetSaleForStaffParams{ShopID: f.shopID, ID: sale.ID})
	if err != nil {
		t.Fatalf("GetSaleForStaff: %v", err)
	}
	if staffRow.LocationName != "Main" {
		t.Errorf("want location_name Main, got %q", staffRow.LocationName)
	}
	if staffRow.CustomerName == nil || *staffRow.CustomerName != "Nodira Karimova" {
		t.Errorf("want customer_name Nodira Karimova, got %v", staffRow.CustomerName)
	}
	if staffRow.PaymentMethod == nil || *staffRow.PaymentMethod != db.PaymentMethodCash {
		t.Errorf("want payment_method cash, got %v", staffRow.PaymentMethod)
	}
	if staffRow.HasReturns {
		t.Error("want has_returns = false before any return exists")
	}

	cashierRow, err := f.q.GetSaleForCashier(ctx, db.GetSaleForCashierParams{ShopID: f.shopID, ID: sale.ID})
	if err != nil {
		t.Fatalf("GetSaleForCashier: %v", err)
	}
	if cashierRow.CashierName != staffRow.CashierName {
		t.Errorf("want the same cashier_name from both queries, got staff=%q cashier=%q", staffRow.CashierName, cashierRow.CashierName)
	}

	// A completed return referencing this sale flips has_returns to true.
	newSale(ctx, t, f.q, f.shopID, f.locationID, f.cashierID, &customer.ID, db.SaleKindReturn, &sale.ID,
		"100.00", "0.00", "100.00",
		[]saleItemSpec{{variantID: f.variantID, qty: "1.000", unitPrice: "100.00", unitCost: "50.00", lineTotal: "100.00", originalSaleItemID: &items[0].ID}})

	staffRow2, err := f.q.GetSaleForStaff(ctx, db.GetSaleForStaffParams{ShopID: f.shopID, ID: sale.ID})
	if err != nil {
		t.Fatalf("GetSaleForStaff (after return): %v", err)
	}
	if !staffRow2.HasReturns {
		t.Error("want has_returns = true once a completed return references this sale")
	}

	count, err := f.q.CountCompletedReturnsForSale(ctx, db.CountCompletedReturnsForSaleParams{ShopID: f.shopID, OriginalSaleID: &sale.ID})
	if err != nil {
		t.Fatalf("CountCompletedReturnsForSale: %v", err)
	}
	if count != 1 {
		t.Errorf("want 1 completed return for the sale, got %d", count)
	}
}

func TestListSaleItems_staffHasUnitCostAndReturnedQtyAggregatesCompletedReturnsOnly(t *testing.T) {
	f := newSalesFixture(t, "shop-sales-list-items")
	ctx := context.Background()

	sale, items := newSale(ctx, t, f.q, f.shopID, f.locationID, f.cashierID, nil, db.SaleKindSale, nil,
		"200.00", "0.00", "200.00",
		[]saleItemSpec{{variantID: f.variantID, qty: "2.000", unitPrice: "100.00", unitCost: "50.00", lineTotal: "200.00"}})
	original := items[0]

	staffItems, err := f.q.ListSaleItemsForStaff(ctx, db.ListSaleItemsForStaffParams{ShopID: f.shopID, SaleID: sale.ID, Locale: "uz"})
	if err != nil {
		t.Fatalf("ListSaleItemsForStaff: %v", err)
	}
	if len(staffItems) != 1 {
		t.Fatalf("want 1 sale item, got %d", len(staffItems))
	}
	if numericString(t, staffItems[0].UnitCost) != "50.00" {
		t.Errorf("want unit_cost 50.00 for staff, got %s", numericString(t, staffItems[0].UnitCost))
	}
	if got := normalizeScale3(numericString(t, staffItems[0].ReturnedQty)); got != "0.000" {
		t.Errorf("want returned_qty 0.000 before any return, got %s", got)
	}

	cashierItems, err := f.q.ListSaleItemsForCashier(ctx, db.ListSaleItemsForCashierParams{ShopID: f.shopID, SaleID: sale.ID, Locale: "uz"})
	if err != nil {
		t.Fatalf("ListSaleItemsForCashier: %v", err)
	}
	if len(cashierItems) != 1 {
		t.Fatalf("want 1 sale item for cashier, got %d", len(cashierItems))
	}
	// ListSaleItemsForCashierRow has no UnitCost field at all (§
	// 04-DATA-MODEL.md rule 8) — the absence is enforced at compile time by
	// the struct shape, not checked here at runtime.

	// A voided return referencing the original line must NOT count towards
	// returned_qty (a voided return never happened). VoidSale itself
	// refuses a return-kind row (D-59, tested separately in
	// TestVoidSale_refusesReturnKind), so the voided state here is set
	// directly, the way a hypothetical future correction path (or a
	// support-tooling escape hatch) would have to: the sales_immutable
	// trigger and the void-columns CHECK both still apply regardless of
	// kind, only VoidSale's own WHERE clause is sale-only.
	voidedReturn, _ := newSale(ctx, t, f.q, f.shopID, f.locationID, f.cashierID, nil, db.SaleKindReturn, &sale.ID,
		"100.00", "0.00", "100.00",
		[]saleItemSpec{{variantID: f.variantID, qty: "1.000", unitPrice: "100.00", unitCost: "50.00", lineTotal: "100.00", originalSaleItemID: &original.ID}})
	owner := salesUser(ctx, t, f.q, f.shopID, "owner1", db.UserRoleOwner)
	if _, err := f.pool.Exec(ctx, `UPDATE sales SET status = 'voided', voided_at = now(), voided_by = $1 WHERE id = $2`, owner.ID, voidedReturn.ID); err != nil {
		t.Fatalf("void the return directly: %v", err)
	}

	afterVoided, err := f.q.ListSaleItemsForStaff(ctx, db.ListSaleItemsForStaffParams{ShopID: f.shopID, SaleID: sale.ID, Locale: "uz"})
	if err != nil {
		t.Fatalf("ListSaleItemsForStaff (after voided return): %v", err)
	}
	if got := normalizeScale3(numericString(t, afterVoided[0].ReturnedQty)); got != "0.000" {
		t.Errorf("want returned_qty still 0.000 after the return was voided, got %s", got)
	}

	// A completed partial return of qty 1 out of 2 now counts.
	newSale(ctx, t, f.q, f.shopID, f.locationID, f.cashierID, nil, db.SaleKindReturn, &sale.ID,
		"100.00", "0.00", "100.00",
		[]saleItemSpec{{variantID: f.variantID, qty: "1.000", unitPrice: "100.00", unitCost: "50.00", lineTotal: "100.00", originalSaleItemID: &original.ID}})

	afterCompleted, err := f.q.ListSaleItemsForStaff(ctx, db.ListSaleItemsForStaffParams{ShopID: f.shopID, SaleID: sale.ID, Locale: "uz"})
	if err != nil {
		t.Fatalf("ListSaleItemsForStaff (after completed return): %v", err)
	}
	if got := normalizeScale3(numericString(t, afterCompleted[0].ReturnedQty)); got != "1.000" {
		t.Errorf("want returned_qty 1.000 after a completed partial return, got %s", got)
	}
}

// TestListSaleItems_returnedQtyHandlesFractionalQuantities guards against a
// regression where returned_qty's COALESCE(sum(...), 0) made sqlc infer
// int64 (the literal 0's type) instead of numeric — which 500s in Go the
// moment a fractional qty (e.g. weighed goods, unit precision > 0) comes
// back from a real return. The explicit ::numeric(12,3) cast in
// ListSaleItemsForStaff/ForCashier is what this test would catch a
// regression of.
func TestListSaleItems_returnedQtyHandlesFractionalQuantities(t *testing.T) {
	f := newSalesFixture(t, "shop-sales-returned-qty-fractional")
	ctx := context.Background()

	sale, items := newSale(ctx, t, f.q, f.shopID, f.locationID, f.cashierID, nil, db.SaleKindSale, nil,
		"250.00", "0.00", "250.00",
		[]saleItemSpec{{variantID: f.variantID, qty: "2.500", unitPrice: "100.00", unitCost: "50.00", lineTotal: "250.00"}})
	original := items[0]

	newSale(ctx, t, f.q, f.shopID, f.locationID, f.cashierID, nil, db.SaleKindReturn, &sale.ID,
		"150.00", "0.00", "150.00",
		[]saleItemSpec{{variantID: f.variantID, qty: "1.500", unitPrice: "100.00", unitCost: "50.00", lineTotal: "150.00", originalSaleItemID: &original.ID}})

	staffItems, err := f.q.ListSaleItemsForStaff(ctx, db.ListSaleItemsForStaffParams{ShopID: f.shopID, SaleID: sale.ID, Locale: "uz"})
	if err != nil {
		t.Fatalf("ListSaleItemsForStaff: %v", err)
	}
	if got := normalizeScale3(numericString(t, staffItems[0].ReturnedQty)); got != "1.500" {
		t.Errorf("want returned_qty 1.500, got %s", got)
	}

	cashierItems, err := f.q.ListSaleItemsForCashier(ctx, db.ListSaleItemsForCashierParams{ShopID: f.shopID, SaleID: sale.ID, Locale: "uz"})
	if err != nil {
		t.Fatalf("ListSaleItemsForCashier: %v", err)
	}
	if got := normalizeScale3(numericString(t, cashierItems[0].ReturnedQty)); got != "1.500" {
		t.Errorf("want returned_qty 1.500 for cashier, got %s", got)
	}
}

func TestListSalesForStaff_filtersAndCursor(t *testing.T) {
	f := newSalesFixture(t, "shop-sales-list-filters")
	ctx := context.Background()
	otherCashier := salesUser(ctx, t, f.q, f.shopID, "cashier2", db.UserRoleCashier)

	saleA, _ := newSale(ctx, t, f.q, f.shopID, f.locationID, f.cashierID, nil, db.SaleKindSale, nil,
		"100.00", "0.00", "100.00",
		[]saleItemSpec{{variantID: f.variantID, qty: "1.000", unitPrice: "100.00", unitCost: "50.00", lineTotal: "100.00"}})
	saleB, _ := newSale(ctx, t, f.q, f.shopID, f.locationID, otherCashier.ID, nil, db.SaleKindSale, nil,
		"50.00", "0.00", "50.00",
		[]saleItemSpec{{variantID: f.variantID, qty: "1.000", unitPrice: "50.00", unitCost: "25.00", lineTotal: "50.00"}})
	returnC, _ := newSale(ctx, t, f.q, f.shopID, f.locationID, f.cashierID, nil, db.SaleKindReturn, &saleA.ID,
		"100.00", "0.00", "100.00", nil)

	all, err := f.q.ListSalesForStaff(ctx, db.ListSalesForStaffParams{ShopID: f.shopID, Limit: 100})
	if err != nil {
		t.Fatalf("ListSalesForStaff (all): %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("want 3 sales total, got %d", len(all))
	}

	byCashier, err := f.q.ListSalesForStaff(ctx, db.ListSalesForStaffParams{ShopID: f.shopID, CashierID: &f.cashierID, Limit: 100})
	if err != nil {
		t.Fatalf("ListSalesForStaff (by cashier): %v", err)
	}
	gotIDs := map[uuid.UUID]bool{}
	for _, r := range byCashier {
		gotIDs[r.ID] = true
	}
	if !gotIDs[saleA.ID] || !gotIDs[returnC.ID] || gotIDs[saleB.ID] {
		t.Fatalf("want saleA and returnC (both f.cashierID) but not saleB, got %+v", gotIDs)
	}

	kind := db.SaleKindReturn
	byKind, err := f.q.ListSalesForStaff(ctx, db.ListSalesForStaffParams{ShopID: f.shopID, Kind: &kind, Limit: 100})
	if err != nil {
		t.Fatalf("ListSalesForStaff (by kind): %v", err)
	}
	if len(byKind) != 1 || byKind[0].ID != returnC.ID {
		t.Fatalf("want only returnC for kind=return, got %+v", byKind)
	}

	// Cursor pagination (one row per page) reproduces the full unfiltered
	// list, in the same order.
	var paginated []uuid.UUID
	p := db.ListSalesForStaffParams{ShopID: f.shopID, Limit: 1}
	for {
		page, err := f.q.ListSalesForStaff(ctx, p)
		if err != nil {
			t.Fatalf("ListSalesForStaff (paginated): %v", err)
		}
		if len(page) == 0 {
			break
		}
		for _, r := range page {
			paginated = append(paginated, r.ID)
		}
		last := page[len(page)-1]
		ca, id := last.CompletedAt, last.ID
		p.CursorCompletedAt, p.CursorID = &ca, &id
	}
	if len(paginated) != len(all) {
		t.Fatalf("want pagination to reproduce all %d sales, got %d", len(all), len(paginated))
	}
	for i := range all {
		if all[i].ID != paginated[i] {
			t.Fatalf("pagination order mismatch at index %d: want %s, got %s", i, all[i].ID, paginated[i])
		}
	}
}

// decimalOf parses a pgtype.Numeric result into a shopspring/decimal.Decimal
// for the reconciliation arithmetic below — never float (§ 04-DATA-MODEL.md
// rule 3), and the same library the service layer uses for money
// (sqlc.yaml's own comment on the numeric override).
func decimalOf(t *testing.T, n pgtype.Numeric) decimal.Decimal {
	t.Helper()
	d, err := decimal.NewFromString(numericString(t, n))
	if err != nil {
		t.Fatalf("decimalOf: %v", err)
	}
	return d
}

// TestReports_summaryAndByProductReconcileUnderD64Discount is the D-64
// worked example from the review: a sale with subtotal 100 / discount 10 /
// total 90, two lines of 50 each (cost 10 each), and a return refunding
// one whole line. D-61's proportional formula (line share = line_total /
// subtotal) gives that line a 5.00 share of the discount, so its net
// revenue — and therefore the return refunding it in full — is 45.00, not
// the line's gross 50.00. Before this fix, SalesByProduct summed the sale
// leg's gross line_total while the return leg was already net (D-61),
// so the two reports disagreed by exactly the discount; this test pins
// the numbers so that regression cannot come back unnoticed.
func TestReports_summaryAndByProductReconcileUnderD64Discount(t *testing.T) {
	f := newSalesFixture(t, "shop-sales-reports-d64")
	ctx := context.Background()

	sale, items := newSale(ctx, t, f.q, f.shopID, f.locationID, f.cashierID, nil, db.SaleKindSale, nil,
		"100.00", "10.00", "90.00",
		[]saleItemSpec{
			{variantID: f.variantID, qty: "1.000", unitPrice: "50.00", unitCost: "10.00", lineTotal: "50.00"},
			{variantID: f.variantID, qty: "1.000", unitPrice: "50.00", unitCost: "10.00", lineTotal: "50.00"},
		})
	// Refunds items[0] in full: net line revenue = 50 - round(50*10/100, 2)
	// = 50 - 5 = 45 (D-61); the return row carries that net amount, not the
	// line's gross 50.
	newSale(ctx, t, f.q, f.shopID, f.locationID, f.cashierID, nil, db.SaleKindReturn, &sale.ID,
		"45.00", "0.00", "45.00",
		[]saleItemSpec{{variantID: f.variantID, qty: "1.000", unitPrice: "45.00", unitCost: "10.00", lineTotal: "45.00", originalSaleItemID: &items[0].ID}})

	from := sale.CompletedAt.Add(-time.Second)
	to := sale.CompletedAt.Add(48 * time.Hour)

	staff, err := f.q.SalesSummaryForStaff(ctx, db.SalesSummaryForStaffParams{ShopID: f.shopID, From: from, To: to})
	if err != nil {
		t.Fatalf("SalesSummaryForStaff: %v", err)
	}
	if staff.SalesCount != 1 || staff.ReturnsCount != 1 {
		t.Fatalf("want 1 sale and 1 return, got sales=%d returns=%d", staff.SalesCount, staff.ReturnsCount)
	}
	if numericString(t, staff.Revenue) != "90.00" {
		t.Errorf("want revenue 90.00, got %s", numericString(t, staff.Revenue))
	}
	if numericString(t, staff.Refunds) != "45.00" {
		t.Errorf("want refunds 45.00, got %s", numericString(t, staff.Refunds))
	}
	if numericString(t, staff.Discounts) != "10.00" {
		t.Errorf("want discounts 10.00, got %s", numericString(t, staff.Discounts))
	}
	// cost = (10 + 10) sold - 10 returned = 10.00
	if numericString(t, staff.Cost) != "10.00" {
		t.Errorf("want cost 10.00, got %s", numericString(t, staff.Cost))
	}
	net := decimalOf(t, staff.Revenue).Sub(decimalOf(t, staff.Refunds))
	if net.StringFixed(2) != "45.00" {
		t.Errorf("want net (revenue - refunds) 45.00, got %s", net.StringFixed(2))
	}
	margin := net.Sub(decimalOf(t, staff.Cost))
	if margin.StringFixed(2) != "35.00" {
		t.Errorf("want margin (net - cost) 35.00, got %s", margin.StringFixed(2))
	}

	cashier, err := f.q.SalesSummaryForCashier(ctx, db.SalesSummaryForCashierParams{ShopID: f.shopID, From: from, To: to})
	if err != nil {
		t.Fatalf("SalesSummaryForCashier: %v", err)
	}
	if numericString(t, cashier.Revenue) != numericString(t, staff.Revenue) ||
		numericString(t, cashier.Refunds) != numericString(t, staff.Refunds) ||
		numericString(t, cashier.Discounts) != numericString(t, staff.Discounts) {
		t.Errorf("want the same revenue/refunds/discounts for cashier and staff summaries")
	}
	// SalesSummaryForCashierRow has no Cost field at all (§ 04-DATA-MODEL.md
	// rule 8) — enforced at compile time by the struct shape.

	rows, err := f.q.SalesByProduct(ctx, db.SalesByProductParams{ShopID: f.shopID, From: from, To: to, Limit: 100, Locale: "uz"})
	if err != nil {
		t.Fatalf("SalesByProduct: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 product row, got %d", len(rows))
	}
	row := rows[0]
	if numericString(t, row.QtySold) != "2.000" {
		t.Errorf("want qty_sold 2.000, got %s", numericString(t, row.QtySold))
	}
	if numericString(t, row.QtyReturned) != "1.000" {
		t.Errorf("want qty_returned 1.000, got %s", numericString(t, row.QtyReturned))
	}
	if numericString(t, row.Revenue) != "45.00" {
		t.Errorf("want net revenue 45.00 ((50-5) + (50-5) sold - 45 returned), got %s", numericString(t, row.Revenue))
	}
	if numericString(t, row.Cost) != "10.00" {
		t.Errorf("want net cost 10.00 (10+10 sold - 10 returned), got %s", numericString(t, row.Cost))
	}

	// Reconciliation (the whole point of this test, and D-64): since this
	// shop has exactly one product, SalesByProduct's single row must equal
	// the shop-wide net (revenue - refunds) and margin from the summary —
	// this is what disagreed by exactly the discount before the fix.
	if numericString(t, row.Revenue) != net.StringFixed(2) {
		t.Errorf("want by-product revenue to reconcile with summary net (revenue - refunds) %s, got %s", net.StringFixed(2), numericString(t, row.Revenue))
	}
	byProductMargin := decimalOf(t, row.Revenue).Sub(decimalOf(t, row.Cost))
	if byProductMargin.StringFixed(2) != margin.StringFixed(2) {
		t.Errorf("want by-product margin to reconcile with summary margin %s, got %s", margin.StringFixed(2), byProductMargin.StringFixed(2))
	}
}

// TestSalesByProduct_lastLineAbsorbsRoundingRemainder is the owner ruling
// amending D-64: a plain round(line_total * discount_amount / subtotal, 2)
// on every line can lose or gain a cent to rounding when the shares do not
// divide evenly. Three lines of 33.33/33.33/33.34 (subtotal 100.00) with a
// 10.00 discount each independently round to a 3.33 share (33.33*10/100 =
// 3.333 -> 3.33, and 33.34*10/100 = 3.334 -> 3.33), which would sum to
// 9.99, one cent short of the actual 10.00 discount. The fix allocates the
// last line (by sale_items.id order — uuid v7, insertion order) whatever
// is left over instead of its own independently-rounded share, so the
// three lines' net revenue is exactly 30.00 + 30.00 + 30.00 = 90.00, not
// 90.01.
func TestSalesByProduct_lastLineAbsorbsRoundingRemainder(t *testing.T) {
	f := newSalesFixture(t, "shop-sales-by-product-rounding")
	ctx := context.Background()

	sale, _ := newSale(ctx, t, f.q, f.shopID, f.locationID, f.cashierID, nil, db.SaleKindSale, nil,
		"100.00", "10.00", "90.00",
		[]saleItemSpec{
			{variantID: f.variantID, qty: "1.000", unitPrice: "33.33", unitCost: "1.00", lineTotal: "33.33"},
			{variantID: f.variantID, qty: "1.000", unitPrice: "33.33", unitCost: "1.00", lineTotal: "33.33"},
			{variantID: f.variantID, qty: "1.000", unitPrice: "33.34", unitCost: "1.00", lineTotal: "33.34"},
		})

	from := sale.CompletedAt.Add(-time.Second)
	to := sale.CompletedAt.Add(48 * time.Hour)

	rows, err := f.q.SalesByProduct(ctx, db.SalesByProductParams{ShopID: f.shopID, From: from, To: to, Limit: 100, Locale: "uz"})
	if err != nil {
		t.Fatalf("SalesByProduct: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 product row, got %d", len(rows))
	}
	if got := numericString(t, rows[0].Revenue); got != "90.00" {
		t.Errorf("want net revenue exactly 90.00 (matching the sale's own total, no rounding drift), got %s", got)
	}
}

// TestSalesByProduct_threeEqualLinesRoundingRemainder is the second owner
// example: subtotal 3.00, discount 2.00, three lines of 1.00 each. Each
// line's independent share would round(1*2/3, 2) = round(0.6667, 2) =
// 0.67, and three of those sum to 2.01 — one cent more than the actual
// 2.00 discount. The last line instead gets 2.00 - 0.67 - 0.67 = 0.66, so
// net revenue is 0.33 + 0.33 + 0.34 = 1.00 exactly, matching the sale's
// own total.
func TestSalesByProduct_threeEqualLinesRoundingRemainder(t *testing.T) {
	f := newSalesFixture(t, "shop-sales-by-product-rounding-equal")
	ctx := context.Background()

	sale, _ := newSale(ctx, t, f.q, f.shopID, f.locationID, f.cashierID, nil, db.SaleKindSale, nil,
		"3.00", "2.00", "1.00",
		[]saleItemSpec{
			{variantID: f.variantID, qty: "1.000", unitPrice: "1.00", unitCost: "0.10", lineTotal: "1.00"},
			{variantID: f.variantID, qty: "1.000", unitPrice: "1.00", unitCost: "0.10", lineTotal: "1.00"},
			{variantID: f.variantID, qty: "1.000", unitPrice: "1.00", unitCost: "0.10", lineTotal: "1.00"},
		})

	from := sale.CompletedAt.Add(-time.Second)
	to := sale.CompletedAt.Add(48 * time.Hour)

	rows, err := f.q.SalesByProduct(ctx, db.SalesByProductParams{ShopID: f.shopID, From: from, To: to, Limit: 100, Locale: "uz"})
	if err != nil {
		t.Fatalf("SalesByProduct: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 product row, got %d", len(rows))
	}
	if got := numericString(t, rows[0].Revenue); got != "1.00" {
		t.Errorf("want net revenue exactly 1.00 (matching the sale's own total, no rounding drift), got %s", got)
	}
}
