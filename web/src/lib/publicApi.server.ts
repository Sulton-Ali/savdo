import type { components } from "@savdo/api-client";
import { createClient } from "@savdo/api-client";
import type { Locale } from "@savdo/i18n";

/**
 * Builds the typed public client bound to `API_URL`, sending the requested
 * `locale` as `Accept-Language` — the Go API resolves every `/public/*`
 * response for that header (D-100/O-20/O-21), so this module never parses
 * or re-derives locale fallback itself. Building the client happens only in
 * this `*.server.ts` module: TanStack Start's import-protection convention
 * keeps `API_URL` out of the client bundle (ADR-011), same pattern as the
 * existing `api.server.ts`/`fetchHealthz`. Only ever call these from inside
 * a `createServerFn` handler (`publicApi.functions.ts`), never directly
 * from route/component code.
 */
function client(locale: Locale) {
  return createClient(process.env.API_URL ?? "http://localhost:8080/v1", {
    headers: { "Accept-Language": locale },
  });
}

export type ListProductsParams = {
  category?: string;
  featured?: boolean;
  cursor?: string;
  limit?: number;
};

/** Returns the shop, `null` when `PUBLIC_SHOP_SLUG` (D-105) does not
 * resolve to a shop (404) — the caller decides what that means (the whole
 * site has nothing to render without a shop, so callers throw `notFound()`). */
export async function fetchPublicShop(
  locale: Locale,
): Promise<components["schemas"]["PublicShop"] | null> {
  const { data, error, response } = await client(locale).GET("/public/shop");
  if (error) {
    if (response.status === 404) {
      return null;
    }
    throw new Error(`GET /public/shop failed: ${response.status}`);
  }
  return data;
}

export async function fetchPublicCategories(
  locale: Locale,
): Promise<components["schemas"]["PublicCategoryList"]> {
  const { data, error, response } = await client(locale).GET("/public/categories");
  if (error) {
    throw new Error(`GET /public/categories failed: ${response.status}`);
  }
  return data;
}

export async function fetchPublicProducts(
  locale: Locale,
  params: ListProductsParams,
): Promise<components["schemas"]["PublicProductList"]> {
  const { data, error, response } = await client(locale).GET("/public/products", {
    params: { query: params },
  });
  if (error) {
    throw new Error(`GET /public/products failed: ${response.status}`);
  }
  return data;
}

/** Returns the product, `null` on 404 (unknown slug or an inactive/hidden
 * product/category, O-22) — the caller throws `notFound()`. */
export async function fetchPublicProduct(
  locale: Locale,
  slug: string,
): Promise<components["schemas"]["ProductPublic"] | null> {
  const { data, error, response } = await client(locale).GET("/public/products/{slug}", {
    params: { path: { slug } },
  });
  if (error) {
    if (response.status === 404) {
      return null;
    }
    throw new Error(`GET /public/products/${slug} failed: ${response.status}`);
  }
  return data;
}
