package content_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/content"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

// newTestHandler builds a content.Handler backed by a real (testcontainers)
// Postgres, truncated for isolation — mirrors crm.newTestHandler.
func newTestHandler(t *testing.T) (*content.Handler, *content.Service, *db.Queries) {
	t.Helper()
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	q := db.New(pool)
	svc := content.NewService(q)
	return content.NewHandler(svc), svc, q
}

func seedShop(ctx context.Context, t *testing.T, q *db.Queries, slug string) db.Shop {
	t.Helper()
	shopRow, err := q.CreateShop(ctx, db.CreateShopParams{ID: uuid.New(), Slug: slug, Name: "Content Shop " + slug})
	if err != nil {
		t.Fatalf("seedShop(%q): %v", slug, err)
	}
	return shopRow
}

// seedUser creates a real users row — content_blocks.updated_by is a
// foreign key (04-DATA-MODEL.md § 6), so ctxAs's UserID must name an
// existing row, unlike crm's tests (suppliers/customers have no such FK).
func seedUser(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, username string, role db.UserRole) db.User {
	t.Helper()
	hash, err := auth.Hash("correct-horse-battery")
	if err != nil {
		t.Fatalf("seedUser: Hash: %v", err)
	}
	user, err := q.CreateUser(ctx, db.CreateUserParams{
		ID: uuid.New(), ShopID: shopID, Username: username, PasswordHash: hash,
		FullName: "Content Test User " + username, Role: role, Locale: "uz",
	})
	if err != nil {
		t.Fatalf("seedUser(%q): %v", username, err)
	}
	return user
}

func ctxAs(shopID uuid.UUID, user db.User) context.Context {
	return auth.WithContext(context.Background(), auth.Context{
		ShopID: shopID, UserID: user.ID, Role: user.Role, SessionID: uuid.New(), Client: db.SessionClientWeb,
	})
}

// validHoursData is a well-formed ContentHours instance (D-107): all
// seven distinct weekdays, one closed with no open/close, the rest
// 09:00-18:00.
func validHoursData() map[string]interface{} {
	days := make([]interface{}, 0, 7)
	for _, d := range []string{"mon", "tue", "wed", "thu", "fri", "sat"} {
		days = append(days, map[string]interface{}{"day": d, "closed": false, "open": "09:00", "close": "18:00"})
	}
	days = append(days, map[string]interface{}{"day": "sun", "closed": true})
	return map[string]interface{}{"days": days, "note": "Public holidays: closed"}
}

func putContent(ctx context.Context, t *testing.T, h *content.Handler, key gen.ContentKey, locale gen.Locale, data map[string]interface{}) (gen.ContentBlock, error) {
	t.Helper()
	resp, err := h.PutContent(ctx, gen.PutContentRequestObject{Key: key, Body: &gen.ContentPut{Locale: locale, Data: data}})
	if err != nil {
		return gen.ContentBlock{}, err
	}
	created, ok := resp.(gen.PutContent200JSONResponse)
	if !ok {
		t.Fatalf("PutContent(%s/%s) response type = %T", key, locale, resp)
	}
	return gen.ContentBlock(created), nil
}

func TestPutContent_thenGetContent_perLocale(t *testing.T) {
	h, _, q := newTestHandler(t)
	shopRow := seedShop(context.Background(), t, q, "put-get")
	owner := seedUser(context.Background(), t, q, shopRow.ID, "owner", db.UserRoleOwner)
	ctx := ctxAs(shopRow.ID, owner)

	uzBlock, err := putContent(ctx, t, h, gen.Hero, gen.LocaleUz, map[string]interface{}{"title": "Bizning do'kon"})
	if err != nil {
		t.Fatalf("PutContent(hero, uz): %v", err)
	}
	if uzBlock.Data["title"] != "Bizning do'kon" {
		t.Fatalf("uz title = %v, want %q", uzBlock.Data["title"], "Bizning do'kon")
	}
	if uzBlock.UpdatedBy == nil || *uzBlock.UpdatedBy != owner.ID {
		t.Fatalf("UpdatedBy = %v, want %s", uzBlock.UpdatedBy, owner.ID)
	}

	if _, err := putContent(ctx, t, h, gen.Hero, gen.LocaleRu, map[string]interface{}{"title": "Наш магазин", "tagline": "Лучший выбор"}); err != nil {
		t.Fatalf("PutContent(hero, ru): %v", err)
	}

	resp, err := h.GetContent(ctx, gen.GetContentRequestObject{Key: gen.Hero})
	if err != nil {
		t.Fatalf("GetContent(hero): %v", err)
	}
	resource, ok := resp.(gen.GetContent200JSONResponse)
	if !ok {
		t.Fatalf("GetContent(hero) response type = %T", resp)
	}
	if resource.Locales.Uz == nil || resource.Locales.Uz.Data["title"] != "Bizning do'kon" {
		t.Fatalf("Locales.Uz = %+v, want the saved uz block", resource.Locales.Uz)
	}
	if resource.Locales.Ru == nil || resource.Locales.Ru.Data["title"] != "Наш магазин" {
		t.Fatalf("Locales.Ru = %+v, want the saved ru block", resource.Locales.Ru)
	}
	if resource.Locales.En != nil {
		t.Fatalf("Locales.En = %+v, want absent (never saved)", resource.Locales.En)
	}
}

