import { expect, test } from "@playwright/test";

/**
 * D-100: the locale is a path prefix and the language switcher swaps only
 * that segment, keeping the rest of the page — a full navigation (new SSR
 * render, new `<html lang>`), not a client-side transition (see
 * `LanguageSwitcher.tsx`'s doc comment). Discovers the first category from
 * the home page's `CategoryGrid` rather than hard-coding a slug, so the
 * test only depends on "there is at least one category with products" in
 * the seeded demo shop, not on a specific slug.
 */
test("locale switch keeps the page and changes the language", async ({ page }) => {
  await page.goto("/uz");

  const firstCategoryLink = page.locator('a[href^="/uz/c/"]').first();
  await expect(firstCategoryLink).toBeVisible();
  await firstCategoryLink.click();

  // The category page's own loader (an RPC call) runs client-side after the
  // URL updates — wait for it to settle before reading the heading, or a
  // read can race the transition and still see the home page's h1.
  await page.waitForURL(/^http:\/\/localhost:3100\/uz\/c\/[^/]+$/);
  await page.waitForLoadState("networkidle");
  const categorySlug = new URL(page.url()).pathname.replace(/^\/uz\/c\//, "");

  await expect(page.locator("html")).toHaveAttribute("lang", "uz");
  const heading = page.getByRole("heading", { level: 1 });
  const uzHeading = await heading.textContent();
  expect(uzHeading?.trim().length).toBeGreaterThan(0);

  // Switch to Russian. phase-7.5 T4: the switcher's visible label is the
  // two-letter locale code ("RU"), not the full `lang.ru` name — see
  // `LanguageSwitcher.tsx`'s doc comment.
  await page
    .getByRole("navigation", { name: "Tilni tanlang" })
    .getByRole("link", { name: "RU" })
    .click();
  await page.waitForURL(`/ru/c/${categorySlug}`);
  await page.waitForLoadState("networkidle");

  await expect(page.locator("html")).toHaveAttribute("lang", "ru");
  const ruHeading = await page.getByRole("heading", { level: 1 }).textContent();
  expect(ruHeading?.trim().length).toBeGreaterThan(0);
  // A real per-locale translation, not a copy of the uz heading — the
  // public API resolves category names per `Accept-Language` (D-100/O-20).
  expect(ruHeading).not.toBe(uzHeading);
  expect(ruHeading).toMatch(/[Ѐ-ӿ]/); // contains Cyrillic

  // Switch back to uz.
  await page
    .getByRole("navigation", { name: "Выберите язык" })
    .getByRole("link", { name: "UZ" })
    .click();
  await page.waitForURL(`/uz/c/${categorySlug}`);
  await page.waitForLoadState("networkidle");

  await expect(page.locator("html")).toHaveAttribute("lang", "uz");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(uzHeading ?? "");
});
