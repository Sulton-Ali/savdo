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
 * the tree from `parentId`. Always fetched with `includeInactive: true` here
 * since the categories page is only reachable with `catalog.write`, which
 * must be able to see and manage inactive categories too. */
export async function fetchCategories(): Promise<Category[]> {
  const { data, error } = await api.GET("/categories", {
    params: { query: { includeInactive: true } },
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
