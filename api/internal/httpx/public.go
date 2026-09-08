package httpx

import (
	"context"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// The four methods below satisfy gen.StrictServerInterface's `/public/*`
// operations by forwarding to server.public (*public.Handler) — named,
// not embedded, for the same reason server.shop/server.content are
// (healthz.go's doc comment on the server struct). auth.Middleware's
// allowlistedOperations (api/internal/auth/middleware.go) lets all four
// through without a session, and public.Service.CacheMiddleware
// (NewRouter's own doc comment) caches their 200 responses — no auth
// context is ever read inside server.public itself.

// GetPublicShop returns the shop's identity and landing content blocks.
func (s server) GetPublicShop(ctx context.Context, req gen.GetPublicShopRequestObject) (gen.GetPublicShopResponseObject, error) {
	return s.public.GetPublicShop(ctx, req)
}

// ListPublicCategories returns the shop's active categories.
func (s server) ListPublicCategories(ctx context.Context, req gen.ListPublicCategoriesRequestObject) (gen.ListPublicCategoriesResponseObject, error) {
	return s.public.ListPublicCategories(ctx, req)
}

// ListPublicProducts browses the public catalogue.
func (s server) ListPublicProducts(ctx context.Context, req gen.ListPublicProductsRequestObject) (gen.ListPublicProductsResponseObject, error) {
	return s.public.ListPublicProducts(ctx, req)
}

// GetPublicProductBySlug returns one public product by slug.
func (s server) GetPublicProductBySlug(ctx context.Context, req gen.GetPublicProductBySlugRequestObject) (gen.GetPublicProductBySlugResponseObject, error) {
	return s.public.GetPublicProductBySlug(ctx, req)
}
