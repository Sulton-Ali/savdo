package public_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/catalog"
	"github.com/Sulton-Ali/savdo/api/internal/content"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
	"github.com/Sulton-Ali/savdo/api/internal/public"
	"github.com/Sulton-Ali/savdo/api/internal/stock"
)

// newTestHandler builds a public.Handler backed by a real (testcontainers)
// Postgres, truncated for isolation, wired to a real content.Service the
// same way cmd/api/main.go wires the two (contentSvc.SetInvalidator(svc)),
// so a content PUT through contentSvc is visible to cache-invalidation
// tests.
func newTestHandler(t *testing.T, publicShopSlug string) (*public.Handler, *public.Service, *content.Service, *db.Queries, *pgxpool.Pool) {
	t.Helper()
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	q := db.New(pool)
	contentSvc := content.NewService(q)
	svc := public.NewService(q, contentSvc, publicShopSlug, "/media")
	contentSvc.SetInvalidator(svc)
	return public.NewHandler(svc), svc, contentSvc, q, pool
}

func seedShop(ctx context.Context, t *testing.T, q *db.Queries, slug string) db.Shop {
	t.Helper()
	shopRow, err := q.CreateShop(ctx, db.CreateShopParams{ID: uuid.New(), Slug: slug, Name: "Test Shop " + slug})
	if err != nil {
		t.Fatalf("seedShop(%q): %v", slug, err)
	}
	return shopRow
}

func seedUnit(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID) db.Unit {
	t.Helper()
	u, err := q.UpsertUnit(ctx, db.UpsertUnitParams{ID: uuid.New(), ShopID: shopID, Code: "pcs", Precision: 0})
	if err != nil {
		t.Fatalf("seedUnit: %v", err)
	}
	return u
}

func seedLocation(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, name string, isActive bool) db.Location {
	t.Helper()
	l, err := q.CreateLocation(ctx, db.CreateLocationParams{
		ID: uuid.New(), ShopID: shopID, Name: name, Kind: db.LocationKindStore, IsDefault: false, IsActive: isActive,
	})
	if err != nil {
		t.Fatalf("seedLocation(%q): %v", name, err)
	}
	return l
}

// seedCategory creates an active/inactive category with a uz translation.
func seedCategory(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, slug, name string, isActive bool) db.Category {
	t.Helper()
	c, err := q.CreateCategory(ctx, db.CreateCategoryParams{ID: uuid.New(), ShopID: shopID, Slug: slug, IsActive: isActive})
	if err != nil {
		t.Fatalf("seedCategory(%q): %v", slug, err)
	}
	if err := q.UpsertCategoryTranslation(ctx, db.UpsertCategoryTranslationParams{CategoryID: c.ID, Locale: "uz", Name: name}); err != nil {
		t.Fatalf("seedCategory(%q) translation: %v", slug, err)
	}
	return c
}

// seedSubcategory creates an active/inactive category under parentID, with
// a uz translation — T7's parent/child hierarchy tests.
func seedSubcategory(ctx context.Context, t *testing.T, q *db.Queries, shopID, parentID uuid.UUID, slug, name string, isActive bool) db.Category {
	t.Helper()
	c, err := q.CreateCategory(ctx, db.CreateCategoryParams{ID: uuid.New(), ShopID: shopID, ParentID: &parentID, Slug: slug, IsActive: isActive})
	if err != nil {
		t.Fatalf("seedSubcategory(%q): %v", slug, err)
	}
	if err := q.UpsertCategoryTranslation(ctx, db.UpsertCategoryTranslationParams{CategoryID: c.ID, Locale: "uz", Name: name}); err != nil {
		t.Fatalf("seedSubcategory(%q) translation: %v", slug, err)
	}
	return c
}

// productSpec is seedProduct's input — every field has a small, obvious
// default via seedProduct's own zero-value handling, so a test only sets
// the fields it cares about.
type productSpec struct {
	CategoryID           *uuid.UUID
	Slug, Name           string
	BasePrice            string
	PromoPrice           string // "" = no promo
	PromoFrom, PromoTo   *time.Time
	IsActive, IsFeatured bool
	LowStockThreshold    *int32 // nil = use the shop default (D-44)
}

func seedProduct(ctx context.Context, t *testing.T, q *db.Queries, shopID, unitID uuid.UUID, spec productSpec) db.Product {
	t.Helper()
	params := db.CreateProductParams{
		ID: uuid.New(), ShopID: shopID, CategoryID: spec.CategoryID, UnitID: unitID, Slug: spec.Slug,
		BasePrice: numeric(t, spec.BasePrice), IsActive: spec.IsActive, IsFeatured: spec.IsFeatured,
		PromoFrom: spec.PromoFrom, PromoTo: spec.PromoTo, LowStockThreshold: spec.LowStockThreshold,
	}
	if spec.PromoPrice != "" {
		params.PromoPrice = numeric(t, spec.PromoPrice)
	}
	p, err := q.CreateProduct(ctx, params)
	if err != nil {
		t.Fatalf("seedProduct(%q): %v", spec.Slug, err)
	}
	if err := q.UpsertProductTranslation(ctx, db.UpsertProductTranslationParams{ProductID: p.ID, Locale: "uz", Name: spec.Name}); err != nil {
		t.Fatalf("seedProduct(%q) translation: %v", spec.Slug, err)
	}
	return p
}

