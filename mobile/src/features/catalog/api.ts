import type { components } from "@savdo/api-client";

import { api } from "@/lib/api";

export type Product = components["schemas"]["Product"];
export type Variant = components["schemas"]["Variant"];
export type StockLevel = components["schemas"]["StockLevel"];
export type Location = components["schemas"]["Location"];
export type ErrorCode = components["schemas"]["ErrorCode"];

/** Matches every other collection endpoint's default (`docs/05-API.md` §
 * Conventions). */
const PAGE_LIMIT = 50;

export interface CursorPage<T> {
  items: T[];
  nextCursor: string | null;
}

/**
 * Thrown by every function in this module on a non-2xx response. Carries
 * only the machine-readable `code` (ADR-013) — callers translate it to a
 * display sentence via `@savdo/i18n`, never show a raw API message.
 * Mirrors `lib/authApi.ts`'s `ApiAuthError` and `admin/src/lib/errors.ts`'s
 * `ApiError`.
 */
export class CatalogApiError extends Error {
  readonly code: ErrorCode;

  constructor(code: ErrorCode) {
    super(`catalog request failed: ${code}`);
    this.name = "CatalogApiError";
    this.code = code;
  }
}

export interface ListProductsParams {
  q?: string;
  cursor?: string | null;
}

/** `GET /products` — cursor-paginated. `costPrice`/`translations` are
 * present only for owner/manager (§ Conventions, ADR-010) — this module
 * never reads either field. */
export async function listProducts({
  q,
  cursor,
}: ListProductsParams): Promise<CursorPage<Product>> {
  const { data, error } = await api.GET("/products", {
    params: {
      query: {
        limit: PAGE_LIMIT,
        cursor: cursor ?? undefined,
        q: q || undefined,
      },
    },
  });
  if (error) {
    throw new CatalogApiError(error.error.code);
  }
  return data;
}

/** `GET /products/{id}` — a single product, including its variants and
 * images. */
export async function getProduct(id: string): Promise<Product> {
  const { data, error } = await api.GET("/products/{id}", {
    params: { path: { id } },
  });
  if (error) {
    throw new CatalogApiError(error.error.code);
  }
  return data;
}

/** `GET /products/{id}/variants` — not cursor-paginated (a product has at
 * most a handful of variants). `Variant.costOverride` is present only for
 * owner/manager — this module never reads it. */
export async function listVariants(productId: string): Promise<Variant[]> {
  const { data, error } = await api.GET("/products/{id}/variants", {
    params: { path: { id: productId } },
  });
  if (error) {
    throw new CatalogApiError(error.error.code);
  }
  return data.items;
}

export interface ListStockLevelsParams {
  variantId?: string;
  productId?: string;
  locationId?: string;
  cursor?: string | null;
}

/** `GET /stock/levels` — any authenticated role, cashier included (D-40).
 * No cost anywhere on `StockLevel`; it carries only `variantId`,
 * `productId`, `locationId` and `qty`. */
export async function listStockLevels({
  variantId,
  productId,
  locationId,
  cursor,
}: ListStockLevelsParams): Promise<CursorPage<StockLevel>> {
  const { data, error } = await api.GET("/stock/levels", {
    params: {
      query: {
        limit: PAGE_LIMIT,
        cursor: cursor ?? undefined,
        variantId,
        productId,
        locationId,
      },
    },
  });
  if (error) {
    throw new CatalogApiError(error.error.code);
  }
  return data;
}

/** Every page of `GET /stock/levels` for the given filters, flattened.
 * Used where the filter set is bounded — one product's variants across the
 * shop's locations, or one location's variants for the `VariantPicker` —
 * never for the unfiltered shop-wide list, which stays properly
 * cursor-paginated (there is no such screen in this task). */
export async function listAllStockLevels(
  filters: Omit<ListStockLevelsParams, "cursor">,
): Promise<StockLevel[]> {
  const all: StockLevel[] = [];
  let cursor: string | null = null;
  do {
    const page: CursorPage<StockLevel> = await listStockLevels({ ...filters, cursor });
    all.push(...page.items);
    cursor = page.nextCursor;
  } while (cursor);
  return all;
}

async function listLocationsPage(cursor: string | null): Promise<CursorPage<Location>> {
  const { data, error } = await api.GET("/locations", {
    params: { query: { limit: PAGE_LIMIT, cursor: cursor ?? undefined } },
  });
  if (error) {
    throw new CatalogApiError(error.error.code);
  }
  return data;
}

/** `GET /locations` — every page, flattened. Not cursor-paginated in
 * practice (a shop has a handful of locations); loops the (rare) extra
 * page rather than exposing cursor plumbing callers here don't need.
 * Mirrors `admin/src/stock/api.ts`'s `fetchAllLocations`. */
export async function listLocations(): Promise<Location[]> {
  const all: Location[] = [];
  let cursor: string | null = null;
  do {
    const page = await listLocationsPage(cursor);
    all.push(...page.items);
    cursor = page.nextCursor;
  } while (cursor);
  return all;
}
