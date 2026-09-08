package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

func TestContentBlocks_upsertUpdatesInPlace(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-content")
	owner, err := q.CreateUser(ctx, db.CreateUserParams{
		ID: uuid.New(), ShopID: shop.ID, Username: "owner", PasswordHash: "x", FullName: "Owner", Role: db.UserRoleOwner, Locale: "uz",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	first, err := q.UpsertContentBlock(ctx, db.UpsertContentBlockParams{
		ShopID: shop.ID, Key: db.ContentBlockKeyHero, Locale: "uz",
		Data: json.RawMessage(`{"title":"Salom"}`), UpdatedBy: &owner.ID,
	})
	if err != nil {
		t.Fatalf("UpsertContentBlock (create): %v", err)
	}

	got, err := q.GetContentBlock(ctx, db.GetContentBlockParams{ShopID: shop.ID, Key: db.ContentBlockKeyHero, Locale: "uz"})
	if err != nil {
		t.Fatalf("GetContentBlock: %v", err)
	}
	assertJSONField(t, got.Data, "title", "Salom")

	// Upsert again with new data for the same (shop, key, locale): the row
	// updates in place — same primary key, new data, updated_at moves
	// forward — not a second row.
	second, err := q.UpsertContentBlock(ctx, db.UpsertContentBlockParams{
		ShopID: shop.ID, Key: db.ContentBlockKeyHero, Locale: "uz",
		Data: json.RawMessage(`{"title":"Yangilangan"}`), UpdatedBy: &owner.ID,
	})
	if err != nil {
		t.Fatalf("UpsertContentBlock (update): %v", err)
	}
	assertJSONField(t, second.Data, "title", "Yangilangan")
	if !second.UpdatedAt.After(first.UpdatedAt) {
		t.Fatalf("UpdatedAt = %v, want after the first upsert's %v", second.UpdatedAt, first.UpdatedAt)
	}

	byKey, err := q.ListContentBlocksByKey(ctx, db.ListContentBlocksByKeyParams{ShopID: shop.ID, Key: db.ContentBlockKeyHero})
	if err != nil {
		t.Fatalf("ListContentBlocksByKey: %v", err)
	}
	if len(byKey) != 1 {
		t.Fatalf("want exactly one row for (shop, hero) after two upserts to the same locale, got %d", len(byKey))
	}
}

func TestContentBlocks_listByKeyAndForShop(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-content-list")

	upsert := func(key db.ContentBlockKey, locale, data string) {
		t.Helper()
		if _, err := q.UpsertContentBlock(ctx, db.UpsertContentBlockParams{
			ShopID: shop.ID, Key: key, Locale: locale, Data: json.RawMessage(data),
		}); err != nil {
			t.Fatalf("UpsertContentBlock(%s/%s): %v", key, locale, err)
		}
	}
	upsert(db.ContentBlockKeyHero, "uz", `{"title":"uz hero"}`)
	upsert(db.ContentBlockKeyHero, "ru", `{"title":"ru hero"}`)
	upsert(db.ContentBlockKeySeo, "uz", `{"title":"uz seo"}`)

	byKey, err := q.ListContentBlocksByKey(ctx, db.ListContentBlocksByKeyParams{ShopID: shop.ID, Key: db.ContentBlockKeyHero})
	if err != nil {
		t.Fatalf("ListContentBlocksByKey: %v", err)
	}
	if len(byKey) != 2 || byKey[0].Locale != "ru" || byKey[1].Locale != "uz" {
		t.Fatalf("ListContentBlocksByKey(hero) = %+v, want [ru, uz] ordered by locale", byKey)
	}

	forShop, err := q.ListContentBlocksForShop(ctx, shop.ID)
	if err != nil {
		t.Fatalf("ListContentBlocksForShop: %v", err)
	}
	if len(forShop) != 3 {
		t.Fatalf("ListContentBlocksForShop: want 3 rows (hero/uz, hero/ru, seo/uz), got %d: %+v", len(forShop), forShop)
	}

	// Cross-tenant isolation (hard rule 1): a second shop's blocks must
	// never leak into the first shop's reads.
	otherShop := catalogShop(ctx, t, q, "shop-content-other")
	if _, err := q.UpsertContentBlock(ctx, db.UpsertContentBlockParams{
		ShopID: otherShop.ID, Key: db.ContentBlockKeyHero, Locale: "uz", Data: json.RawMessage(`{"title":"other shop"}`),
	}); err != nil {
		t.Fatalf("UpsertContentBlock (other shop): %v", err)
	}
	forShop, err = q.ListContentBlocksForShop(ctx, shop.ID)
	if err != nil {
		t.Fatalf("ListContentBlocksForShop (recheck): %v", err)
	}
	if len(forShop) != 3 {
		t.Fatalf("ListContentBlocksForShop leaked another shop's row: got %d, want 3", len(forShop))
	}
}

func TestContentBlocks_keyCheckedByEnum(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := catalogShop(ctx, t, q, "shop-content-badkey")

	_, err := pool.Exec(ctx, `
		INSERT INTO content_blocks (shop_id, key, locale, data)
		VALUES ($1, 'not-a-real-key', 'uz', '{}')
	`, shop.ID)
	if err == nil {
		t.Fatal("want an error inserting an unrecognized content_block_key, got none")
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || !strings.Contains(pgErr.Message, "content_block_key") {
		t.Fatalf("want a content_block_key enum error, got: %v", err)
	}
}

// assertJSONField unmarshals data and asserts field's string value —
// Postgres re-serializes jsonb (e.g. adds a space after ':'), so a raw
// string comparison against the literal sent in would be brittle.
func assertJSONField(t *testing.T, data json.RawMessage, field, want string) {
	t.Helper()
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal %s: %v", data, err)
	}
	if m[field] != want {
		t.Fatalf("%s = %q, want %q (data: %s)", field, m[field], want, data)
	}
}
