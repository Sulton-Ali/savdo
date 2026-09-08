import { expect, test } from "@playwright/test";

test.describe("SEO surface", () => {
  test("home page has a canonical link and hreflang alternates for every locale", async ({
    page,
  }) => {
    await page.goto("/uz");

    await expect(page.locator('link[rel="canonical"]')).toHaveCount(1);
    const canonicalHref = await page.locator('link[rel="canonical"]').getAttribute("href");
    expect(canonicalHref).toMatch(/\/uz\/?$/);

    // uz, ru, en, plus x-default (`buildHreflangLinks`).
    const hreflangLinks = page.locator('link[rel="alternate"][hreflang]');
    await expect(hreflangLinks).toHaveCount(4);
    const hreflangs = await hreflangLinks.evaluateAll((nodes) =>
      nodes.map((node) => node.getAttribute("hreflang")),
    );
    expect(hreflangs.sort()).toEqual(["en", "ru", "uz", "x-default"].sort());
  });

  test("/sitemap.xml returns a urlset", async ({ request }) => {
    const response = await request.get("/sitemap.xml");
    expect(response.status()).toBe(200);
    expect(response.headers()["content-type"]).toContain("xml");
    const body = await response.text();
    expect(body).toContain("<urlset");
    expect(body).toContain("<loc>");
  });

  test("/robots.txt allows crawling and points at the sitemap", async ({ request }) => {
    const response = await request.get("/robots.txt");
    expect(response.status()).toBe(200);
    const body = await response.text();
    expect(body).toMatch(/Allow:\s*\//);
    expect(body).toContain("Sitemap:");
    expect(body).toContain("/sitemap.xml");
  });
});
