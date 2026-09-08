import type { components } from "@savdo/api-client";
import type { AnyRouteMatch } from "@tanstack/react-router";

import { defaultLocale, type Locale, locales } from "./locale";

type ProductImage = components["schemas"]["ProductImage"];
type MediaUrls = components["schemas"]["MediaUrls"];

/**
 * One `<link>` tag descriptor (canonical or `hreflang` alternate). A
 * route's `head()` `links` field resolves (via this router's own
 * `RouteMatchExtensions` augmentation, `Matches.tsx`) to
 * `JSX.IntrinsicElements['link']`, and every field this module uses
 * (`rel`/`href`/`hrefLang`) is a real anchor/link attribute, so this type
 * is structurally compatible with no cast needed — unlike `MetaTag` below.
 */
export type LinkDescriptor = {
  rel: string;
  href: string;
  hrefLang?: string;
};

/**
 * One `<meta>` tag descriptor. React's `MetaHTMLAttributes` type (used by
 * `JSX.IntrinsicElements['meta']`, which `head()`'s `meta` field resolves
 * to) only models `charSet`/`content`/`httpEquiv`/`media`/`name` — it has
 * no `property`, even though `property="og:title"` is valid HTML/RDFa and
 * is exactly what the Open Graph protocol requires. `buildSeoHead`'s
 * return is cast once, at the very end of this module (`asRouteMatchHead`),
 * to the router's own `AnyRouteMatch['meta']`/`['links']` types — safe at
 * runtime (verified with `curl` in the T5b report: every `property="og:*"`
 * tag renders and is read correctly), just outside what `@types/react`
 * models for `<meta>`.
 */
export type MetaTag =
  | { title: string }
  | { name: string; content: string }
  | { property: string; content: string };

/**
 * Joins `siteUrl` with an absolute-from-root `path`, tolerating a
 * trailing slash on either side (defensive — `site.server.ts` already
 * strips it, but this stays correct if that ever changes). Returns `path`
 * unchanged when it is already an absolute `http(s)` URL — media URLs from
 * `MediaUrls` are host-relative today (Caddy fronts the web app and the
 * API's `/media/*` under the same domain in production, so a relative
 * `<img src>` already resolves correctly on-page), but Open Graph/Twitter
 * crawlers fetch `og:image`/`twitter:image` directly over HTTP with no
 * page context to resolve a relative URL against, so those specific tags
 * must always be absolute (see `chooseOgImage`).
 */
export function absoluteUrl(siteUrl: string, path: string): string {
  if (/^https?:\/\//i.test(path)) {
    return path;
  }
  const base = siteUrl.replace(/\/+$/, "");
  const suffix = path.startsWith("/") ? path : `/${path}`;
  return `${base}${suffix}`;
}

/**
 * D-100: the locale is a path prefix (`/uz`, `/ru`, `/en`); `path` is
 * everything after it — `""` for home, `"/about"`, `"/c/<slug>"`,
 * `"/p/<slug>"`.
 */
export function buildCanonicalUrl(siteUrl: string, locale: Locale, path: string): string {
  return absoluteUrl(siteUrl, `/${locale}${path}`);
}

/**
 * `hreflang` alternates for every locale plus `x-default`, which points at
 * `uz` — the default and canonical locale (D-100).
 */
export function buildHreflangLinks(siteUrl: string, path: string): LinkDescriptor[] {
  return [
    ...locales.map((locale) => ({
      rel: "alternate",
      hrefLang: locale,
      href: buildCanonicalUrl(siteUrl, locale, path),
    })),
    {
      rel: "alternate",
      hrefLang: "x-default",
      href: buildCanonicalUrl(siteUrl, defaultLocale, path),
    },
  ];
}

const OG_LOCALE: Record<Locale, string> = { uz: "uz_UZ", ru: "ru_RU", en: "en_US" };

/** `og:locale` value for a `Locale` (deliverable 2). */
export function ogLocale(locale: Locale): string {
  return OG_LOCALE[locale];
}

export type SeoImage = {
  url: string;
  width?: number;
  height?: number;
};

/**
 * O-18's image rule: a product's own cover image, else the shop's hero
 * photo, else a static brand image. `productImages` is a product's full
 * `images` array (only ever passed on the product page); `heroImage` is
 * `PublicShop.blocks.hero.image` (home/category/about pages, when set).
 * `siteUrl` absolutizes a chosen media URL (see `absoluteUrl`'s doc
 * comment) — `fallbackUrl` is expected to already be absolute.
 */
export function chooseOgImage(options: {
  productImages?: readonly ProductImage[];
  heroImage?: MediaUrls;
  siteUrl: string;
  fallbackUrl: string;
}): SeoImage {
  const cover = options.productImages?.find((image) => image.isCover) ?? options.productImages?.[0];
  if (cover != null) {
    return { url: absoluteUrl(options.siteUrl, cover.urls.full) };
  }
  if (options.heroImage != null) {
    return { url: absoluteUrl(options.siteUrl, options.heroImage.full) };
  }
  // The static brand image is exactly 1200×630 (`web/public/og-default.png`).
  return { url: options.fallbackUrl, width: 1200, height: 630 };
}

export type SeoInput = {
  siteUrl: string;
  locale: Locale;
  /** Path after the locale prefix — see `buildCanonicalUrl`. */
  path: string;
  title: string;
  description: string;
  image: SeoImage;
};

/**
 * Builds the `<title>`/meta description, Open Graph and Twitter card tags,
 * canonical `<link>` and `hreflang` alternates for one page (deliverable
 * 2). Pure data — this module never renders anything, so it stays
 * unit-testable without a router or DOM. Every route's `head()` returns
 * this directly (via `asRouteMatchHead` below).
 */
export function buildSeoHead(input: SeoInput): {
  meta: MetaTag[];
  links: LinkDescriptor[];
} {
  const canonical = buildCanonicalUrl(input.siteUrl, input.locale, input.path);
  const meta: MetaTag[] = [
    { title: input.title },
    { name: "description", content: input.description },
    { property: "og:type", content: "website" },
    { property: "og:title", content: input.title },
    { property: "og:description", content: input.description },
    { property: "og:url", content: canonical },
    { property: "og:image", content: input.image.url },
    { property: "og:locale", content: ogLocale(input.locale) },
    { name: "twitter:card", content: "summary_large_image" },
    { name: "twitter:title", content: input.title },
    { name: "twitter:description", content: input.description },
    { name: "twitter:image", content: input.image.url },
  ];
  if (input.image.width != null) {
    meta.push({ property: "og:image:width", content: String(input.image.width) });
  }
  if (input.image.height != null) {
    meta.push({ property: "og:image:height", content: String(input.image.height) });
  }
  const links: LinkDescriptor[] = [
    { rel: "canonical", href: canonical },
    ...buildHreflangLinks(input.siteUrl, input.path),
  ];
  return { meta, links };
}

/**
 * Casts `buildSeoHead`'s result to what a route's `head()` callback must
 * return (`AnyRouteMatch['meta']`/`['links']`) — see `MetaTag`'s doc
 * comment for why `meta` needs it. Every page route calls this at its
 * `head()` return statement instead of returning `buildSeoHead(...)`
 * directly.
 */
export function asRouteMatchHead(head: { meta: MetaTag[]; links: LinkDescriptor[] }): {
  meta: AnyRouteMatch["meta"];
  links: AnyRouteMatch["links"];
} {
  return head as unknown as { meta: AnyRouteMatch["meta"]; links: AnyRouteMatch["links"] };
}
