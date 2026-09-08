import { locales } from "./locale";
import { absoluteUrl, buildCanonicalUrl, buildHreflangLinks } from "./seo";

export type SitemapEntry = {
  /** Path after the locale prefix — same shape as `SeoInput.path` in `./seo`. */
  path: string;
};

function escapeXml(value: string): string {
  return value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&apos;");
}

/**
 * O-17: one `<url>` node per locale for every entry, each carrying
 * `xhtml:link hreflang` alternates to the other locales. The caller is
 * responsible for the entry list itself (home, every active category,
 * every active product, the about page — see `routes/sitemap[.]xml.ts`);
 * this module only ever turns an already-filtered list into XML, so it
 * never risks listing an inactive item. `lastmod` is intentionally
 * omitted — it's optional per O-17 and there is no reliable per-page
 * timestamp available where this is called from.
 */
export function buildSitemapXml(siteUrl: string, entries: readonly SitemapEntry[]): string {
  const urlNodes = entries.flatMap((entry) => {
    const alternateLinks = buildHreflangLinks(siteUrl, entry.path)
      .map(
        (link) =>
          `    <xhtml:link rel="alternate" hreflang="${escapeXml(link.hrefLang ?? "")}" href="${escapeXml(link.href)}"/>`,
      )
      .join("\n");
    return locales.map((locale) => {
      const loc = buildCanonicalUrl(siteUrl, locale, entry.path);
      return `  <url>\n    <loc>${escapeXml(loc)}</loc>\n${alternateLinks}\n  </url>`;
    });
  });
  return [
    '<?xml version="1.0" encoding="UTF-8"?>',
    '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">',
    ...urlNodes,
    "</urlset>",
    "",
  ].join("\n");
}

/** O-17: allows every crawler, points at `/sitemap.xml`. */
export function buildRobotsTxt(siteUrl: string): string {
  return [
    "User-agent: *",
    "Allow: /",
    "",
    `Sitemap: ${absoluteUrl(siteUrl, "/sitemap.xml")}`,
    "",
  ].join("\n");
}
