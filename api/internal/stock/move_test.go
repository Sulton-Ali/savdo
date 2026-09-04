package stock_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/stock"
)

func d(t *testing.T, s string) decimal.Decimal {
	t.Helper()
	v, err := decimal.NewFromString(s)
	if err != nil {
		t.Fatalf("decimal(%q): %v", s, err)
	}
	return v
}

func adjReason(r db.AdjustmentReason) *db.AdjustmentReason { return &r }

func TestMove_variantFromAnotherShop404s(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()

	shopA := seedShop(ctx, t, q, "move-shop-a")
	shopB := seedShop(ctx, t, q, "move-shop-b")
	unitB := seedUnit(ctx, t, q, shopB.ID, "pcs")
	productB := seedProduct(ctx, t, q, shopB.ID, unitB.ID, "other-shop-product")
	variantB := seedVariant(ctx, t, q, shopB.ID, productB.ID, "{}")
	locA := seedLocation(ctx, t, q, shopA.ID, "Main A")

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := q.WithTx(tx)

	_, err = stock.Move(ctx, qtx, stock.MoveParams{
		ShopID: shopA.ID, VariantID: variantB.ID, LocationID: locA.ID,
		Kind: db.StockMovementKindAdjustment, Qty: d(t, "1.000"), AdjustmentReason: adjReason(db.AdjustmentReasonFound),
	})
	if err == nil {
		t.Fatal("want an error moving a variant that belongs to another shop, got none")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 404 {
		t.Fatalf("error = %v, want a 404 *apierr.Error", err)
	}
}

func TestMove_locationFromAnotherShop404s(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()

	shopA := seedShop(ctx, t, q, "move-loc-shop-a")
	shopB := seedShop(ctx, t, q, "move-loc-shop-b")
	unitA := seedUnit(ctx, t, q, shopA.ID, "pcs")
	productA := seedProduct(ctx, t, q, shopA.ID, unitA.ID, "shop-a-product")
	variantA := seedVariant(ctx, t, q, shopA.ID, productA.ID, "{}")
	locB := seedLocation(ctx, t, q, shopB.ID, "Main B")

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := q.WithTx(tx)

	_, err = stock.Move(ctx, qtx, stock.MoveParams{
		ShopID: shopA.ID, VariantID: variantA.ID, LocationID: locB.ID,
		Kind: db.StockMovementKindAdjustment, Qty: d(t, "1.000"), AdjustmentReason: adjReason(db.AdjustmentReasonFound),
	})
	if err == nil {
		t.Fatal("want an error moving to a location that belongs to another shop, got none")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 404 {
		t.Fatalf("error = %v, want a 404 *apierr.Error", err)
	}
}

// TestMove_insufficientRollsBackNoLevelRowNoMovementRow is the "a failure
// must not leave anything behind" test: Move's own doc comment on
// ErrInsufficient warns UpsertLevelRow's zero-row insert only stays undone
// because the caller rolls back — this pins that the caller side (a
// pool.Begin/defer Rollback pair, same shape every real caller uses)
// actually delivers on it.
func TestMove_insufficientRollsBackNoLevelRowNoMovementRow(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()

	shop := seedShop(ctx, t, q, "move-insufficient-rollback")
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "rollback-product")
	variant := seedVariant(ctx, t, q, shop.ID, product.ID, "{}")
	loc := seedLocation(ctx, t, q, shop.ID, "Main")

	// No opening stock at all: no stock_levels row exists yet for this
	// (variant, location) — the level Move's UpsertLevelRow would create is
	// exactly the "nothing must survive" row this test checks for.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	qtx := q.WithTx(tx)

	_, moveErr := stock.Move(ctx, qtx, stock.MoveParams{
		ShopID: shop.ID, VariantID: variant.ID, LocationID: loc.ID,
		Kind: db.StockMovementKindAdjustment, Qty: d(t, "-1.000"), AdjustmentReason: adjReason(db.AdjustmentReasonCountCorrection),
	})
	var insufficient *stock.ErrInsufficient
	if !errors.As(moveErr, &insufficient) {
		t.Fatalf("want ErrInsufficient moving -1 with no opening stock and allow_negative_stock=false, got: %v", moveErr)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	if _, exists := readLevel(ctx, t, pool, shop.ID, variant.ID, loc.ID); exists {
		t.Fatal("want no stock_levels row after a rolled-back ErrInsufficient, found one")
	}
	if got := countMovements(ctx, t, pool, shop.ID, variant.ID, loc.ID, ""); got != 0 {
		t.Fatalf("want 0 stock_movements rows after a rolled-back ErrInsufficient, got %d", got)
	}
}

// TestMove_concurrentLastUnit_exactlyOneSucceeds is the correctness-
// critical race test: two goroutines each try to move the last unit away
// (kind adjustment, qty -1) in their own, separate transactions. Move's
// UpsertLevelRow + GetLevelForUpdate (SELECT ... FOR UPDATE) sequence must
// serialize them — the second to reach the lock blocks until the first
// commits or rolls back, then reads the now-zero level and gets
// ErrInsufficient itself — so exactly one succeeds, never both and never
// neither.
func TestMove_concurrentLastUnit_exactlyOneSucceeds(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()

	shop := seedShop(ctx, t, q, "move-race")
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "race-product")
	variant := seedVariant(ctx, t, q, shop.ID, product.ID, "{}")
	loc := seedLocation(ctx, t, q, shop.ID, "Main")

	seedTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin seed: %v", err)
	}
	if _, err := stock.Move(ctx, q.WithTx(seedTx), stock.MoveParams{
		ShopID: shop.ID, VariantID: variant.ID, LocationID: loc.ID,
		Kind: db.StockMovementKindPurchaseIn, Qty: d(t, "1.000"),
	}); err != nil {
		t.Fatalf("seed opening stock: %v", err)
	}
	if err := seedTx.Commit(ctx); err != nil {
		t.Fatalf("commit seed: %v", err)
	}

	run := func() error {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		_, moveErr := stock.Move(ctx, q.WithTx(tx), stock.MoveParams{
			ShopID: shop.ID, VariantID: variant.ID, LocationID: loc.ID,
			Kind: db.StockMovementKindAdjustment, Qty: d(t, "-1.000"), AdjustmentReason: adjReason(db.AdjustmentReasonCountCorrection),
		})
		if moveErr != nil {
			_ = tx.Rollback(ctx)
			return moveErr
		}
		return tx.Commit(ctx)
	}

	const n = 2
	results := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			results[i] = run()
		}(i)
	}
	wg.Wait()

	succeeded, failed := 0, 0
	for _, err := range results {
		if err == nil {
			succeeded++
			continue
		}
		failed++
		var insufficient *stock.ErrInsufficient
		if !errors.As(err, &insufficient) {
			t.Fatalf("want ErrInsufficient for the losing goroutine, got: %v", err)
		}
	}
	if succeeded != 1 || failed != 1 {
		t.Fatalf("succeeded=%d failed=%d, want exactly 1 and 1", succeeded, failed)
	}

	qty, exists := readLevel(ctx, t, pool, shop.ID, variant.ID, loc.ID)
	if !exists {
		t.Fatal("want a stock_levels row after the race, found none")
	}
	if !qty.Equal(d(t, "0.000")) {
		t.Fatalf("final level = %s, want 0.000", qty)
	}
	// The seed purchase_in (1) plus exactly one successful adjustment (1) —
	// the losing goroutine's adjustment must not have been written.
	if got := countMovements(ctx, t, pool, shop.ID, variant.ID, loc.ID, db.StockMovementKindAdjustment); got != 1 {
		t.Fatalf("adjustment movement count = %d, want 1 (the loser's rolled back)", got)
	}
	if got := countMovements(ctx, t, pool, shop.ID, variant.ID, loc.ID, ""); got != 2 {
		t.Fatalf("total movement count = %d, want 2 (1 seed purchase_in + 1 adjustment)", got)
	}
}

