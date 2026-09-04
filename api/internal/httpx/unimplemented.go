package httpx

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// notImplementedResponse satisfies every Phase 2 T1 *ResponseObject
// interface the strict server needs so `server` compiles against
// `gen.StrictServerInterface` before each operation's owning module lands
// (docs/06-ROADMAP.md Phase 2). It always writes the shared `Error`
// envelope (ADR-013) with code INTERNAL and `details.reason:
// "not_implemented"` — same shape and same rationale as Phase 1's
// unimplemented.go (git history), which this file re-creates for the 22
// catalogue/media operations T1 adds to the contract.
//
// Each method here corresponds to one contracts/openapi.yaml operationId.
// Replaced as the catalog/media handlers land in their own module; delete
// the method here when its real handler lands.
type notImplementedResponse struct{}

func (notImplementedResponse) write(w http.ResponseWriter) error {
	body := gen.Error{}
	body.Error.Code = gen.INTERNAL
	details := map[string]interface{}{"reason": "not_implemented"}
	body.Error.Details = &details

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	return json.NewEncoder(w).Encode(body)
}

func (r notImplementedResponse) VisitListUnitsResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitListAttributeDefinitionsResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitCreateAttributeDefinitionResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitUpdateAttributeDefinitionResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitListCategoriesResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitCreateCategoryResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitGetCategoryResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitUpdateCategoryResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitDeleteCategoryResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitListProductsResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitCreateProductResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitGetProductResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitUpdateProductResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitDeleteProductResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitListVariantsResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitCreateVariantResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitUpdateVariantResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitDeleteVariantResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitUploadMediaResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitAddProductImageResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitRemoveProductImageResponse(w http.ResponseWriter) error {
	return r.write(w)
}
func (r notImplementedResponse) VisitReorderProductImagesResponse(w http.ResponseWriter) error {
	return r.write(w)
}

// ListUnits lists the shop's units of measure.
// Phase 2 T1: replaced once the catalog module lands.
func (server) ListUnits(_ context.Context, _ gen.ListUnitsRequestObject) (gen.ListUnitsResponseObject, error) {
	return notImplementedResponse{}, nil
}

// ListAttributeDefinitions lists the shop's attribute definitions.
// Phase 2 T1: replaced once the catalog module lands.
func (server) ListAttributeDefinitions(_ context.Context, _ gen.ListAttributeDefinitionsRequestObject) (gen.ListAttributeDefinitionsResponseObject, error) {
	return notImplementedResponse{}, nil
}

// CreateAttributeDefinition creates an attribute definition.
// Phase 2 T1: replaced once the catalog module lands.
func (server) CreateAttributeDefinition(_ context.Context, _ gen.CreateAttributeDefinitionRequestObject) (gen.CreateAttributeDefinitionResponseObject, error) {
	return notImplementedResponse{}, nil
}

// UpdateAttributeDefinition updates an attribute definition.
// Phase 2 T1: replaced once the catalog module lands.
func (server) UpdateAttributeDefinition(_ context.Context, _ gen.UpdateAttributeDefinitionRequestObject) (gen.UpdateAttributeDefinitionResponseObject, error) {
	return notImplementedResponse{}, nil
}

// ListCategories lists the shop's categories.
// Phase 2 T1: replaced once the catalog module lands.
func (server) ListCategories(_ context.Context, _ gen.ListCategoriesRequestObject) (gen.ListCategoriesResponseObject, error) {
	return notImplementedResponse{}, nil
}

// CreateCategory creates a category.
// Phase 2 T1: replaced once the catalog module lands.
func (server) CreateCategory(_ context.Context, _ gen.CreateCategoryRequestObject) (gen.CreateCategoryResponseObject, error) {
	return notImplementedResponse{}, nil
}

// GetCategory gets a category.
// Phase 2 T1: replaced once the catalog module lands.
func (server) GetCategory(_ context.Context, _ gen.GetCategoryRequestObject) (gen.GetCategoryResponseObject, error) {
	return notImplementedResponse{}, nil
}

