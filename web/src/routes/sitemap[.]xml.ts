import { createFileRoute } from "@tanstack/react-router";

import { defaultLocale } from "../lib/locale";
import { fetchPublicCategories, fetchPublicProducts } from "../lib/publicApi.server";
import { getSiteUrlValue } from "../lib/site.server";
import { buildSitemapXml, type SitemapEntry } from "../lib/sitemap";

const PAGE_LIMIT = 200;

/**
 * Slugs are locale-independent routing identifiers, not translated content
 * (D-100), so walking the catalogue in one locale (`uz`, the default) is
 * enough — `buildSitemapXml` itself emits the uz/ru/en/x-default rows for
 * every entry regardless of which locale its slug came from.
 */
async function collectProductPaths(): Promise<string[]> {
  const paths: string[] = [];
  let cursor: string | undefined;
  for (;;) {
    const page = await fetchPublicProducts(defaultLocale, { limit: PAGE_LIMIT, cursor });
    paths.push(...page.items.map((item) => `/p/${item.slug}`));
    if (page.nextCursor == null) {
      return paths;
    }
    cursor = page.nextCursor;
  }
}

/**
 * O-17 sitemap: home, every active category, every active product and the
 * about page, once per locale. A `server.handlers` route rather than a
 * `createServerFn` — it must be reachable at a real, crawlable URL, not
 * only as an RPC endpoint (`@tanstack/start` `server-routes` skill).
 * Reads straight from `publicApi.server`'s fetchers (the same functions
 * the `createServerFn`s in `publicApi.functions.ts` wrap): this handler
 * already runs server-only, so the client-safe RPC wrapper is unnecessary
 * here (see that skill's "Sharing Data with a Start Route" example). The
 * underlying `/public/*` calls are cached 60 s by the API itself (D-106),
 * so this route adds no caching of its own.
 */
export const Route = createFileRoute("/sitemap.xml")({
  server: {
    handlers: {
      GET: async () => {
        const siteUrl = getSiteUrlValue();
        const [categories, productPaths] = await Promise.all([
          fetchPublicCategories(defaultLocale),
          collectProductPaths(),
        ]);
        const entries: SitemapEntry[] = [
          { path: "" },
          { path: "/about" },
          ...categories.items.map((category) => ({ path: `/c/${category.slug}` })),
          ...productPaths.map((path) => ({ path })),
        ];
        return new Response(buildSitemapXml(siteUrl, entries), {
          headers: { "Content-Type": "application/xml; charset=utf-8" },
        });
      },
    },
  },
});
