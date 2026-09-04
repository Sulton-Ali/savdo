import type { components } from "@savdo/api-client";

import { api } from "../lib/api";
import { ApiError } from "../lib/errors";
import type { CursorPage } from "../lib/useCursorList";

export type Unit = components["schemas"]["Unit"];
export type AttributeDefinition = components["schemas"]["AttributeDefinition"];
export type AttributeDefinitionCreate = components["schemas"]["AttributeDefinitionCreate"];
export type AttributeDefinitionPatch = components["schemas"]["AttributeDefinitionPatch"];
export type Category = components["schemas"]["Category"];
export type CategoryCreate = components["schemas"]["CategoryCreate"];
export type CategoryPatch = components["schemas"]["CategoryPatch"];
export type Product = components["schemas"]["Product"];
export type ProductCreate = components["schemas"]["ProductCreate"];
export type ProductPatch = components["schemas"]["ProductPatch"];
export type Translations = components["schemas"]["Translations"];
export type AttributeValues = components["schemas"]["AttributeValues"];
export type Variant = components["schemas"]["Variant"];
export type VariantCreate = components["schemas"]["VariantCreate"];
export type VariantPatch = components["schemas"]["VariantPatch"];
export type MediaFile = components["schemas"]["MediaFile"];
export type ProductImage = components["schemas"]["ProductImage"];
export type ProductImageCreate = components["schemas"]["ProductImageCreate"];
export type ProductImageOrder = components["schemas"]["ProductImageOrder"];

/** Matches every other collection endpoint's default (`docs/05-API.md` §
 * Conventions). */
const PAGE_LIMIT = 50;

/** `GET /units` — the shop's units of measure. Not cursor-paginated, seeded
 * and read-only in Phase 2. */
export async function fetchUnits(): Promise<Unit[]> {
  const { data, error } = await api.GET("/units");
  if (error) {
    throw new ApiError(error);
  }
  return data.items;
}

/** `GET /attribute-definitions` — not cursor-paginated (small, seeded set). */
export async function fetchAttributeDefinitions(): Promise<AttributeDefinition[]> {
  const { data, error } = await api.GET("/attribute-definitions");
  if (error) {
    throw new ApiError(error);
  }
  return data.items;
}

