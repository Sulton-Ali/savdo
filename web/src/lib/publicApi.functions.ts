import { notFound } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";

import { isLocale, type Locale } from "./locale";
import {
  fetchPublicCategories,
  fetchPublicProduct,
  fetchPublicProducts,
  fetchPublicShop,
  type ListProductsParams,
} from "./publicApi.server";

/**
 * Client-safe handles to the server-only `publicApi.server` fetchers.
 * TanStack Start replaces these handlers with RPC stubs in the client
 * bundle, so `API_URL` never leaves the server (ADR-011) — same pattern as
 * `api.functions.ts`/`getHealthz`.
 *
 * Server functions are reachable as their own HTTP endpoints regardless of
 * which route calls them, so `locale` is validated at the handler boundary
 * too (not just trusted from the caller's already-narrowed `Locale` type).
 */

function requireLocale(locale: string): Locale {
  if (!isLocale(locale)) {
    throw new Error(`invalid locale: ${locale}`);
  }
  return locale;
}

export const getPublicShop = createServerFn({ method: "GET" })
  .validator((data: { locale: string }) => data)
  .handler(async ({ data }) => {
    const shop = await fetchPublicShop(requireLocale(data.locale));
    if (!shop) {
      throw notFound();
    }
    return shop;
  });

export const listPublicCategories = createServerFn({ method: "GET" })
  .validator((data: { locale: string }) => data)
  .handler(async ({ data }) => fetchPublicCategories(requireLocale(data.locale)));

export const listPublicProducts = createServerFn({ method: "GET" })
  .validator((data: { locale: string } & ListProductsParams) => data)
  .handler(async ({ data: { locale, ...params } }) =>
    fetchPublicProducts(requireLocale(locale), params),
  );

export const getPublicProductBySlug = createServerFn({ method: "GET" })
  .validator((data: { locale: string; slug: string }) => data)
  .handler(async ({ data }) => {
    const product = await fetchPublicProduct(requireLocale(data.locale), data.slug);
    if (!product) {
      throw notFound();
    }
    return product;
  });
