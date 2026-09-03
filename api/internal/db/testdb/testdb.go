// Package testdb spins up a real PostgreSQL 18.6 (testcontainers-go,
// modules/postgres) for integration tests and applies the goose migrations
// embedded in api/db (db.Migrations), so tests run against the same schema
// production does — no mocks, no sqlite substitute.
package testdb

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver, for goose
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	savdodb "github.com/Sulton-Ali/savdo/api/db"
)

const (
	// SkipEnv, when set to "1", skips every test that calls New instead of
	// starting a container. Anything else (including "" / unset) means
	// "run for real"; if Docker is not available in that case, New fails
	// the test loudly rather than skipping it silently.
	SkipEnv = "TESTDB_SKIP"

	image             = "postgres:18.6"
	migrationsDir     = "migrations"
	containerUser     = "savdo"
	containerPassword = "savdo"
	containerDatabase = "savdo"
)

// One container and one pool for the entire test binary. Tests in the same
// package run against the same Postgres instance; use Truncate between
// tests for isolation instead of paying container-startup cost per test.
//
// Lifecycle: we deliberately do not call container.Terminate ourselves.
// testcontainers-go starts its own "Ryuk" reaper alongside every container
// it creates, and that reaper removes the container the moment this test
// process exits (crash, panic, t.Fatal, or a clean return from TestMain —
// all of them). A t.Cleanup on "the last test" cannot be expressed
// correctly with the stdlib testing package once tests run in parallel
// (cleanups fire per-test, not per-binary, and their relative order across
// independent Test functions is unspecified), so leaning on Ryuk is the
// more robust choice, not a shortcut: it is the one mechanism guaranteed to
// run exactly once, after the very last test, regardless of how the tests
// in this binary are structured. Local `docker ps` may show the container
// for a few seconds after `go test` exits while Ryuk finishes; that is
// expected.
var (
	once     sync.Once
	pool     *pgxpool.Pool
	dsn      string
	initErrs error
)

// New starts (once per test binary) a postgres:18.6 container, applies every
// goose migration embedded in api/db, and returns a pgxpool.Pool connected
// to it. Call Truncate between tests that need a clean database.
//
// Set TESTDB_SKIP=1 to skip tests that need a database (e.g. no Docker on
// this machine). Any other failure — Docker missing, image pull failure,
// migration failure — fails the test loudly via t.Fatal.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()

	if os.Getenv(SkipEnv) == "1" {
		t.Skip("testdb: TESTDB_SKIP=1, skipping test that needs Postgres")
	}

	once.Do(func() {
		p, d, err := start()
		if err != nil {
			initErrs = err
			return
		}
		pool, dsn = p, d
	})

	if initErrs != nil {
		t.Fatalf("testdb: start shared Postgres container: %v", initErrs)
	}
	return pool
}

func start() (*pgxpool.Pool, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	ctr, err := postgres.Run(ctx, image,
		postgres.WithDatabase(containerDatabase),
		postgres.WithUsername(containerUser),
		postgres.WithPassword(containerPassword),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		return nil, "", fmt.Errorf("run postgres container (is Docker running?): %w", err)
	}

	connStr, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, "", fmt.Errorf("container connection string: %w", err)
	}

	if err := migrateUp(ctx, connStr); err != nil {
		return nil, "", fmt.Errorf("apply migrations: %w", err)
	}

	p, err := pgxpool.New(ctx, connStr)
	if err != nil {
		return nil, "", fmt.Errorf("open pgxpool: %w", err)
	}
	return p, connStr, nil
}

// migrateUp applies every migration with the goose *library* (no goose
// binary, per 02-TECH-STACK.md), the same way cmd/savdo does. It opens its
// own database/sql handle because goose drives *sql.DB, not pgxpool.
func migrateUp(ctx context.Context, dsn string) error {
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()

	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	goose.SetBaseFS(savdodb.Migrations)

	return goose.UpContext(ctx, sqlDB, migrationsDir)
}

// MigrateDownAll rolls back every migration (goose down to version 0) then
// re-applies them, against the shared container's database. It is meant for
// a single dedicated test that proves Down is real SQL, not a comment — run
// it against a database nothing else is using at the same time, and call
// Truncate afterwards since Up recreates empty tables.
func MigrateDownAll(ctx context.Context, t *testing.T) {
	t.Helper()
	if dsn == "" {
		t.Fatal("testdb: MigrateDownAll called before New")
	}

	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("testdb: open sql.DB for migration test: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("testdb: set dialect: %v", err)
	}
	goose.SetBaseFS(savdodb.Migrations)

	if err := goose.DownToContext(ctx, sqlDB, migrationsDir, 0); err != nil {
		t.Fatalf("testdb: migrate down to 0: %v", err)
	}
	if err := goose.UpContext(ctx, sqlDB, migrationsDir); err != nil {
		t.Fatalf("testdb: migrate up after down: %v", err)
	}
}

// Truncate empties every application table (everything in schema "public"
// except goose's own version table) so the next test starts from an empty
// database. It discovers tables at run time instead of listing them by name
// so it keeps working as later phases add tables.
func Truncate(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	rows, err := pool.Query(ctx, `
		SELECT tablename FROM pg_tables
		WHERE schemaname = 'public' AND tablename <> 'goose_db_version'
	`)
	if err != nil {
		t.Fatalf("testdb: list tables to truncate: %v", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			t.Fatalf("testdb: scan table name: %v", err)
		}
		tables = append(tables, `"`+name+`"`)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("testdb: list tables to truncate: %v", err)
	}
	if len(tables) == 0 {
		return
	}

	stmt := "TRUNCATE TABLE " + strings.Join(tables, ", ") + " RESTART IDENTITY CASCADE"
	if _, err := pool.Exec(ctx, stmt); err != nil {
		t.Fatalf("testdb: truncate: %v", err)
	}
}
