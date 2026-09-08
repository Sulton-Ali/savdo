package public_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

func mustGetPublicShop(ctx context.Context, t *testing.T, h interface {
	GetPublicShop(context.Context, gen.GetPublicShopRequestObject) (gen.GetPublicShopResponseObject, error)
}) gen.PublicShop {
	t.Helper()
	resp, err := h.GetPublicShop(ctx, gen.GetPublicShopRequestObject{})
	if err != nil {
		t.Fatalf("GetPublicShop: %v", err)
	}
	shop, ok := resp.(gen.GetPublicShop200JSONResponse)
	if !ok {
		t.Fatalf("GetPublicShop response type = %T", resp)
	}
	return gen.PublicShop(shop)
}

func TestGetPublicShop_unknownSlug_404(t *testing.T) {
	h, _, _, q, _ := newTestHandler(t, "no-such-shop")
	seedShop(context.Background(), t, q, "some-other-shop")

	_, err := h.GetPublicShop(context.Background(), gen.GetPublicShopRequestObject{})
	assertNotFound(t, "GetPublicShop", err)
}

func TestGetPublicShop_allBlocksInRequestedLocale_noFallback(t *testing.T) {
	h, _, contentSvc, q, _ := newTestHandler(t, "shop-a")
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	user := seedUser(ctx, t, q, shopRow.ID, "owner")

	put := func(key gen.ContentKey, data map[string]interface{}) {
		if _, err := contentSvc.Upsert(ctx, shopRow.ID, key, gen.LocaleRu, data, user.ID); err != nil {
			t.Fatalf("Upsert(%s, ru): %v", key, err)
		}
	}
	put(gen.Hero, map[string]interface{}{"title": "Ru Hero"})
	put(gen.About, map[string]interface{}{"body": "Ru About"})
	put(gen.Hours, validHoursData())
	put(gen.Contacts, map[string]interface{}{"phone": "+998901234567", "address": "Tashkent"})
	put(gen.Social, map[string]interface{}{"telegram": "https://t.me/shop"})
	put(gen.Seo, map[string]interface{}{"title": "T", "description": "D"})

	out := mustGetPublicShop(ctxWithAcceptLanguage("ru"), t, h)
	if out.Locale != gen.LocaleRu {
		t.Errorf("Locale = %v, want ru", out.Locale)
	}
	if out.TranslationFallback {
		t.Errorf("TranslationFallback = true, want false (every block saved in ru)")
	}
	if out.Blocks.Hero == nil || out.Blocks.Hero.Title != "Ru Hero" {
		t.Errorf("Blocks.Hero = %+v, want the ru hero", out.Blocks.Hero)
	}
	if out.Blocks.About == nil || out.Blocks.About.Body != "Ru About" {
		t.Errorf("Blocks.About = %+v, want the ru about", out.Blocks.About)
	}
	if out.Blocks.Hours == nil || len(out.Blocks.Hours.Days) != 7 {
		t.Errorf("Blocks.Hours = %+v, want 7 days", out.Blocks.Hours)
	}
	if out.Blocks.Contacts == nil || out.Blocks.Contacts.Phone != "+998901234567" {
		t.Errorf("Blocks.Contacts = %+v, want the saved phone", out.Blocks.Contacts)
	}
	if out.Blocks.Social == nil || out.Blocks.Social.Telegram == nil {
		t.Errorf("Blocks.Social = %+v, want the saved telegram link", out.Blocks.Social)
	}
	if out.Blocks.Seo == nil || out.Blocks.Seo.Title != "T" {
		t.Errorf("Blocks.Seo = %+v, want the saved seo title", out.Blocks.Seo)
	}
}

