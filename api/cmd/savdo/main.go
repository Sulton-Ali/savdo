// Command savdo is the admin CLI: database migrations, seeding the demo
// shop (D-30) and resetting the owner's password from the server (D-28).
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/pressly/goose/v3"

	"github.com/Sulton-Ali/savdo/api/db"
	apidb "github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/seed"
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
		return fmt.Errorf("usage: savdo <migrate|seed|reset-owner-password>")
	}

	switch args[0] {
	case "migrate":
		return runMigrate(args[1:])
	case "seed":
		return runSeed(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

// databaseURL is the one place every subcommand reads DATABASE_URL from,
// so the DSN source (and its "missing" error) is shared rather than
// repeated per subcommand.
func databaseURL() (string, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return "", fmt.Errorf("DATABASE_URL is required")
	}
	return dsn, nil
}

func runMigrate(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: savdo migrate <up|down|status>")
	}

	dsn, err := databaseURL()
	if err != nil {
		return err
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

// openPool opens a pgxpool.Pool against dsn, verifying connectivity first
// (internal/db.NewPool), for the subcommands that run sqlc-generated
// queries rather than driving goose directly like runMigrate does.
func openPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := apidb.NewPool(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	return pool, nil
}

func runSeed(args []string) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	shopSlug := fs.String("shop-slug", seed.DefaultShopSlug, "shop slug to seed")
	force := fs.Bool("force", false, "seed even when ENV=prod")
	if err := fs.Parse(args); err != nil {
		return err
	}

	dsn, err := databaseURL()
	if err != nil {
		return err
	}

	env := os.Getenv("ENV")
	if env == "" {
		env = "dev"
	}
	if env == "prod" && !*force {
		return fmt.Errorf("refusing to seed: ENV=prod (pass --force to seed a production database anyway)")
	}

	ctx := context.Background()
	pool, err := openPool(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	report, err := seed.Seed(ctx, pool, *shopSlug)
	if err != nil {
		return err
	}

	for _, entity := range report.Entities {
		status := "exists"
		if entity.Created {
			status = "created"
		}
		fmt.Printf("%s: %s (%s)\n", entity.Kind, entity.Name, status)
	}
	return nil
}
