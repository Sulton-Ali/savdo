package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// maxConns bounds how many connections the API ever holds open against
	// Postgres at once.
	maxConns = 10

	// maxConnLifetime recycles connections periodically so long-lived ones
	// don't accumulate server-side state or outlive a Postgres failover.
	maxConnLifetime = 30 * time.Minute

	// healthCheckPeriod is how often the pool checks idle connections in
	// the background (pgxpool's own default; set explicitly so it is a
	// documented, tunable part of this configuration rather than implicit).
	healthCheckPeriod = time.Minute

	// pingTimeout bounds the startup connectivity check in NewPool.
	pingTimeout = 3 * time.Second
)

// NewPool opens a pgx connection pool to url and verifies connectivity with
// a bounded Ping before returning, so callers (cmd/api) fail fast at
// startup instead of discovering an unreachable database on the first
// request.
func NewPool(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("db: parse config: %w", err)
	}

	cfg.MaxConns = maxConns
	cfg.MaxConnLifetime = maxConnLifetime
	cfg.HealthCheckPeriod = healthCheckPeriod

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: open pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}

	return pool, nil
}
