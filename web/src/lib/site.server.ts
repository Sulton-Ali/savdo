/**
 * Reads this site's own public URL (`SITE_URL`, `web/README.md`) —
 * server-side only, same import-protection convention as
 * `api.server.ts`/`publicApi.server.ts` (ADR-011): only ever called from
 * inside a `createServerFn` handler (`site.functions.ts`) or another
 * server-only handler (the sitemap/robots server routes). Used to build
 * absolute URLs for canonical/`hreflang` links, Open Graph tags and the
 * sitemap (D-100 — Phase 6 uses a placeholder site URL, the real domain is
 * a Phase 8 decision, Q-09). Never has a trailing slash.
 */
export function getSiteUrlValue(): string {
  return (process.env.SITE_URL ?? "http://localhost:3000").replace(/\/+$/, "");
}