// UpdateCategory updates a category.
// Phase 2 T1: replaced once the catalog module lands.
func (server) UpdateCategory(_ context.Context, _ gen.UpdateCategoryRequestObject) (gen.UpdateCategoryResponseObject, error) {
	return notImplementedResponse{}, nil
}

// DeleteCategory soft-deletes a category.
// Phase 2 T1: replaced once the catalog module lands.
func (server) DeleteCategory(_ context.Context, _ gen.DeleteCategoryRequestObject) (gen.DeleteCategoryResponseObject, error) {
	return notImplementedResponse{}, nil
}

// ListProducts lists products.
// Phase 2 T1: replaced once the catalog module lands.
func (server) ListProducts(_ context.Context, _ gen.ListProductsRequestObject) (gen.ListProductsResponseObject, error) {
	return notImplementedResponse{}, nil
}

// CreateProduct creates a product.
// Phase 2 T1: replaced once the catalog module lands.
func (server) CreateProduct(_ context.Context, _ gen.CreateProductRequestObject) (gen.CreateProductResponseObject, error) {
	return notImplementedResponse{}, nil
}

// GetProduct gets a product, including its variants and images.
// Phase 2 T1: replaced once the catalog module lands.
func (server) GetProduct(_ context.Context, _ gen.GetProductRequestObject) (gen.GetProductResponseObject, error) {
	return notImplementedResponse{}, nil
}

// UpdateProduct updates a product.
// Phase 2 T1: replaced once the catalog module lands.
func (server) UpdateProduct(_ context.Context, _ gen.UpdateProductRequestObject) (gen.UpdateProductResponseObject, error) {
	return notImplementedResponse{}, nil
}

// DeleteProduct soft-deletes a product.
// Phase 2 T1: replaced once the catalog module lands.
func (server) DeleteProduct(_ context.Context, _ gen.DeleteProductRequestObject) (gen.DeleteProductResponseObject, error) {
	return notImplementedResponse{}, nil
}

// ListVariants lists a product's variants.
// Phase 2 T1: replaced once the catalog module lands.
func (server) ListVariants(_ context.Context, _ gen.ListVariantsRequestObject) (gen.ListVariantsResponseObject, error) {
	return notImplementedResponse{}, nil
}

// CreateVariant adds a variant to a product.
// Phase 2 T1: replaced once the catalog module lands.
func (server) CreateVariant(_ context.Context, _ gen.CreateVariantRequestObject) (gen.CreateVariantResponseObject, error) {
	return notImplementedResponse{}, nil
}

// UpdateVariant updates a variant.
// Phase 2 T1: replaced once the catalog module lands.
func (server) UpdateVariant(_ context.Context, _ gen.UpdateVariantRequestObject) (gen.UpdateVariantResponseObject, error) {
	return notImplementedResponse{}, nil
}

// DeleteVariant soft-deletes a variant.
// Phase 2 T1: replaced once the catalog module lands.
func (server) DeleteVariant(_ context.Context, _ gen.DeleteVariantRequestObject) (gen.DeleteVariantResponseObject, error) {
	return notImplementedResponse{}, nil
}

// UploadMedia uploads an image.
// Phase 2 T1: replaced once the media module lands.
func (server) UploadMedia(_ context.Context, _ gen.UploadMediaRequestObject) (gen.UploadMediaResponseObject, error) {
	return notImplementedResponse{}, nil
}

// AddProductImage attaches an uploaded image to a product.
// Phase 2 T1: replaced once the media module lands.
func (server) AddProductImage(_ context.Context, _ gen.AddProductImageRequestObject) (gen.AddProductImageResponseObject, error) {
	return notImplementedResponse{}, nil
}

// RemoveProductImage removes an image from a product.
// Phase 2 T1: replaced once the media module lands.
func (server) RemoveProductImage(_ context.Context, _ gen.RemoveProductImageRequestObject) (gen.RemoveProductImageResponseObject, error) {
	return notImplementedResponse{}, nil
}

// ReorderProductImages reorders a product's images and/or changes its cover image.
// Phase 2 T1: replaced once the media module lands.
func (server) ReorderProductImages(_ context.Context, _ gen.ReorderProductImagesRequestObject) (gen.ReorderProductImagesResponseObject, error) {
	return notImplementedResponse{}, nil
}
