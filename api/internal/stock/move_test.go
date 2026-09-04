package stock_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
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

// moveStep is one goroutine's half of a deterministic two-transaction
// interleaving (MINOR 3): it begins its own transaction, runs Move — which
// blocks inside GetLevelForUpdate if some other transaction already holds
// the row's lock — and, once Move returns, waits on commit before
// finishing. locked closes the instant Move returns (the transaction is
// still open at that point: this is the moment to assert a second,
// concurrent Move on the same row is genuinely blocked, not just "hasn't
// happened to run yet").
type moveStep struct {
	result stock.MoveResult
	err    error
	locked chan struct{}
	commit chan struct{}
	done   chan error
}

// startMoveHoldingLock begins a transaction, runs Move inside it, and
// blocks (holding whatever row lock Move took) until the test sends on
// commit — letting the test observe "Move returned, but the transaction
// (and its lock) is still open" as a distinct, assertable moment rather
// than a race between two goroutines' real wall-clock timing.
func startMoveHoldingLock(ctx context.Context, t *testing.T, pool *pgxpool.Pool, q *db.Queries, p stock.MoveParams) *moveStep {
	t.Helper()
	s := &moveStep{locked: make(chan struct{}), commit: make(chan struct{}), done: make(chan error, 1)}
	go func() {
		tx, err := pool.Begin(ctx)
		if err != nil {
			s.err = err
			close(s.locked)
			s.done <- err
			return
		}
		s.result, s.err = stock.Move(ctx, q.WithTx(tx), p)
		close(s.locked)

		<-s.commit
		if s.err != nil {
			s.done <- tx.Rollback(ctx)
			return
		}
		s.done <- tx.Commit(ctx)
	}()
	return s
}

// assertStillBlocked asserts s.locked has not closed within timeout — a
// bounded wait (channel + select), not a bare sleep used as a
// synchronization primitive: a false pass here (s actually finished, just
// slower than the wait) is impossible by construction, since we only ever
// assert "still blocked" for a goroutine we know is contending for a row
// lock another, still-open transaction holds.
func assertStillBlocked(t *testing.T, label string, s *moveStep, timeout time.Duration) {
	t.Helper()
	select {
	case <-s.locked:
		t.Fatalf("%s: Move returned before the blocking transaction committed, want it still blocked", label)
	case <-time.After(timeout):
	}
}

// waitLocked waits (with a generous timeout, not a bare sleep) for s.Move
// to have returned, then returns its error.
func waitLocked(t *testing.T, label string, s *moveStep, timeout time.Duration) error {
	t.Helper()
	select {
	case <-s.locked:
		return s.err
	case <-time.After(timeout):
		t.Fatalf("%s: Move did not return within %s", label, timeout)
		return nil
	}
}

// finish tells s to commit (or roll back, if Move itself failed) and waits
// for that to complete.
func finish(t *testing.T, label string, s *moveStep, timeout time.Duration) error {
	t.Helper()
	close(s.commit)
	select {
	case err := <-s.done:
		return err
	case <-time.After(timeout):
		t.Fatalf("%s: commit/rollback did not complete within %s", label, timeout)
		return nil
	}
}

const (
	blockedWait = 200 * time.Millisecond
	resultWait  = 5 * time.Second
)

