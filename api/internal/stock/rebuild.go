package stock

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// RebuildResult is `savdo stock rebuild`'s summary line: how many
// stock_levels rows the rebuild produced and how many stock_movements
// rows they were computed from.
type RebuildResult struct {
	Levels    int64
	Movements int64
}

// rebuildLockClassID is the first argument to the two-argument
// pg_advisory_xact_lock(classid, key) Rebuild uses (NIT 9) — a second,
// independent advisory-lock namespace from httpx.Idempotent's own
// idempotencyLockClassID, so the two locks' key spaces can never collide.
const rebuildLockClassID = 2

// Rebuild recomputes shopSlug's stock_levels entirely from stock_movements
// (ADR-006, docs/04-DATA-MODEL.md § 3: "Rebuildable: savdo stock
// rebuild"). One transaction: pg_advisory_xact_lock keyed by the shop (so
// two concurrent rebuilds of the same shop serialize instead of racing —
// the second waits for the first's transaction to end, then reruns
// against whatever the ledger looks like at that point, rather than
// interleaving truncate/rebuild statements from both), TruncateLevelsForShop,
// RebuildLevelsFromMovements, then the two counts.
//
// The lock does not stop a concurrent stock.Service.Move for the same shop
// from running — Move opens its own, unrelated transaction and takes no
// advisory lock — which is why the CLI's own --help says to run this only
// when writers are idle. Concretely, if one runs anyway: a Move that
// already holds a stock_levels row's FOR UPDATE lock (it started before
// TruncateLevelsForShop reached that row) blocks the DELETE until Move's
// transaction ends, so the rebuild simply waits; a Move that starts after
// TruncateLevelsForShop removed a row but before RebuildLevelsFromMovements
// re-inserts it commits its own fresh row for that (variant, location)
// first, and RebuildLevelsFromMovements' INSERT for the same key then
// fails with a unique-violation, aborting the whole rebuild transaction
// (rolled back, nothing partially applied). Either way, a concurrent Move
// can make a rebuild wait or fail outright — it can never make it silently
// overwrite or lose a level.
func Rebuild(ctx context.Context, pool *pgxpool.Pool, shopSlug string) (RebuildResult, error) {
	q := db.New(pool)
	shopRow, err := q.GetShopBySlug(ctx, shopSlug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RebuildResult{}, fmt.Errorf("stock: no shop with slug %q", shopSlug)
		}
		return RebuildResult{}, fmt.Errorf("stock: get shop %q: %w", shopSlug, err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return RebuildResult{}, fmt.Errorf("stock: rebuild: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := db.New(tx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, hashtext($2))`, rebuildLockClassID, shopRow.ID.String()); err != nil {
		return RebuildResult{}, fmt.Errorf("stock: rebuild: advisory lock: %w", err)
	}

	if err := qtx.TruncateLevelsForShop(ctx, shopRow.ID); err != nil {
		return RebuildResult{}, fmt.Errorf("stock: rebuild: truncate levels: %w", err)
	}
	if err := qtx.RebuildLevelsFromMovements(ctx, shopRow.ID); err != nil {
		return RebuildResult{}, fmt.Errorf("stock: rebuild: rebuild levels: %w", err)
	}

	levels, err := qtx.CountLevelsForShop(ctx, shopRow.ID)
	if err != nil {
		return RebuildResult{}, fmt.Errorf("stock: rebuild: count levels: %w", err)
	}
	movements, err := qtx.CountMovementsForShop(ctx, shopRow.ID)
	if err != nil {
		return RebuildResult{}, fmt.Errorf("stock: rebuild: count movements: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return RebuildResult{}, fmt.Errorf("stock: rebuild: commit: %w", err)
	}
	return RebuildResult{Levels: levels, Movements: movements}, nil
}
