package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

func TestNewPool_opensAWorkingConfiguredPool(t *testing.T) {
	dsn := testdb.DSN(t)

	pool, err := db.NewPool(context.Background(), dsn)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Close()

	cfg := pool.Config()
	if cfg.MaxConns != 10 {
		t.Fatalf("MaxConns = %d, want 10", cfg.MaxConns)
	}
	if cfg.MaxConnLifetime != 30*time.Minute {
		t.Fatalf("MaxConnLifetime = %v, want 30m", cfg.MaxConnLifetime)
	}
	if cfg.HealthCheckPeriod == 0 {
		t.Fatalf("HealthCheckPeriod = 0, want a configured period")
	}

	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
}

func TestNewPool_failsFastAgainstAnUnreachableDatabase(t *testing.T) {
	start := time.Now()
	pool, err := db.NewPool(context.Background(), "postgres://savdo:savdo@127.0.0.1:5999/savdo?sslmode=disable")
	if err == nil {
		pool.Close()
		t.Fatal("NewPool() error = nil, want an error connecting to a closed port")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("NewPool() took %v, want it to fail within its own ping timeout", elapsed)
	}
}

func TestNewPool_rejectsAMalformedConnectionString(t *testing.T) {
	if _, err := db.NewPool(context.Background(), "not a valid url"); err == nil {
		t.Fatal("NewPool() error = nil, want a parse error")
	}
}