// TestMove_concurrentLastUnit_exactlyOneSucceeds is the correctness-
// critical race test, made deterministic (MINOR 3): goroutine A locks and
// applies its -1 adjustment inside its own, still-open transaction; the
// test confirms goroutine B is genuinely blocked (not just not-yet-
// scheduled) trying to do the same before letting A commit; only then does
// B's Move unblock, see the now-zero level and fail with ErrInsufficient.
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

	adjustment := stock.MoveParams{
		ShopID: shop.ID, VariantID: variant.ID, LocationID: loc.ID,
		Kind: db.StockMovementKindAdjustment, Qty: d(t, "-1.000"), AdjustmentReason: adjReason(db.AdjustmentReasonCountCorrection),
	}

	stepA := startMoveHoldingLock(ctx, t, pool, q, adjustment)
	if err := waitLocked(t, "A", stepA, resultWait); err != nil {
		t.Fatalf("A: want Move to succeed (first to the lock), got: %v", err)
	}

	stepB := startMoveHoldingLock(ctx, t, pool, q, adjustment)
	assertStillBlocked(t, "B", stepB, blockedWait)

	if err := finish(t, "A", stepA, resultWait); err != nil {
		t.Fatalf("A: commit: %v", err)
	}

	bMoveErr := waitLocked(t, "B", stepB, resultWait)
	var insufficient *stock.ErrInsufficient
	if !errors.As(bMoveErr, &insufficient) {
		t.Fatalf("B: want ErrInsufficient once unblocked (level already at 0), got: %v", bMoveErr)
	}
	if err := finish(t, "B", stepB, resultWait); err != nil {
		t.Fatalf("B: rollback: %v", err)
	}

	qty, exists := readLevel(ctx, t, pool, shop.ID, variant.ID, loc.ID)
	if !exists {
		t.Fatal("want a stock_levels row after the race, found none")
	}
	if !qty.Equal(d(t, "0.000")) {
		t.Fatalf("final level = %s, want 0.000", qty)
	}
	// The seed purchase_in (1) plus exactly A's successful adjustment (1) —
	// B's adjustment must not have been written.
	if got := countMovements(ctx, t, pool, shop.ID, variant.ID, loc.ID, db.StockMovementKindAdjustment); got != 1 {
		t.Fatalf("adjustment movement count = %d, want 1 (B's rolled back)", got)
	}
	if got := countMovements(ctx, t, pool, shop.ID, variant.ID, loc.ID, ""); got != 2 {
		t.Fatalf("total movement count = %d, want 2 (1 seed purchase_in + 1 adjustment)", got)
	}
}

// TestMove_concurrentLastUnit_allowNegativeStockLetsBothSucceed is the same
// deterministic interleaving as above, but on a shop with
// allow_negative_stock = true (D-41/D-48's second branch): B still blocks
// on A's row lock (locking is unconditional — it does not depend on
// whether negative stock is allowed), but once unblocked it succeeds too,
// and the level goes negative.
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

	adjustment := stock.MoveParams{
		ShopID: shop.ID, VariantID: variant.ID, LocationID: loc.ID,
		Kind: db.StockMovementKindAdjustment, Qty: d(t, "-1.000"), AdjustmentReason: adjReason(db.AdjustmentReasonCountCorrection),
	}

	stepA := startMoveHoldingLock(ctx, t, pool, q, adjustment)
	if err := waitLocked(t, "A", stepA, resultWait); err != nil {
		t.Fatalf("A: want Move to succeed (first to the lock), got: %v", err)
	}

	stepB := startMoveHoldingLock(ctx, t, pool, q, adjustment)
	assertStillBlocked(t, "B", stepB, blockedWait)

	if err := finish(t, "A", stepA, resultWait); err != nil {
		t.Fatalf("A: commit: %v", err)
	}

	if bMoveErr := waitLocked(t, "B", stepB, resultWait); bMoveErr != nil {
		t.Fatalf("B: want success once unblocked (allow_negative_stock=true), got: %v", bMoveErr)
	}
	if err := finish(t, "B", stepB, resultWait); err != nil {
		t.Fatalf("B: commit: %v", err)
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

	result, err := svc.MoveInTx(ctx, stock.MoveParams{
		ShopID: shop.ID, VariantID: variant.ID, LocationID: loc.ID,
		Kind: db.StockMovementKindPurchaseIn, Qty: d(t, "3.000"),
	})
	if err != nil {
		t.Fatalf("MoveInTx (success): %v", err)
	}
	if result.Movement.ID == uuid.Nil {
		t.Fatal("MoveInTx returned a zero-value movement on success")
	}
	if !result.Before.Equal(d(t, "0.000")) || !result.After.Equal(d(t, "3.000")) {
		t.Fatalf("MoveInTx (success) Before/After = %s/%s, want 0.000/3.000", result.Before, result.After)
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
