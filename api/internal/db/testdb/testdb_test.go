package testdb_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

func TestNew_appliesAllMigrations(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)

	ctx := context.Background()
	var version int64
	err := pool.QueryRow(ctx, `SELECT version_id FROM goose_db_version ORDER BY id DESC LIMIT 1`).Scan(&version)
	if err != nil {
		t.Fatalf("read goose_db_version: %v", err)
	}
	if version != 23 {
		t.Fatalf("want migrations up to version 23, got %d", version)
	}

	// Every table the migrations create must exist and be queryable.
	for _, table := range []string{
		"shops", "users", "sessions", "locations",
		"units", "unit_translations",
		"attribute_definitions", "attribute_definition_translations",
		"categories", "category_translations",
		"products", "product_translations", "product_variants",
		"media_files", "product_images",
		"suppliers", "purchases", "purchase_items",
		"stock_movements", "stock_levels",
		"audit_log", "idempotency_keys",
		"customers", "sales", "sale_items", "sale_payments",
		"sale_drafts", "sale_draft_items",
		"content_blocks",
		"telegram_accounts", "otp_codes",
		"bot_conversations", "bot_messages",
	} {
		if _, err := pool.Exec(ctx, "SELECT 1 FROM "+table+" LIMIT 0"); err != nil {
			t.Fatalf("table %q not usable: %v", table, err)
		}
	}
}

func TestUsers_requireShopID(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, username, password_hash, full_name, role)
		VALUES ($1, 'nouser', 'hash', 'No Shop', 'owner')
	`, uuid.New())
	if err == nil {
		t.Fatal("want an error inserting a user without shop_id, got none")
	}
}

func TestUsers_usernameUniquePerShopNotGlobally(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()

	shopA := createShop(ctx, t, pool, "shop-a")
	shopB := createShop(ctx, t, pool, "shop-b")

	insertUser := func(shopID uuid.UUID) error {
		_, err := pool.Exec(ctx, `
			INSERT INTO users (id, shop_id, username, password_hash, full_name, role)
			VALUES ($1, $2, 'owner1', 'hash', 'Owner', 'owner')
		`, uuid.New(), shopID)
		return err
	}

	if err := insertUser(shopA); err != nil {
		t.Fatalf("insert user in shop A: %v", err)
	}
	if err := insertUser(shopB); err != nil {
		t.Fatalf("insert user with the same username in shop B should succeed (unique is per shop): %v", err)
	}
	if err := insertUser(shopA); err == nil {
		t.Fatal("want a uniqueness error inserting the same username twice in shop A, got none")
	}
}

func TestLocations_onlyOneDefaultPerShop(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()

	shopID := createShop(ctx, t, pool, "shop-locations")

	insertLocation := func(name string, isDefault bool) error {
		_, err := pool.Exec(ctx, `
			INSERT INTO locations (id, shop_id, name, kind, is_default)
			VALUES ($1, $2, $3, 'store', $4)
		`, uuid.New(), shopID, name, isDefault)
		return err
	}

	if err := insertLocation("Dokon", true); err != nil {
		t.Fatalf("insert first default location: %v", err)
	}
	if err := insertLocation("Ombor", false); err != nil {
		t.Fatalf("insert non-default location: %v", err)
	}
	if err := insertLocation("Filial", true); err == nil {
		t.Fatal("want a uniqueness error inserting a second default location for the same shop, got none")
	}
}

func TestMigrations_downAllThenUpAgain(t *testing.T) {
	_ = testdb.New(t) // ensure the container + first migration run happened
	ctx := context.Background()

	testdb.MigrateDownAll(ctx, t)

	pool := testdb.New(t)
	var version int64
	err := pool.QueryRow(ctx, `SELECT version_id FROM goose_db_version ORDER BY id DESC LIMIT 1`).Scan(&version)
	if err != nil {
		t.Fatalf("read goose_db_version after down+up: %v", err)
	}
	if version != 23 {
		t.Fatalf("want version 23 after down-all then up, got %d", version)
	}
	// Down recreated empty tables; leave the database clean for tests that
	// run after this one in the same binary.
	testdb.Truncate(t, pool)
}

func createShop(ctx context.Context, t *testing.T, pool *pgxpool.Pool, slug string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO shops (id, slug, name) VALUES ($1, $2, $3)
	`, id, slug, slug)
	if err != nil {
		t.Fatalf("create shop %q: %v", slug, err)
	}
	return id
}