func TestPutContent_savingOneLocaleLeavesAnotherUntouched(t *testing.T) {
	h, _, q := newTestHandler(t)
	shopRow := seedShop(context.Background(), t, q, "locale-isolation")
	owner := seedUser(context.Background(), t, q, shopRow.ID, "owner", db.UserRoleOwner)
	ctx := ctxAs(shopRow.ID, owner)

	if _, err := putContent(ctx, t, h, gen.Seo, gen.LocaleUz, map[string]interface{}{"title": "Uz sarlavha", "description": "Uz tavsif"}); err != nil {
		t.Fatalf("PutContent(seo, uz): %v", err)
	}
	if _, err := putContent(ctx, t, h, gen.Seo, gen.LocaleEn, map[string]interface{}{"title": "En title", "description": "En description"}); err != nil {
		t.Fatalf("PutContent(seo, en): %v", err)
	}

	resp, err := h.GetContent(ctx, gen.GetContentRequestObject{Key: gen.Seo})
	if err != nil {
		t.Fatalf("GetContent(seo): %v", err)
	}
	resource := resp.(gen.GetContent200JSONResponse)
	if resource.Locales.Uz == nil || resource.Locales.Uz.Data["title"] != "Uz sarlavha" {
		t.Fatalf("Locales.Uz = %+v, want unchanged uz block", resource.Locales.Uz)
	}
	if resource.Locales.En == nil || resource.Locales.En.Data["title"] != "En title" {
		t.Fatalf("Locales.En = %+v, want the new en block", resource.Locales.En)
	}
}

