import { describe, expect, it } from "vitest";

import {
  absoluteUrl,
  buildCanonicalUrl,
  buildHreflangLinks,
  buildSeoHead,
  chooseOgImage,
  ogLocale,
} from "./seo";

const SITE = "https://savdo.example";

describe("absoluteUrl", () => {
  it("joins siteUrl with a root-relative path", () => {
    expect(absoluteUrl(SITE, "/og-default.png")).toBe("https://savdo.example/og-default.png");
  });

  it("leaves an already-absolute http(s) URL unchanged", () => {
    expect(absoluteUrl(SITE, "https://cdn.example/x.png")).toBe("https://cdn.example/x.png");
    expect(absoluteUrl(SITE, "http://cdn.example/x.png")).toBe("http://cdn.example/x.png");
  });
});

describe("buildCanonicalUrl", () => {
  it("puts the locale as a path prefix before the page path", () => {
    expect(buildCanonicalUrl(SITE, "uz", "")).toBe("https://savdo.example/uz");
    expect(buildCanonicalUrl(SITE, "ru", "/about")).toBe("https://savdo.example/ru/about");
    expect(buildCanonicalUrl(SITE, "en", "/p/blue-shirt")).toBe(
      "https://savdo.example/en/p/blue-shirt",
    );
  });

  it("tolerates a trailing slash on siteUrl", () => {
    expect(buildCanonicalUrl(`${SITE}/`, "uz", "/about")).toBe("https://savdo.example/uz/about");
  });
});

describe("buildHreflangLinks", () => {
  it("returns uz/ru/en plus x-default pointing at uz (D-100)", () => {
    const links = buildHreflangLinks(SITE, "/c/shirts");
    expect(links).toEqual([
      { rel: "alternate", hrefLang: "uz", href: "https://savdo.example/uz/c/shirts" },
      { rel: "alternate", hrefLang: "ru", href: "https://savdo.example/ru/c/shirts" },
      { rel: "alternate", hrefLang: "en", href: "https://savdo.example/en/c/shirts" },
      { rel: "alternate", hrefLang: "x-default", href: "https://savdo.example/uz/c/shirts" },
    ]);
  });
});

describe("ogLocale", () => {
  it("maps every Locale to its IETF-ish og:locale value", () => {
    expect(ogLocale("uz")).toBe("uz_UZ");
    expect(ogLocale("ru")).toBe("ru_RU");
    expect(ogLocale("en")).toBe("en_US");
  });
});

describe("chooseOgImage", () => {
  const siteUrl = SITE;
  const fallbackUrl = "https://savdo.example/og-default.png";
  // Media URLs from the API are host-relative (Caddy fronts web + API under
  // one domain in production), so the fixtures use relative paths to
  // exercise the absolutizing behaviour that og:image/twitter:image need.
  const heroImage = { thumb: "/media/hero-t", card: "/media/hero-c", full: "/media/hero-f" };

  it("prefers the product's own cover image over the hero photo, absolutized against siteUrl", () => {
    const productImages = [
      {
        id: "1",
        mediaId: "m1",
        variantId: null,
        isCover: false,
        sortOrder: 0,
        urls: { thumb: "/media/1t", card: "/media/1c", full: "/media/1f" },
      },
      {
        id: "2",
        mediaId: "m2",
        variantId: null,
        isCover: true,
        sortOrder: 1,
        urls: { thumb: "/media/2t", card: "/media/2c", full: "/media/2f" },
      },
    ];
    expect(chooseOgImage({ siteUrl, productImages, heroImage, fallbackUrl })).toEqual({
      url: "https://savdo.example/media/2f",
    });
  });

  it("falls back to the first image when none is flagged isCover", () => {
    const productImages = [
      {
        id: "1",
        mediaId: "m1",
        variantId: null,
        isCover: false,
        sortOrder: 0,
        urls: { thumb: "/media/1t", card: "/media/1c", full: "/media/1f" },
      },
    ];
    expect(chooseOgImage({ siteUrl, productImages, heroImage, fallbackUrl })).toEqual({
      url: "https://savdo.example/media/1f",
    });
  });

  it("uses the hero photo when there is no product image (category/about/home pages)", () => {
    expect(chooseOgImage({ siteUrl, heroImage, fallbackUrl })).toEqual({
      url: "https://savdo.example/media/hero-f",
    });
  });

  it("falls back to the static brand image with its known size when neither exists", () => {
    expect(chooseOgImage({ siteUrl, fallbackUrl })).toEqual({
      url: fallbackUrl,
      width: 1200,
      height: 630,
    });
  });

  it("treats an empty productImages array the same as none (falls through to hero)", () => {
    expect(chooseOgImage({ siteUrl, productImages: [], heroImage, fallbackUrl })).toEqual({
      url: "https://savdo.example/media/hero-f",
    });
  });

  it("leaves an already-absolute media URL untouched", () => {
    const productImages = [
      {
        id: "1",
        mediaId: "m1",
        variantId: null,
        isCover: true,
        sortOrder: 0,
        urls: {
          thumb: "https://cdn.example/1t",
          card: "https://cdn.example/1c",
          full: "https://cdn.example/1f",
        },
      },
    ];
    expect(chooseOgImage({ siteUrl, productImages, fallbackUrl })).toEqual({
      url: "https://cdn.example/1f",
    });
  });
});

describe("buildSeoHead", () => {
  it("builds title/description meta, OG/Twitter tags and canonical + hreflang links", () => {
    const { meta, links } = buildSeoHead({
      siteUrl: SITE,
      locale: "ru",
      path: "/p/blue-shirt",
      title: "Blue shirt",
      description: "A comfortable blue shirt.",
      image: { url: "https://savdo.example/img/blue.png" },
    });

    expect(meta).toContainEqual({ title: "Blue shirt" });
    expect(meta).toContainEqual({ name: "description", content: "A comfortable blue shirt." });
    expect(meta).toContainEqual({ property: "og:title", content: "Blue shirt" });
    expect(meta).toContainEqual({
      property: "og:url",
      content: "https://savdo.example/ru/p/blue-shirt",
    });
    expect(meta).toContainEqual({
      property: "og:image",
      content: "https://savdo.example/img/blue.png",
    });
    expect(meta).toContainEqual({ property: "og:locale", content: "ru_RU" });
    expect(meta).toContainEqual({ name: "twitter:card", content: "summary_large_image" });
    expect(meta).toContainEqual({
      name: "twitter:image",
      content: "https://savdo.example/img/blue.png",
    });
    // No width/height meta when the image doesn't carry them.
    expect(meta.some((tag) => "property" in tag && tag.property === "og:image:width")).toBe(false);

    expect(links[0]).toEqual({
      rel: "canonical",
      href: "https://savdo.example/ru/p/blue-shirt",
    });
    expect(links).toHaveLength(1 + 4); // canonical + 3 locales + x-default
  });

  it("adds og:image:width/height only when the image carries them", () => {
    const { meta } = buildSeoHead({
      siteUrl: SITE,
      locale: "uz",
      path: "",
      title: "Savdo",
      description: "Savdo",
      image: { url: "https://savdo.example/og-default.png", width: 1200, height: 630 },
    });
    expect(meta).toContainEqual({ property: "og:image:width", content: "1200" });
    expect(meta).toContainEqual({ property: "og:image:height", content: "630" });
  });
});
