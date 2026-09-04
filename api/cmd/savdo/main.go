// Command savdo is the admin CLI: database migrations, seeding the demo
// shop (D-30) and resetting the owner's password from the server (D-28).
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/pressly/goose/v3"

	"github.com/Sulton-Ali/savdo/api/db"
	"github.com/Sulton-Ali/savdo/api/internal/catalog"
	"github.com/Sulton-Ali/savdo/api/internal/config"
	apidb "github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/media"
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
	case "reset-owner-password":
		return runResetOwnerPassword(args[1:])
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
// (internal/db.NewPool), for the two subcommands (seed, reset-owner-password)
// that run sqlc-generated queries rather than driving goose directly like
// runMigrate does.
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

	// config.Load, not the bare databaseURL() helper: the catalog seed
	// step below needs MEDIA_DIR/MEDIA_BASE_URL/MEDIA_MAX_BYTES (media.
	// Service's own constructor arguments, cmd/api/main.go's own doc
	// comment on cfg.MediaDir) alongside DATABASE_URL, so this is the one
	// subcommand that reads the full Config rather than DATABASE_URL alone.
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if cfg.Env == "prod" && !*force {
		return fmt.Errorf("refusing to seed: ENV=prod (pass --force to seed a production database anyway)")
	}

	ctx := context.Background()
	pool, err := openPool(ctx, cfg.DatabaseURL)
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

	catalogReport, err := runSeedCatalog(ctx, pool, cfg, report.ShopID)
	if err != nil {
		return fmt.Errorf("seed catalog: %w", err)
	}
	fmt.Printf("catalog: %d units, %d attribute definitions, %d categories, %d products, %d variants, %d images created\n",
		catalogReport.UnitsCreated, catalogReport.AttributesCreated, catalogReport.CategoriesCreated,
		catalogReport.ProductsCreated, catalogReport.VariantsCreated, catalogReport.ImagesCreated)
	return nil
}

// runSeedCatalog wires the catalog service and the media pipeline exactly
// as cmd/api does (see its own main.go), then calls seed.Catalog —
// the units/attribute-definitions/categories/products/variants/images
// pass documented on seed.Seed's own doc comment. MEDIA_DIR must be set
// (or left at its default, ../infra/data/media, relative to this
// process's working directory — `make seed` sources infra/.env, which
// sets it, before running `go run ./cmd/savdo seed` from api/) for the
// generated placeholder images to have anywhere to be stored.
func runSeedCatalog(ctx context.Context, pool *pgxpool.Pool, cfg config.Config, shopID uuid.UUID) (seed.CatalogReport, error) {
	q := apidb.New(pool)

	shopRow, err := q.GetShop(ctx, shopID)
	if err != nil {
		return seed.CatalogReport{}, fmt.Errorf("get shop: %w", err)
	}
	owner, err := q.GetOwner(ctx, shopID)
	if err != nil {
		return seed.CatalogReport{}, fmt.Errorf("get owner: %w", err)
	}

	mediaStorage, err := media.NewLocalStorage(cfg.MediaDir, cfg.MediaBaseURL)
	if err != nil {
		return seed.CatalogReport{}, fmt.Errorf("open media storage at MEDIA_DIR=%q: %w", cfg.MediaDir, err)
	}
	mediaSvc := media.NewService(q, mediaStorage, cfg.MediaBaseURL, cfg.MediaMaxBytes, cfg.MediaConcurrency, cfg.MediaQueue)

	catalogSvc := catalog.NewService(pool, q, shopRow.DefaultLocale, cfg.MediaBaseURL)
	catalogHandler := catalog.NewHandler(catalogSvc)

	return seed.Catalog(ctx, q, catalogHandler, mediaSvc, shopID, owner.ID)
}

func runResetOwnerPassword(args []string) error {
	fs := flag.NewFlagSet("reset-owner-password", flag.ContinueOnError)
	shopSlug := fs.String("shop-slug", seed.DefaultShopSlug, "shop slug whose owner to reset")
	passwordStdin := fs.Bool("password-stdin", false, "read the new password from stdin")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Only --password-stdin is implemented. An interactive, no-echo prompt
	// needs golang.org/x/term, which is not a dependency of this module
	// (AGENTS.md hard rule 11: no new dependency without owner approval) —
	// so, rather than silently echoing a typed password to the terminal,
	// this subcommand refuses to run without --password-stdin.
	if !*passwordStdin {
		return fmt.Errorf("reset-owner-password: --password-stdin is required " +
			"(no interactive prompt: golang.org/x/term is not yet a dependency of this module)")
	}

	password, err := readPasswordStdin(os.Stdin)
	if err != nil {
		return err
	}

	dsn, err := databaseURL()
	if err != nil {
		return err
	}

	ctx := context.Background()
	pool, err := openPool(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	result, err := seed.ResetOwnerPassword(ctx, pool, *shopSlug, password)
	if err != nil {
		return err
	}

	fmt.Printf("owner password updated, %d sessions revoked\n", result.RevokedSessions)
	return nil
}

// readPasswordStdin reads the whole of r as the new password, trimming a
// trailing line ending (so `printf 'pw\n' | savdo ...` and a file with no
// trailing newline both work) and never echoing or logging what it read.
func readPasswordStdin(r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("read password from stdin: %w", err)
	}
	password := strings.TrimRight(string(data), "\r\n")
	if password == "" {
		return "", fmt.Errorf("no password read from stdin")
	}
	return password, nil
}
