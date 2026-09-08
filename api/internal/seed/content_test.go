package seed_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
	"github.com/Sulton-Ali/savdo/api/internal/seed"
)

// wantContentBlocks is contentBlockSpecs' total (task spec: uz for all six
// keys, plus ru for hero and seo only) — pinned here rather than reaching
// into seed's unexported slice.
const wantContentBlocks = 6 + 2

func TestContent_seedsAllBlocksIdempotently(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()

	shopReport, err := seed.Seed(ctx, pool, seed.DefaultShopSlug)
	if err != nil {
		t.Fatalf("Seed() error = %v", err)
	}
	q := db.New(pool)
	owner, err := q.GetOwner(ctx, shopReport.ShopID)
	if err != nil {
		t.Fatalf("GetOwner() error = %v", err)
	}

	first, err := seed.Content(ctx, q, shopReport.ShopID, owner.ID)
	if err != nil {
		t.Fatalf("first Content() error = %v", err)
	}
	if first.BlocksCreated != wantContentBlocks {
		t.Fatalf("first Content().BlocksCreated = %d, want %d", first.BlocksCreated, wantContentBlocks)
	}

	rows, err := q.ListContentBlocksForShop(ctx, shopReport.ShopID)
	if err != nil {
		t.Fatalf("ListContentBlocksForShop() error = %v", err)
	}
	if len(rows) != wantContentBlocks {
		t.Fatalf("len(rows) = %d, want %d", len(rows), wantContentBlocks)
	}

	// uz must carry all six keys.
	uzKeys := map[db.ContentBlockKey]bool{}
	ruKeys := map[db.ContentBlockKey]bool{}
	for _, r := range rows {
		switch r.Locale {
		case "uz":
			uzKeys[r.Key] = true
		case "ru":
			ruKeys[r.Key] = true
		default:
			t.Errorf("unexpected locale %q in seeded content blocks", r.Locale)
		}
	}
	for _, key := range []db.ContentBlockKey{
		db.ContentBlockKeyHero, db.ContentBlockKeyAbout, db.ContentBlockKeyHours,
		db.ContentBlockKeyContacts, db.ContentBlockKeySocial, db.ContentBlockKeySeo,
	} {
		if !uzKeys[key] {
			t.Errorf("uz is missing key %q", key)
		}
	}
	if len(ruKeys) != 2 || !ruKeys[db.ContentBlockKeyHero] || !ruKeys[db.ContentBlockKeySeo] {
		t.Fatalf("ru keys = %v, want exactly {hero, seo}", ruKeys)
	}

	// Second call: nothing new, no duplicates.
	second, err := seed.Content(ctx, q, shopReport.ShopID, owner.ID)
	if err != nil {
		t.Fatalf("second Content() error = %v", err)
	}
	if second.BlocksCreated != 0 {
		t.Fatalf("second Content().BlocksCreated = %d, want 0 (already seeded)", second.BlocksCreated)
	}
	rowsAfter, err := q.ListContentBlocksForShop(ctx, shopReport.ShopID)
	if err != nil {
		t.Fatalf("ListContentBlocksForShop() after second seed error = %v", err)
	}
	if len(rowsAfter) != wantContentBlocks {
		t.Fatalf("len(rowsAfter) = %d, want %d (no duplicates)", len(rowsAfter), wantContentBlocks)
	}
}

func TestContent_neverOverwritesAnEditedBlock(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()

	shopReport, err := seed.Seed(ctx, pool, seed.DefaultShopSlug)
	if err != nil {
		t.Fatalf("Seed() error = %v", err)
	}
	q := db.New(pool)
	owner, err := q.GetOwner(ctx, shopReport.ShopID)
	if err != nil {
		t.Fatalf("GetOwner() error = %v", err)
	}

	if _, err := seed.Content(ctx, q, shopReport.ShopID, owner.ID); err != nil {
		t.Fatalf("first Content() error = %v", err)
	}

	// Simulate the owner editing hero/uz through the (future) admin content
	// editor: this write goes through the same UpsertContentBlock query the
	// editor will use, no different from any other manual edit.
	edited := json.RawMessage(`{"title":"Owner Edited Title","tagline":"Owner Edited Tagline"}`)
	if _, err := q.UpsertContentBlock(ctx, db.UpsertContentBlockParams{
		ShopID: shopReport.ShopID, Key: db.ContentBlockKeyHero, Locale: "uz", Data: edited, UpdatedBy: &owner.ID,
	}); err != nil {
		t.Fatalf("simulate owner edit: %v", err)
	}

	if _, err := seed.Content(ctx, q, shopReport.ShopID, owner.ID); err != nil {
		t.Fatalf("re-running Content() after an edit: %v", err)
	}

	got, err := q.GetContentBlock(ctx, db.GetContentBlockParams{ShopID: shopReport.ShopID, Key: db.ContentBlockKeyHero, Locale: "uz"})
	if err != nil {
		t.Fatalf("GetContentBlock(hero/uz): %v", err)
	}
	var m map[string]string
	if err := json.Unmarshal(got.Data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["title"] != "Owner Edited Title" {
		t.Fatalf("hero/uz title = %q, want the owner's edit to survive re-seeding, not the seeded default", m["title"])
	}
}
