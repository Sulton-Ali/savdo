package catalog_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/catalog"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

// newTestHandler builds a catalog.Handler backed by a real (testcontainers)
// Postgres, truncated for isolation, with "uz" as the shop's default
// locale (matching every test shop this file seeds).
func newTestHandler(t *testing.T) (*catalog.Handler, *db.Queries, *pgxpool.Pool) {
	t.Helper()
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	q := db.New(pool)
	svc := catalog.NewService(pool, q, "uz", "/media")
	return catalog.NewHandler(svc), q, pool
}

func seedShop(ctx context.Context, t *testing.T, q *db.Queries, slug string) db.Shop {
	t.Helper()
	shopRow, err := q.CreateShop(ctx, db.CreateShopParams{ID: uuid.New(), Slug: slug, Name: "Test Shop " + slug})
	if err != nil {
		t.Fatalf("seedShop: %v", err)
	}
	return shopRow
}

func seedUnit(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, code string) db.Unit {
	t.Helper()
	u, err := q.UpsertUnit(ctx, db.UpsertUnitParams{ID: uuid.New(), ShopID: shopID, Code: code, Precision: 0})
	if err != nil {
		t.Fatalf("seedUnit: %v", err)
	}
	return u
}

func seedMedia(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, storageKey string) db.MediaFile {
	t.Helper()
	sha := uuid.New()
	m, err := q.CreateMediaFile(ctx, db.CreateMediaFileParams{
		ID: uuid.New(), ShopID: shopID, StorageKey: storageKey, Mime: "image/webp",
		SizeBytes: 1024, Sha256: sha[:],
	})
	if err != nil {
		t.Fatalf("seedMedia: %v", err)
	}
	return m
}

func seedAttribute(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, code string) db.AttributeDefinition {
	t.Helper()
	a, err := q.CreateAttributeDefinition(ctx, db.CreateAttributeDefinitionParams{ID: uuid.New(), ShopID: shopID, Code: code})
	if err != nil {
		t.Fatalf("seedAttribute: %v", err)
	}
	if err := q.UpsertAttributeDefinitionTranslation(ctx, db.UpsertAttributeDefinitionTranslationParams{
		AttributeDefinitionID: a.ID, Locale: "uz", Name: code,
	}); err != nil {
		t.Fatalf("seedAttribute translation: %v", err)
	}
	return a
}

// ctxAs builds a context carrying the auth.Context a real request would
// have after Middleware ran, for shopID as role — the same shape
// auth.FromContext reads everywhere in this package.
func ctxAs(shopID uuid.UUID, role db.UserRole) context.Context {
	return auth.WithContext(context.Background(), auth.Context{
		ShopID: shopID, UserID: uuid.New(), Role: role, SessionID: uuid.New(), Client: db.SessionClientWeb,
	})
}

func owner(shopID uuid.UUID) context.Context   { return ctxAs(shopID, db.UserRoleOwner) }
func manager(shopID uuid.UUID) context.Context { return ctxAs(shopID, db.UserRoleManager) }
func cashier(shopID uuid.UUID) context.Context { return ctxAs(shopID, db.UserRoleCashier) }

func strPtr(s string) *string { return &s }

// uzTranslations builds a minimal gen.Translations with only a `uz` entry
// — the one every Create needs to pass "default locale required".
func uzTranslations(name string) gen.Translations {
	return gen.Translations{Uz: &gen.TranslationEntry{Name: name}}
}

// numeric parses a decimal literal into a valid pgtype.Numeric, the way a
// caller supplying a NUMERIC(14,2) parameter would (mirrors
// internal/db's own catalog_test.go helper of the same name).
func numeric(t *testing.T, s string) pgtype.Numeric {
	t.Helper()
	var n pgtype.Numeric
	if err := n.Scan(s); err != nil {
		t.Fatalf("numeric(%q): %v", s, err)
	}
	return n
}
