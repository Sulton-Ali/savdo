import { describe, expect, it } from "vitest";

import { buildRobotsTxt, buildSitemapXml } from "./sitemap";

const SITE = "https://savdo.example";

describe("buildSitemapXml", () => {
  it("starts with the XML declaration and urlset root (with the xhtml namespace)", () => {
    const xml = buildSitemapXml(SITE, [{ path: "" }]);
    expect(xml.startsWith('<?xml version="1.0" encoding="UTF-8"?>\n')).toBe(true);
    expect(xml).toContain(
      '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">',
    );
    expect(xml.trim().endsWith("</urlset>")).toBe(true);
  });

  it("emits one <url> per locale for every entry (3 locales × 2 entries = 6)", () => {
    const xml = buildSitemapXml(SITE, [{ path: "" }, { path: "/about" }]);
    expect((xml.match(/<url>/g) ?? []).length).toBe(6);
  });

  it("carries the locale's own <loc> and hreflang alternates to the other locales", () => {
    const xml = buildSitemapXml(SITE, [{ path: "/c/shirts" }]);
    expect(xml).toContain("<loc>https://savdo.example/uz/c/shirts</loc>");
    expect(xml).toContain(
      '<xhtml:link rel="alternate" hreflang="ru" href="https://savdo.example/ru/c/shirts"/>',
    );
    expect(xml).toContain(
      '<xhtml:link rel="alternate" hreflang="x-default" href="https://savdo.example/uz/c/shirts"/>',
    );
  });

  it("XML-escapes an unsafe character in a slug", () => {
    const xml = buildSitemapXml(SITE, [{ path: "/p/salt&pepper" }]);
    expect(xml).toContain("<loc>https://savdo.example/uz/p/salt&amp;pepper</loc>");
    expect(xml).not.toContain("salt&pepper<");
  });

  it("returns just the empty urlset for no entries", () => {
    const xml = buildSitemapXml(SITE, []);
    expect(xml).not.toContain("<url>");
  });
});

describe("buildRobotsTxt", () => {
  it("allows every crawler and points at the absolute sitemap URL", () => {
    const txt = buildRobotsTxt(SITE);
    expect(txt).toContain("User-agent: *");
    expect(txt).toContain("Allow: /");
    expect(txt).toContain("Sitemap: https://savdo.example/sitemap.xml");
  });
});