func TestPutContent_validationFailures(t *testing.T) {
	h, _, q := newTestHandler(t)
	shopRow := seedShop(context.Background(), t, q, "validation")
	owner := seedUser(context.Background(), t, q, shopRow.ID, "owner", db.UserRoleOwner)
	ctx := ctxAs(shopRow.ID, owner)

	tests := []struct {
		name       string
		key        gen.ContentKey
		data       map[string]interface{}
		wantFields []string
	}{
		{
			name:       "hero missing required title",
			key:        gen.Hero,
			data:       map[string]interface{}{"tagline": "no title here"},
			wantFields: []string{"title"},
		},
		{
			name:       "hero unknown field",
			key:        gen.Hero,
			data:       map[string]interface{}{"title": "OK", "unexpectedField": "nope"},
			wantFields: []string{"unexpectedField"},
		},
		{
			name: "hours bad day set — duplicate weekday, missing one",
			key:  gen.Hours,
			data: map[string]interface{}{"days": []interface{}{
				map[string]interface{}{"day": "mon", "closed": false, "open": "09:00", "close": "18:00"},
				map[string]interface{}{"day": "mon", "closed": false, "open": "09:00", "close": "18:00"},
				map[string]interface{}{"day": "tue", "closed": false, "open": "09:00", "close": "18:00"},
				map[string]interface{}{"day": "wed", "closed": false, "open": "09:00", "close": "18:00"},
				map[string]interface{}{"day": "thu", "closed": false, "open": "09:00", "close": "18:00"},
				map[string]interface{}{"day": "fri", "closed": false, "open": "09:00", "close": "18:00"},
				map[string]interface{}{"day": "sat", "closed": false, "open": "09:00", "close": "18:00"},
			}},
			wantFields: []string{"days"},
		},
		{
			name: "hours open not before close",
			key:  gen.Hours,
			data: func() map[string]interface{} {
				d := validHoursData()
				days := d["days"].([]interface{})
				days[0] = map[string]interface{}{"day": "mon", "closed": false, "open": "18:00", "close": "09:00"}
				return d
			}(),
			wantFields: []string{"days[0].close"},
		},
		{
			name: "hours open missing when not closed",
			key:  gen.Hours,
			data: func() map[string]interface{} {
				d := validHoursData()
				days := d["days"].([]interface{})
				days[0] = map[string]interface{}{"day": "mon", "closed": false, "close": "18:00"}
				return d
			}(),
			wantFields: []string{"days[0].open"},
		},
		{
			name:       "contacts non-https mapUrl",
			key:        gen.Contacts,
			data:       map[string]interface{}{"phone": "+998901234567", "address": "Tashkent", "mapUrl": "http://yandex.uz/maps/1"},
			wantFields: []string{"mapUrl"},
		},
		{
			name:       "seo missing required description",
			key:        gen.Seo,
			data:       map[string]interface{}{"title": "Title only"},
			wantFields: []string{"description"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := putContent(ctx, t, h, tt.key, gen.LocaleUz, tt.data)
			if err == nil {
				t.Fatalf("want 422 VALIDATION_FAILED, got no error")
			}
			var apiErr *apierr.Error
			if !errors.As(err, &apiErr) || apiErr.Status != 422 || apiErr.Code != gen.VALIDATIONFAILED {
				t.Fatalf("error = %v, want 422 VALIDATION_FAILED", err)
			}
			fields, _ := apiErr.Details["fields"].(map[string]string)
			for _, want := range tt.wantFields {
				if _, ok := fields[want]; !ok {
					t.Fatalf("details.fields = %v, want a %q entry", fields, want)
				}
			}
		})
	}
}

func TestPutContent_hoursValidData(t *testing.T) {
	h, _, q := newTestHandler(t)
	shopRow := seedShop(context.Background(), t, q, "hours-valid")
	owner := seedUser(context.Background(), t, q, shopRow.ID, "owner", db.UserRoleOwner)
	ctx := ctxAs(shopRow.ID, owner)

	block, err := putContent(ctx, t, h, gen.Hours, gen.LocaleUz, validHoursData())
	if err != nil {
		t.Fatalf("PutContent(hours): %v", err)
	}
	days, ok := block.Data["days"].([]interface{})
	if !ok || len(days) != 7 {
		t.Fatalf("Data[days] = %v, want 7 items", block.Data["days"])
	}
}

func TestGetContent_and_PutContent_unknownKeyIs400(t *testing.T) {
	h, _, q := newTestHandler(t)
	shopRow := seedShop(context.Background(), t, q, "unknown-key")
	owner := seedUser(context.Background(), t, q, shopRow.ID, "owner", db.UserRoleOwner)
	ctx := ctxAs(shopRow.ID, owner)

	_, err := h.GetContent(ctx, gen.GetContentRequestObject{Key: gen.ContentKey("bogus")})
	assertValidation(t, "GetContent", err, 400)

	_, err = h.PutContent(ctx, gen.PutContentRequestObject{
		Key: gen.ContentKey("bogus"), Body: &gen.ContentPut{Locale: gen.LocaleUz, Data: map[string]interface{}{"title": "x"}},
	})
	assertValidation(t, "PutContent", err, 400)
}

func TestPutContent_unknownLocaleIs400(t *testing.T) {
	h, _, q := newTestHandler(t)
	shopRow := seedShop(context.Background(), t, q, "unknown-locale")
	owner := seedUser(context.Background(), t, q, shopRow.ID, "owner", db.UserRoleOwner)
	ctx := ctxAs(shopRow.ID, owner)

	_, err := h.PutContent(ctx, gen.PutContentRequestObject{
		Key: gen.Seo, Body: &gen.ContentPut{Locale: gen.Locale("fr"), Data: map[string]interface{}{"title": "x", "description": "y"}},
	})
	assertValidation(t, "PutContent", err, 400)
}

