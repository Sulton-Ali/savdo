import { expect, test } from "@playwright/test";

/**
 * phase-7.5 T3: the page background (`--color-bg` #f5f5f4) moved from the
 * homepage's own `<main>` to the root `<body>`, so every public route
 * shares it — this pins that across the home, category and about routes
 * (the three the task named). `rgb(245, 245, 244)` is the browser's
 * resolved form of `#f5f5f4`.
 */
const PAGE_BG = "rgb(245, 245, 244)";

async function bodyBackground(page: import("@playwright/test").Page): Promise<string> {
  return page.evaluate(() => getComputedStyle(document.body).backgroundColor);
}

test("home page body renders on the site background", async ({ page }) => {
  await page.goto("/uz");
  expect(await bodyBackground(page)).toBe(PAGE_BG);
});

test("about page body renders on the site background", async ({ page }) => {
  await page.goto("/uz/about");
  expect(await bodyBackground(page)).toBe(PAGE_BG);
});

test("category page body renders on the site background", async ({ page }) => {
  await page.goto("/uz");
  const firstCategoryLink = page.locator('a[href^="/uz/c/"]').first();
  await expect(firstCategoryLink).toBeVisible();
  await firstCategoryLink.click();
  await page.waitForURL(/\/uz\/c\/[^/]+$/);

  expect(await bodyBackground(page)).toBe(PAGE_BG);
});
