package httpx

import (
	"context"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// The 21 methods below satisfy gen.StrictServerInterface's catalogue and
// product-image operations by forwarding to server.catalog
// (*catalog.Handler) — named, not embedded, for the same reason
// server.shop is (shop.go's doc comment): *auth.Handler, *shop.Handler and
// *catalog.Handler are all named "Handler", and only one anonymous field
// per name is possible. Replaces what unimplemented.go stubbed out before
// the catalog module existed (docs/06-ROADMAP.md Phase 2 T4).

// ListUnits lists the shop's units of measure.
func (s server) ListUnits(ctx context.Context, req gen.ListUnitsRequestObject) (gen.ListUnitsResponseObject, error) {
	return s.catalog.ListUnits(ctx, req)
}

// ListAttributeDefinitions lists the shop's attribute definitions.
func (s server) ListAttributeDefinitions(ctx context.Context, req gen.ListAttributeDefinitionsRequestObject) (gen.ListAttributeDefinitionsResponseObject, error) {
	return s.catalog.ListAttributeDefinitions(ctx, req)
}

// CreateAttributeDefinition creates an attribute definition.
func (s server) CreateAttributeDefinition(ctx context.Context, req gen.CreateAttributeDefinitionRequestObject) (gen.CreateAttributeDefinitionResponseObject, error) {
	return s.catalog.CreateAttributeDefinition(ctx, req)
}

// UpdateAttributeDefinition updates an attribute definition.
func (s server) UpdateAttributeDefinition(ctx context.Context, req gen.UpdateAttributeDefinitionRequestObject) (gen.UpdateAttributeDefinitionResponseObject, error) {
	return s.catalog.UpdateAttributeDefinition(ctx, req)
}

// ListCategories lists the shop's categories.
func (s server) ListCategories(ctx context.Context, req gen.ListCategoriesRequestObject) (gen.ListCategoriesResponseObject, error) {
	return s.catalog.ListCategories(ctx, req)
}

// CreateCategory creates a category.
func (s server) CreateCategory(ctx context.Context, req gen.CreateCategoryRequestObject) (gen.CreateCategoryResponseObject, error) {
	return s.catalog.CreateCategory(ctx, req)
}

// GetCategory gets a category.
func (s server) GetCategory(ctx context.Context, req gen.GetCategoryRequestObject) (gen.GetCategoryResponseObject, error) {
	return s.catalog.GetCategory(ctx, req)
}

// UpdateCategory updates a category.
func (s server) UpdateCategory(ctx context.Context, req gen.UpdateCategoryRequestObject) (gen.UpdateCategoryResponseObject, error) {
	return s.catalog.UpdateCategory(ctx, req)
}

// DeleteCategory soft-deletes a category.
func (s server) DeleteCategory(ctx context.Context, req gen.DeleteCategoryRequestObject) (gen.DeleteCategoryResponseObject, error) {
	return s.catalog.DeleteCategory(ctx, req)
}

// ListProducts lists products.
func (s server) ListProducts(ctx context.Context, req gen.ListProductsRequestObject) (gen.ListProductsResponseObject, error) {
	return s.catalog.ListProducts(ctx, req)
}

// CreateProduct creates a product.
func (s server) CreateProduct(ctx context.Context, req gen.CreateProductRequestObject) (gen.CreateProductResponseObject, error) {
	return s.catalog.CreateProduct(ctx, req)
}

// GetProduct gets a product, including its variants and images.
func (s server) GetProduct(ctx context.Context, req gen.GetProductRequestObject) (gen.GetProductResponseObject, error) {
	return s.catalog.GetProduct(ctx, req)
}

// UpdateProduct updates a product.
func (s server) UpdateProduct(ctx context.Context, req gen.UpdateProductRequestObject) (gen.UpdateProductResponseObject, error) {
	return s.catalog.UpdateProduct(ctx, req)
}

// DeleteProduct soft-deletes a product.
func (s server) DeleteProduct(ctx context.Context, req gen.DeleteProductRequestObject) (gen.DeleteProductResponseObject, error) {
	return s.catalog.DeleteProduct(ctx, req)
}

// ListVariants lists a product's variants.
func (s server) ListVariants(ctx context.Context, req gen.ListVariantsRequestObject) (gen.ListVariantsResponseObject, error) {
	return s.catalog.ListVariants(ctx, req)
}

// CreateVariant adds a variant to a product.
func (s server) CreateVariant(ctx context.Context, req gen.CreateVariantRequestObject) (gen.CreateVariantResponseObject, error) {
	return s.catalog.CreateVariant(ctx, req)
}

// UpdateVariant updates a variant.
func (s server) UpdateVariant(ctx context.Context, req gen.UpdateVariantRequestObject) (gen.UpdateVariantResponseObject, error) {
	return s.catalog.UpdateVariant(ctx, req)
}

// DeleteVariant soft-deletes a variant.
func (s server) DeleteVariant(ctx context.Context, req gen.DeleteVariantRequestObject) (gen.DeleteVariantResponseObject, error) {
	return s.catalog.DeleteVariant(ctx, req)
}

// AddProductImage attaches an uploaded image to a product.
func (s server) AddProductImage(ctx context.Context, req gen.AddProductImageRequestObject) (gen.AddProductImageResponseObject, error) {
	return s.catalog.AddProductImage(ctx, req)
}

// RemoveProductImage removes an image from a product.
func (s server) RemoveProductImage(ctx context.Context, req gen.RemoveProductImageRequestObject) (gen.RemoveProductImageResponseObject, error) {
	return s.catalog.RemoveProductImage(ctx, req)
}

// ReorderProductImages reorders a product's images and/or changes its cover image.
func (s server) ReorderProductImages(ctx context.Context, req gen.ReorderProductImagesRequestObject) (gen.ReorderProductImagesResponseObject, error) {
	return s.catalog.ReorderProductImages(ctx, req)
}
