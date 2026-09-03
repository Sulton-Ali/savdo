// Command savdo is the admin CLI: database migrations today, seeding and
// owner-password reset in later phases.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/pressly/goose/v3"

	"github.com/Sulton-Ali/savdo/api/db"
)

const migrationsDir = "migrations"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "savdo:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: savdo migrate <up|down|status>")
	}

	switch args[0] {
	case "migrate":
		return runMigrate(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runMigrate(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: savdo migrate <up|down|status>")
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set dialect: %w", err)
	}
	goose.SetBaseFS(db.Migrations)

	ctx := context.Background()

	var runErr error
	switch args[0] {
	case "up":
		runErr = goose.UpContext(ctx, conn, migrationsDir)
	case "down":
		runErr = goose.DownContext(ctx, conn, migrationsDir)
	case "status":
		runErr = goose.StatusContext(ctx, conn, migrationsDir)
	default:
		return fmt.Errorf("unknown migrate command %q", args[0])
	}

	// An empty db/migrations directory (true today, before Phase 1 adds the
	// first migration) makes goose return ErrNoMigrationFiles for up, down
	// and status alike. That is not a failure for this repo: report it and
	// exit 0 instead of propagating a hard error.
	if errors.Is(runErr, goose.ErrNoMigrationFiles) {
		fmt.Println("savdo: no migrations found, database is at version 0")
		return nil
	}
	return runErr
}