// TestMove_concurrentLastUnit_allowNegativeStockLetsBothSucceed is the same
// race as above, but on a shop with allow_negative_stock = true (D-41/
// D-48's second branch): the row lock still serializes the two
// transactions (one still waits for the other), but neither is rejected —
// both succeed, and the level goes negative.
func TestMove_concurrentLastUnit_allowNegativeStockLetsBothSucceed(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()

	shop := seedShopAllowNegative(ctx, t, q, "move-race-negative")
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "race-negative-product")
	variant := seedVariant(ctx, t, q, shop.ID, product.ID, "{}")
	loc := seedLocation(ctx, t, q, shop.ID, "Main")

	seedTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin seed: %v", err)
	}
	if _, err := stock.Move(ctx, q.WithTx(seedTx), stock.MoveParams{
		ShopID: shop.ID, VariantID: variant.ID, LocationID: loc.ID,
		Kind: db.StockMovementKindPurchaseIn, Qty: d(t, "1.000"),
	}); err != nil {
		t.Fatalf("seed opening stock: %v", err)
	}
	if err := seedTx.Commit(ctx); err != nil {
		t.Fatalf("commit seed: %v", err)
	}

	run := func() error {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		_, moveErr := stock.Move(ctx, q.WithTx(tx), stock.MoveParams{
			ShopID: shop.ID, VariantID: variant.ID, LocationID: loc.ID,
			Kind: db.StockMovementKindAdjustment, Qty: d(t, "-1.000"), AdjustmentReason: adjReason(db.AdjustmentReasonCountCorrection),
		})
		if moveErr != nil {
			_ = tx.Rollback(ctx)
			return moveErr
		}
		return tx.Commit(ctx)
	}

	const n = 2
	results := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			results[i] = run()
		}(i)
	}
	wg.Wait()

	for i, err := range results {
		if err != nil {
			t.Fatalf("goroutine %d: want success (allow_negative_stock=true), got: %v", i, err)
		}
	}

	qty, exists := readLevel(ctx, t, pool, shop.ID, variant.ID, loc.ID)
	if !exists {
		t.Fatal("want a stock_levels row after the race, found none")
	}
	if !qty.Equal(d(t, "-1.000")) {
		t.Fatalf("final level = %s, want -1.000 (1 opening - 1 - 1)", qty)
	}
	if got := countMovements(ctx, t, pool, shop.ID, variant.ID, loc.ID, db.StockMovementKindAdjustment); got != 2 {
		t.Fatalf("adjustment movement count = %d, want 2 (both succeeded)", got)
	}
}

