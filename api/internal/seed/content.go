package seed

// This file: demo landing content for the demo shop (docs/06-ROADMAP.md
// Phase 6, D-99, D-104), so the landing (web/) has something real to
// render against the seeded catalogue. Every block is written directly
// through db.Queries.UpsertContentBlock, not a content.Handler — Phase 6
// has not built that service yet (the same reasoning seedUnits' own doc
// comment gives for writing units directly: there is no service call to
// route this through, since GET /units/GET|PUT /content/{key} do not
// exist yet at the point this file was written). uz carries all six keys;
// ru carries only hero and seo, so D-104's fallback (requested -> uz ->
// any) has a real gap to fall back across on the landing.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// contentBlockSpec is one (key, locale) content_blocks row Content upserts.
type contentBlockSpec struct {
	key    db.ContentBlockKey
	locale string
	data   map[string]any
}

// weekdayHours is one row of the "hours" block's days array.
func weekdayHours(day string, closed bool, opensAt, closesAt string) map[string]any {
	row := map[string]any{"day": day, "closed": closed}
	if !closed {
		row["open"] = opensAt
		row["close"] = closesAt
	}
	return row
}

// contentBlockSpecs is the demo shop's seeded landing content: uz for
// every key (hero, about, hours, contacts, social, seo), plus ru for hero
// and seo only (task spec — exercises the D-104 fallback path for every
// other key/locale combination on the landing).
var contentBlockSpecs = []contentBlockSpec{
	{
		key: db.ContentBlockKeyHero, locale: "uz",
		data: map[string]any{
			"title":   "Savdo Demo — oilaviy kiyim doʻkoni",
			"tagline": "Erkaklar, ayollar va bolalar uchun sifatli kiyimlar, hamyonbop narxlarda",
		},
	},
	{
		key: db.ContentBlockKeyHero, locale: "ru",
		data: map[string]any{
			"title":   "Savdo Demo — семейный магазин одежды",
			"tagline": "Качественная одежда для всей семьи по доступным ценам",
		},
	},
	{
		key: db.ContentBlockKeyAbout, locale: "uz",
		data: map[string]any{
			"body": "Savdo Demo — Toshkentdagi oilaviy kiyim-kechak doʻkoni. Biz erkaklar, ayollar " +
				"va bolalar uchun kundalik va bayramona kiyimlarni tanlab, sifat va narx nisbatiga " +
				"eʼtibor bilan mijozlarimizga taqdim etamiz.",
		},
	},
	{
		key: db.ContentBlockKeyHours, locale: "uz",
		data: map[string]any{
			"days": []map[string]any{
				weekdayHours("mon", false, "09:00", "19:00"),
				weekdayHours("tue", false, "09:00", "19:00"),
				weekdayHours("wed", false, "09:00", "19:00"),
				weekdayHours("thu", false, "09:00", "19:00"),
				weekdayHours("fri", false, "09:00", "19:00"),
				weekdayHours("sat", false, "10:00", "18:00"),
				weekdayHours("sun", true, "", ""),
			},
			"note": "Bayram kunlari ish vaqti oʻzgarishi mumkin.",
		},
	},
	{
		key: db.ContentBlockKeyContacts, locale: "uz",
		data: map[string]any{
			"phone":   "+998901234567",
			"address": "Toshkent sh., Chilonzor tumani, Bunyodkor shoh koʻchasi, 12-uy",
			"mapUrl":  "https://yandex.com/maps/10335/tashkent/?ll=69.279737%2C41.311151&z=16",
		},
	},
	{
		key: db.ContentBlockKeySocial, locale: "uz",
		data: map[string]any{
			"telegram":  "https://t.me/savdo_demo",
			"instagram": "https://instagram.com/savdo_demo",
		},
	},
	{
		key: db.ContentBlockKeySeo, locale: "uz",
		data: map[string]any{
			"title":       "Savdo Demo — kiyim doʻkoni",
			"description": "Erkaklar, ayollar va bolalar uchun sifatli kiyimlar. Onlayn katalog va doʻkonda xarid.",
		},
	},
	{
		key: db.ContentBlockKeySeo, locale: "ru",
		data: map[string]any{
			"title":       "Savdo Demo — магазин одежды",
			"description": "Качественная одежда для мужчин, женщин и детей. Онлайн-каталог и покупка в магазине.",
		},
	},
}

// ContentReport summarizes what Content did, for the CLI's summary line
// and for tests. BlocksCreated counts only (key, locale) pairs that did
// not already exist — a second call against a fully-seeded shop reports 0,
// and never overwrites a block the owner has since edited through the
// admin content editor.
type ContentReport struct {
	BlocksCreated int
}

// Content upserts contentBlockSpecs for shopID, skipping any (key, locale)
// pair that already has a row — the same look-up-first, create-only-if-
// missing contract Seed/Catalog/Stock all use, so re-running `savdo seed`
// never clobbers a block the owner has since edited.
func Content(ctx context.Context, q *db.Queries, shopID, ownerID uuid.UUID) (ContentReport, error) {
	existing, err := q.ListContentBlocksForShop(ctx, shopID)
	if err != nil {
		return ContentReport{}, fmt.Errorf("seed content: list existing blocks: %w", err)
	}
	type blockKey struct {
		key    db.ContentBlockKey
		locale string
	}
	have := make(map[blockKey]bool, len(existing))
	for _, b := range existing {
		have[blockKey{b.Key, b.Locale}] = true
	}

	created := 0
	for _, spec := range contentBlockSpecs {
		if have[blockKey{spec.key, spec.locale}] {
			continue
		}

		data, err := json.Marshal(spec.data)
		if err != nil {
			return ContentReport{}, fmt.Errorf("seed content: marshal %s/%s: %w", spec.key, spec.locale, err)
		}
		owner := ownerID
		if _, err := q.UpsertContentBlock(ctx, db.UpsertContentBlockParams{
			ShopID: shopID, Key: spec.key, Locale: spec.locale, Data: data, UpdatedBy: &owner,
		}); err != nil {
			return ContentReport{}, fmt.Errorf("seed content: upsert %s/%s: %w", spec.key, spec.locale, err)
		}
		created++
	}
	return ContentReport{BlocksCreated: created}, nil
}