// variantSpec is seedVariant's input, same zero-value-default idea as
// productSpec.
type variantSpec struct {
	Attributes    string // raw JSON, e.g. `{"size":"M"}`; "" -> "{}"
	PriceOverride string // "" = no override
	IsActive      bool
}

func seedVariant(ctx context.Context, t *testing.T, q *db.Queries, shopID, productID uuid.UUID, spec variantSpec) db.ProductVariant {
	t.Helper()
	attrs := spec.Attributes
	if attrs == "" {
		attrs = "{}"
	}
	params := db.CreateVariantParams{
		ID: uuid.New(), ShopID: shopID, ProductID: productID, Attributes: json.RawMessage(attrs), IsActive: spec.IsActive,
	}
	if spec.PriceOverride != "" {
		params.PriceOverride = numeric(t, spec.PriceOverride)
	}
	v, err := q.CreateVariant(ctx, params)
	if err != nil {
		t.Fatalf("seedVariant: %v", err)
	}
	return v
}

// stockIn writes qty into (variantID, locationID) via stock.Move, the
// only writer of stock_levels (hard rule 2, ADR-006) — never a direct
// stock_levels insert, even in a seed helper.
func stockIn(ctx context.Context, t *testing.T, pool *pgxpool.Pool, q *db.Queries, shopID, variantID, locationID uuid.UUID, qty string) {
	t.Helper()
	svc := stock.NewService(pool, q)
	reason := db.AdjustmentReasonCountCorrection
	if _, err := svc.MoveInTx(ctx, stock.MoveParams{
		ShopID: shopID, VariantID: variantID, LocationID: locationID, Kind: db.StockMovementKindAdjustment,
		Qty: decimalOf(t, qty), AdjustmentReason: &reason,
	}); err != nil {
		t.Fatalf("stockIn(%s, %s): %v", variantID, qty, err)
	}
}

func numeric(t *testing.T, s string) pgtype.Numeric {
	t.Helper()
	var n pgtype.Numeric
	if err := n.Scan(s); err != nil {
		t.Fatalf("numeric(%q): %v", s, err)
	}
	return n
}

// ctxWithAcceptLanguage builds a context carrying header the way a real
// request's Accept-Language would after catalog.AcceptLanguageMiddleware
// ran (internal/httpx.NewRouter wires it ahead of every operation,
// including public's) — the only exported way to reach
// catalog.ResolveLocale's stashed value without standing up the whole
// httpx router for every test (router_test.go in this package does that
// instead, for the handful of tests that need real HTTP: no-session,
// caching headers, invalidation through the wire).
func ctxWithAcceptLanguage(header string) context.Context {
	var out context.Context
	wrapped := catalog.AcceptLanguageMiddleware(func(ctx context.Context, _ http.ResponseWriter, _ *http.Request, _ any) (any, error) {
		out = ctx
		return nil, nil
	}, "Test")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if header != "" {
		req.Header.Set("Accept-Language", header)
	}
	_, _ = wrapped(context.Background(), nil, req, nil)
	return out
}

// assertNotFound fails t unless err is a 404 NOT_FOUND apierr.Error.
func assertNotFound(t *testing.T, label string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: want 404 NOT_FOUND, got no error", label)
	}
	code, status := errCodeStatus(err)
	if status != http.StatusNotFound || code != string(gen.NOTFOUND) {
		t.Fatalf("%s: error = %v, want 404 NOT_FOUND", label, err)
	}
}

// jsonBytes marshals v the same way a gen.Visit*Response method does
// (json.Marshal, not the Encoder's trailing-newline variant — tests here
// only need the bytes to scan, not byte-for-byte parity with the wire
// format cache.go's renderResponse cares about).
func jsonBytes(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}

// errCodeStatus extracts an apierr.Error's code and HTTP status from err,
// or ("", 0) when err is not one.
func errCodeStatus(err error) (string, int) {
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) {
		return "", 0
	}
	return string(apiErr.Code), apiErr.Status
}

// decimalOf parses s (a plain decimal literal test data uses, e.g.
// "5.000") into a decimal.Decimal, failing t on a malformed literal —
// stock.MoveParams.Qty's own type.
func decimalOf(t *testing.T, s string) decimal.Decimal {
	t.Helper()
	d, err := decimal.NewFromString(s)
	if err != nil {
		t.Fatalf("decimalOf(%q): %v", s, err)
	}
	return d
}