func assertValidation(t *testing.T, label string, err error, wantStatus int) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: want %d VALIDATION_FAILED, got no error", label, wantStatus)
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != wantStatus || apiErr.Code != gen.VALIDATIONFAILED {
		t.Fatalf("%s: error = %v, want %d VALIDATION_FAILED", label, err, wantStatus)
	}
}

func TestContent_cashierForbiddenOnBothRoutes(t *testing.T) {
	h, _, q := newTestHandler(t)
	shopRow := seedShop(context.Background(), t, q, "content-cashier")
	owner := seedUser(context.Background(), t, q, shopRow.ID, "owner", db.UserRoleOwner)
	cashierUser := seedUser(context.Background(), t, q, shopRow.ID, "cashier", db.UserRoleCashier)
	ownerCtx := ctxAs(shopRow.ID, owner)
	cashierCtx := ctxAs(shopRow.ID, cashierUser)

	if _, err := putContent(ownerCtx, t, h, gen.Seo, gen.LocaleUz, map[string]interface{}{"title": "T", "description": "D"}); err != nil {
		t.Fatalf("PutContent(seo, owner): %v", err)
	}

	assertForbidden(t, "GetContent", func() error {
		_, err := h.GetContent(cashierCtx, gen.GetContentRequestObject{Key: gen.Seo})
		return err
	})
	assertForbidden(t, "PutContent", func() error {
		_, err := h.PutContent(cashierCtx, gen.PutContentRequestObject{
			Key: gen.Seo, Body: &gen.ContentPut{Locale: gen.LocaleUz, Data: map[string]interface{}{"title": "T", "description": "D"}},
		})
		return err
	})
}

func assertForbidden(t *testing.T, label string, call func() error) {
	t.Helper()
	err := call()
	if err == nil {
		t.Fatalf("%s: want 403 FORBIDDEN, got no error", label)
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 403 {
		t.Fatalf("%s: error = %v, want 403 FORBIDDEN", label, err)
	}
}

func TestResolve_fallbackAndMissing(t *testing.T) {
	_, svc, q := newTestHandler(t)
	shopRow := seedShop(context.Background(), t, q, "resolve")
	owner := seedUser(context.Background(), t, q, shopRow.ID, "owner", db.UserRoleOwner)

	h := content.NewHandler(svc)
	ctx := ctxAs(shopRow.ID, owner)

	// hero: uz only -> ru resolves via fallback, flagged.
	if _, err := putContent(ctx, t, h, gen.Hero, gen.LocaleUz, map[string]interface{}{"title": "Uz Hero"}); err != nil {
		t.Fatalf("PutContent(hero, uz): %v", err)
	}
	// seo: both uz and en saved -> en resolves to its own row, no fallback.
	if _, err := putContent(ctx, t, h, gen.Seo, gen.LocaleUz, map[string]interface{}{"title": "Uz Seo", "description": "D"}); err != nil {
		t.Fatalf("PutContent(seo, uz): %v", err)
	}
	if _, err := putContent(ctx, t, h, gen.Seo, gen.LocaleEn, map[string]interface{}{"title": "En Seo", "description": "D"}); err != nil {
		t.Fatalf("PutContent(seo, en): %v", err)
	}
	// about: never saved in any locale -> missing.

	resolved, err := svc.Resolve(context.Background(), shopRow.ID, gen.LocaleRu)
	if err != nil {
		t.Fatalf("Resolve(ru): %v", err)
	}

	hero := resolved[gen.Hero]
	if hero.Data == nil || hero.Data["title"] != "Uz Hero" || !hero.TranslationFallback || hero.Locale != gen.LocaleUz {
		t.Fatalf("hero = %+v, want uz fallback, flagged", hero)
	}

	about := resolved[gen.About]
	if about.Data != nil {
		t.Fatalf("about = %+v, want no data (never saved in any locale)", about)
	}

	resolvedEn, err := svc.Resolve(context.Background(), shopRow.ID, gen.LocaleEn)
	if err != nil {
		t.Fatalf("Resolve(en): %v", err)
	}
	seo := resolvedEn[gen.Seo]
	if seo.Data == nil || seo.Data["title"] != "En Seo" || seo.TranslationFallback || seo.Locale != gen.LocaleEn {
		t.Fatalf("seo = %+v, want its own en row, not flagged", seo)
	}
}
