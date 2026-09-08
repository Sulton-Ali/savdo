package public_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/catalog"
	"github.com/Sulton-Ali/savdo/api/internal/content"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
	"github.com/Sulton-Ali/savdo/api/internal/public"
)

// TestPublicCache_catalogProductDelete_invalidatesSoNextListReflectsChange
// pins T3 review round 2, MINOR 9's "a catalog write (product update)
// invalidates via catalog.Service.SetInvalidator" case — the cmd/api/
// main.go wiring (catalogSvc.SetInvalidator(publicSvc)) actually reaches
// the public cache, mirroring TestPublicCache_contentPUT_invalidates...
// (router_test.go) for content.Service. Driven through the real router
// (T3 review round 3, MINOR 2), not by calling h.ListPublicProducts
// directly, because CacheMiddleware — the thing being tested — wraps the
// router's StrictServerInterface, not the bare *Handler: calling the
// handler directly would bypass caching entirely and the test could
// never fail. DeleteProduct is the cheapest catalog write to drive here:
// no translations/units/attributes payload to build, unlike Create/
// Update.
func TestPublicCache_catalogProductDelete_invalidatesSoNextListReflectsChange(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	counter := &queryCounter{DBTX: pool}
	q := db.New(counter)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "shop-a")
	unit := seedUnit(ctx, t, q, shopRow.ID)

	contentSvc := content.NewService(q)
	publicSvc := public.NewService(q, contentSvc, "shop-a", "/media")
	contentSvc.SetInvalidator(publicSvc)
	catalogSvc := catalog.NewService(pool, q, "uz", "/media")
	catalogSvc.SetInvalidator(publicSvc)
	catalogHandler := catalog.NewHandler(catalogSvc)
	router := newTestRouterWithServices(pool, publicSvc, contentSvc)

	product := seedProduct(ctx, t, q, shopRow.ID, unit.ID, productSpec{
		Slug: "goner", Name: "Goner", BasePrice: "1.00", IsActive: true,
	})

	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/v1/public/products", nil))
	if rec1.Code != http.StatusOK || !strings.Contains(rec1.Body.String(), `"slug":"goner"`) {
		t.Fatalf("first GET = %d %s, want 200 with goner present — test fixture is broken", rec1.Code, rec1.Body.String())
	}
	n1 := counter.n.Load()
	if n1 == 0 {
		t.Fatal("the first GET made zero database queries — the test fixture is broken, not the cache")
	}

	// Second GET, before the delete: must be a cache hit — same ETag, no
	// additional database query — so the invalidation this test actually
	// checks for is meaningful (there is something live in the cache to
	// invalidate, not just an always-fresh handler that happens to look
	// invalidated).
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/v1/public/products", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("second GET status = %d", rec2.Code)
	}
	if rec2.Header().Get("ETag") != rec1.Header().Get("ETag") {
		t.Errorf("second GET ETag = %q, want the same %q (pre-delete cache hit)", rec2.Header().Get("ETag"), rec1.Header().Get("ETag"))
	}
	if got := counter.n.Load(); got != n1 {
		t.Fatalf("queries after the second (pre-delete) GET = %d, want unchanged from %d — it must be a cache hit", got, n1)
	}

	ownerCtx := auth.WithContext(context.Background(), auth.Context{
		ShopID: shopRow.ID, UserID: uuid.New(), Role: db.UserRoleOwner, SessionID: uuid.New(), Client: db.SessionClientWeb,
	})
	if _, err := catalogHandler.DeleteProduct(ownerCtx, gen.DeleteProductRequestObject{Id: product.ID}); err != nil {
		t.Fatalf("DeleteProduct: %v", err)
	}

	// Third GET, after the delete: must reflect the change immediately —
	// not still serving the pre-delete cache entry for the rest of the
	// 60 s window.
	rec3 := httptest.NewRecorder()
	router.ServeHTTP(rec3, httptest.NewRequest(http.MethodGet, "/v1/public/products", nil))
	if rec3.Code != http.StatusOK {
		t.Fatalf("third GET status = %d", rec3.Code)
	}
	if strings.Contains(rec3.Body.String(), `"slug":"goner"`) {
		t.Fatalf("third GET (after delete) = %s, still shows the deleted product — public cache was not invalidated", rec3.Body.String())
	}
}
