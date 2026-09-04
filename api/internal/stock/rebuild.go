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

// Rebuild recomputes shopSlug's stock_levels entirely from stock_movements
// (ADR-006, docs/04-DATA-MODEL.md § 3: "Rebuildable: savdo stock
// rebuild"). One transaction: pg_advisory_xact_lock keyed by the shop (so
// two concurrent rebuilds of the same shop serialize instead of racing —
// the second waits for the first's transaction to end, then reruns
// against whatever the ledger looks like at that point, rather than
// interleaving truncate/rebuild statements from both), TruncateLevelsForShop,
// RebuildLevelsFromMovements, then the two counts. The lock does not stop
// a concurrent stock.Service.Move for the same shop from running (Move
// opens its own, unrelated transaction and takes no advisory lock) — the
// CLI's own --help says as much: run this only when writers are idle.
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

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "stock-rebuild:"+shopRow.ID.String()); err != nil {
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