/** `POST /attribute-definitions` — requires `catalog.write` (manager+). */
export async function createAttributeDefinition(
  body: AttributeDefinitionCreate,
): Promise<AttributeDefinition> {
  const { data, error } = await api.POST("/attribute-definitions", { body });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `PATCH /attribute-definitions/{id}` — requires `catalog.write`. `code` is
 * not patchable. */
export async function updateAttributeDefinition(
  id: string,
  body: AttributeDefinitionPatch,
): Promise<AttributeDefinition> {
  const { data, error } = await api.PATCH("/attribute-definitions/{id}", {
    params: { path: { id } },
    body,
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `GET /categories` — flat list, not cursor-paginated; the client builds
 * the tree from `parentId`. `includeInactive` defaults to `false` (a
 * cashier browsing the product list's category filter has no `catalog.write`
 * and should never see inactive categories); callers with `catalog.write`
 * (the categories page, the product form's category field) pass `true`. */
export async function fetchCategories(includeInactive = false): Promise<Category[]> {
  const { data, error } = await api.GET("/categories", {
    params: { query: { includeInactive: includeInactive || undefined } },
  });
  if (error) {
    throw new ApiError(error);
  }
  return data.items;
}

/** `POST /categories` — requires `catalog.write`. */
export async function createCategory(body: CategoryCreate): Promise<Category> {
  const { data, error } = await api.POST("/categories", { body });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `PATCH /categories/{id}` — requires `catalog.write`. */
export async function updateCategory(id: string, body: CategoryPatch): Promise<Category> {
  const { data, error } = await api.PATCH("/categories/{id}", {
    params: { path: { id } },
    body,
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `DELETE /categories/{id}` — requires `catalog.write`. `409 CONFLICT
 * details.field: "products"` when products still reference this category. */
export async function deleteCategory(id: string): Promise<void> {
  const { error } = await api.DELETE("/categories/{id}", {
    params: { path: { id } },
  });
  if (error) {
    throw new ApiError(error);
  }
}

export interface ProductListFilters {
  q?: string;
  categoryId?: string;
  includeInactive?: boolean;
}

/** `GET /products` — cursor-paginated. `costPrice`/`translations` are
 * present only for owner/manager (§ Conventions, ADR-010) — never assume
 * their presence from the caller's role, check the response instead. */
export async function fetchProductsPage(
  filters: ProductListFilters,
  cursor: string | null,
): Promise<CursorPage<Product>> {
  const { data, error } = await api.GET("/products", {
    params: {
      query: {
        limit: PAGE_LIMIT,
        cursor: cursor ?? undefined,
        q: filters.q || undefined,
        categoryId: filters.categoryId,
        includeInactive: filters.includeInactive,
      },
    },
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `GET /products/{id}` — a single product, including its variants and
 * images. */
export async function fetchProduct(id: string): Promise<Product> {
  const { data, error } = await api.GET("/products/{id}", {
    params: { path: { id } },
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `POST /products` — requires `catalog.write`. No `variants` field means an
 * implicit single variant is created server-side. */
export async function createProduct(body: ProductCreate): Promise<Product> {
  const { data, error } = await api.POST("/products", { body });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `PATCH /products/{id}` — requires `catalog.write`. Partial update; `null`
 * clears a nullable field (D-35). */
export async function updateProduct(id: string, body: ProductPatch): Promise<Product> {
  const { data, error } = await api.PATCH("/products/{id}", {
    params: { path: { id } },
    body,
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `GET /products/{id}/variants` — not cursor-paginated (a product's
 * variants are always few). */
export async function fetchVariants(productId: string): Promise<Variant[]> {
  const { data, error } = await api.GET("/products/{id}/variants", {
    params: { path: { id: productId } },
  });
  if (error) {
    throw new ApiError(error);
  }
  return data.items;
}

/** `POST /products/{id}/variants` — requires `catalog.write`. */
export async function createVariant(productId: string, body: VariantCreate): Promise<Variant> {
  const { data, error } = await api.POST("/products/{id}/variants", {
    params: { path: { id: productId } },
    body,
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `PATCH /variants/{id}` — requires `catalog.write`. Partial update; `null`
 * clears `priceOverride`/`costOverride` (D-35). */
export async function updateVariant(id: string, body: VariantPatch): Promise<Variant> {
  const { data, error } = await api.PATCH("/variants/{id}", {
    params: { path: { id } },
    body,
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `DELETE /variants/{id}` — requires `catalog.write`. `400
 * fields.variantId: invalid` when this is the product's only variant. */
export async function deleteVariant(id: string): Promise<void> {
  const { error } = await api.DELETE("/variants/{id}", {
    params: { path: { id } },
  });
  if (error) {
    throw new ApiError(error);
  }
}

/** `POST /media` — requires `catalog.write` (multipart). The generated
 * `MediaUpload` schema types `file` as `string` (openapi-typescript's
 * rendering of `format: binary`), but the wire body is real
 * `multipart/form-data` — `bodySerializer` below builds actual `FormData`
 * from the `File`/`Blob`, and openapi-fetch's `defaultBodySerializer` passes
 * a `FormData` body through untouched so the browser sets the
 * `Content-Type` boundary itself (never set it by hand). */
export async function uploadMedia(file: File | Blob): Promise<MediaFile> {
  const { data, error } = await api.POST("/media", {
    body: { file } as unknown as { file: string },
    bodySerializer(body) {
      const formData = new FormData();
      formData.append("file", (body as unknown as { file: File | Blob }).file);
      return formData;
    },
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `POST /products/{id}/images` — requires `catalog.write`. `400
 * fields.mediaId: invalid` (or `fields.images: too_long`) once the product
 * already has 8 images (D-34); `409 CONFLICT details.field: mediaId` when
 * this media file is already attached to the product. */
export async function addProductImage(
  productId: string,
  body: ProductImageCreate,
): Promise<ProductImage> {
  const { data, error } = await api.POST("/products/{id}/images", {
    params: { path: { id: productId } },
    body,
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `DELETE /products/{id}/images/{imageId}` — requires `catalog.write`. */
export async function removeProductImage(productId: string, imageId: string): Promise<void> {
  const { error } = await api.DELETE("/products/{id}/images/{imageId}", {
    params: { path: { id: productId, imageId } },
  });
  if (error) {
    throw new ApiError(error);
  }
}

/** `PATCH /products/{id}/images/order` — requires `catalog.write`. Always
 * send every image id belonging to the product, in the new display order;
 * `coverImageId` optionally changes the cover in the same call. */
export async function reorderProductImages(
  productId: string,
  body: ProductImageOrder,
): Promise<ProductImage[]> {
  const { data, error } = await api.PATCH("/products/{id}/images/order", {
    params: { path: { id: productId } },
    body,
  });
  if (error) {
    throw new ApiError(error);
  }
  return data.items;
}