func TestGetPublicShop_missingRequestedLocale_fallsBackToUz(t *testing.T) {
	h, _, contentSvc, q, _ := newTestHandler(t, "shop-b")
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-b")
	user := seedUser(ctx, t, q, shopRow.ID, "owner")

	if _, err := contentSvc.Upsert(ctx, shopRow.ID, gen.Hero, gen.LocaleUz, map[string]interface{}{"title": "Uz Hero"}, user.ID); err != nil {
		t.Fatalf("Upsert(hero, uz): %v", err)
	}

	out := mustGetPublicShop(ctxWithAcceptLanguage("ru"), t, h)
	if out.Locale != gen.LocaleRu {
		t.Errorf("Locale = %v, want ru (the requested locale, even though hero fell back)", out.Locale)
	}
	if !out.TranslationFallback {
		t.Errorf("TranslationFallback = false, want true (hero has no ru row)")
	}
	if out.Blocks.Hero == nil || out.Blocks.Hero.Title != "Uz Hero" {
		t.Errorf("Blocks.Hero = %+v, want the uz hero (D-104 fallback)", out.Blocks.Hero)
	}
	if out.Blocks.About != nil {
		t.Errorf("Blocks.About = %+v, want absent (never saved in any locale)", out.Blocks.About)
	}
}

func TestGetPublicShop_heroImage_resolvesAndToleratesMissingMedia(t *testing.T) {
	h, _, contentSvc, q, _ := newTestHandler(t, "shop-c")
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-c")
	user := seedUser(ctx, t, q, shopRow.ID, "owner")
	mediaFile := seedMedia(ctx, t, q, shopRow.ID, "shop-c/hero")

	if _, err := contentSvc.Upsert(ctx, shopRow.ID, gen.Hero, gen.LocaleUz, map[string]interface{}{
		"title": "Hero", "imageMediaId": mediaFile.ID.String(),
	}, user.ID); err != nil {
		t.Fatalf("Upsert(hero): %v", err)
	}

	out := mustGetPublicShop(ctxWithAcceptLanguage("uz"), t, h)
	if out.Blocks.Hero == nil || out.Blocks.Hero.Image == nil {
		t.Fatalf("Blocks.Hero.Image = nil, want the resolved MediaUrls set")
	}
	if out.Blocks.Hero.Image.Card == "" {
		t.Errorf("Blocks.Hero.Image.Card = %q, want a non-empty URL", out.Blocks.Hero.Image.Card)
	}

	// A hero saved with an imageMediaId that names no media at all (the
	// media file was deleted after the block was saved) must not 500 —
	// the image is simply absent (O-21's own "absent when... no longer
	// exists").
	if _, err := contentSvc.Upsert(ctx, shopRow.ID, gen.Hero, gen.LocaleUz, map[string]interface{}{
		"title": "Hero", "imageMediaId": uuid.New().String(),
	}, user.ID); err != nil {
		t.Fatalf("Upsert(hero, dangling media): %v", err)
	}
	out2 := mustGetPublicShop(ctxWithAcceptLanguage("uz"), t, h)
	if out2.Blocks.Hero.Image != nil {
		t.Errorf("Blocks.Hero.Image = %+v, want absent for a dangling imageMediaId", out2.Blocks.Hero.Image)
	}
}

// seedUser creates a real users row — content_blocks.updated_by is a
// foreign key, so Upsert's userID must name an existing row (mirrors
// content_test.go's own seedUser).
func seedUser(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, username string) db.User {
	t.Helper()
	user, err := q.CreateUser(ctx, db.CreateUserParams{
		ID: uuid.New(), ShopID: shopID, Username: username, PasswordHash: "x",
		FullName: "Test User " + username, Role: db.UserRoleOwner, Locale: "uz",
	})
	if err != nil {
		t.Fatalf("seedUser(%q): %v", username, err)
	}
	return user
}

func seedMedia(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, storageKey string) db.MediaFile {
	t.Helper()
	sha := uuid.New()
	m, err := q.CreateMediaFile(ctx, db.CreateMediaFileParams{
		ID: uuid.New(), ShopID: shopID, StorageKey: storageKey, Mime: "image/webp", SizeBytes: 1024, Sha256: sha[:],
	})
	if err != nil {
		t.Fatalf("seedMedia: %v", err)
	}
	return m
}

// validHoursData is a well-formed ContentHours instance (mirrors
// content_test.go's own helper).
func validHoursData() map[string]interface{} {
	days := make([]interface{}, 0, 7)
	for _, d := range []string{"mon", "tue", "wed", "thu", "fri", "sat"} {
		days = append(days, map[string]interface{}{"day": d, "closed": false, "open": "09:00", "close": "18:00"})
	}
	days = append(days, map[string]interface{}{"day": "sun", "closed": true})
	return map[string]interface{}{"days": days}
}