// TestMoveInTx_commitsOnSuccessRollsBackOnFailure exercises the
// convenience wrapper single-move callers use: success commits (the level
// and movement are visible afterwards), failure leaves nothing behind
// (MoveInTx's own defer Rollback fires).
func TestMoveInTx_commitsOnSuccessRollsBackOnFailure(t *testing.T) {
	pool, q := newTestQueries(t)
	ctx := context.Background()
	svc := stock.NewService(pool, q)

	shop := seedShop(ctx, t, q, "move-in-tx")
	unit := seedUnit(ctx, t, q, shop.ID, "pcs")
	product := seedProduct(ctx, t, q, shop.ID, unit.ID, "move-in-tx-product")
	variant := seedVariant(ctx, t, q, shop.ID, product.ID, "{}")
	loc := seedLocation(ctx, t, q, shop.ID, "Main")

	mv, err := svc.MoveInTx(ctx, stock.MoveParams{
		ShopID: shop.ID, VariantID: variant.ID, LocationID: loc.ID,
		Kind: db.StockMovementKindPurchaseIn, Qty: d(t, "3.000"),
	})
	if err != nil {
		t.Fatalf("MoveInTx (success): %v", err)
	}
	if mv.ID == uuid.Nil {
		t.Fatal("MoveInTx returned a zero-value movement on success")
	}
	qty, exists := readLevel(ctx, t, pool, shop.ID, variant.ID, loc.ID)
	if !exists || !qty.Equal(d(t, "3.000")) {
		t.Fatalf("level after MoveInTx = (%s, exists=%v), want (3.000, true)", qty, exists)
	}

	_, err = svc.MoveInTx(ctx, stock.MoveParams{
		ShopID: shop.ID, VariantID: variant.ID, LocationID: loc.ID,
		Kind: db.StockMovementKindAdjustment, Qty: d(t, "-10.000"), AdjustmentReason: adjReason(db.AdjustmentReasonCountCorrection),
	})
	var insufficient *stock.ErrInsufficient
	if !errors.As(err, &insufficient) {
		t.Fatalf("MoveInTx (failure): want ErrInsufficient, got: %v", err)
	}

	qty, exists = readLevel(ctx, t, pool, shop.ID, variant.ID, loc.ID)
	if !exists || !qty.Equal(d(t, "3.000")) {
		t.Fatalf("level after a failed MoveInTx = (%s, exists=%v), want unchanged (3.000, true)", qty, exists)
	}
	if got := countMovements(ctx, t, pool, shop.ID, variant.ID, loc.ID, db.StockMovementKindAdjustment); got != 0 {
		t.Fatalf("adjustment movement count after a failed MoveInTx = %d, want 0", got)
	}
}
