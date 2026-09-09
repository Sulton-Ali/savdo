import { expect, test } from "@playwright/test";

/**
 * phase-7.5 T3: the page background (`--color-bg` #f5f5f4) moved from the
 * homepage's own `<main>` to the root `<body>`, so every public route
 * shares it — this pins that across the home, category and about routes
 * (the three the task named). `rgb(245, 245, 244)` is the browser's
 * resolved form of `#f5f5f4`.
 */
const PAGE_BG = "rgb(245, 245, 244)";
// phase-7.5 T4: the same three routes, at the mobile artboard's own width
// (`BMobile.dc.html` is 390px wide).
const MOBILE_VIEWPORT = { width: 390, height: 844 };

async function bodyBackground(page: import("@playwright/test").Page): Promise<string> {
  return page.evaluate(() => getComputedStyle(document.body).backgroundColor);
}

/**
 * phase-7.5 T4: walks every element under `<body>` and flags one that is
 * both (a) at least 95% of the viewport's width and (b) painted with a
 * solid background colour other than the page background — i.e. something
 * other than a white card floating on the page background is spanning
 * (near) full width. `header`/`footer` (and anything inside either) are the
 * one deliberate exception (design canvas: full-bleed white header/footer
 * surfaces) and are excluded via `closest`. Elements with a transparent
 * background never paint anything themselves (they show whatever is
 * beneath), so they are not offenders regardless of width.
 */
async function wideOffBackgroundElements(
  page: import("@playwright/test").Page,
): Promise<{ tag: string; className: string; background: string }[]> {
  const viewport = page.viewportSize();
  if (viewport == null) {
    throw new Error("expected an explicit viewport size");
  }
  const threshold = viewport.width * 0.95;
  return page.evaluate(
    ({ threshold, pageBg }) => {
      const offenders: { tag: string; className: string; background: string }[] = [];
      for (const el of Array.from(document.querySelectorAll("body *"))) {
        if (el.closest("header, footer") != null) {
          continue;
        }
        const rect = el.getBoundingClientRect();
        if (rect.width < threshold) {
          continue;
        }
        const background = getComputedStyle(el).backgroundColor;
        if (background === "rgba(0, 0, 0, 0)" || background === "transparent") {
          continue;
        }
        if (background === pageBg) {
          continue;
        }
        offenders.push({ tag: el.tagName, className: el.className.toString(), background });
      }
      return offenders;
    },
    { threshold, pageBg: PAGE_BG },
  );
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

test.describe("at the mobile artboard width (390px)", () => {
  test.use({ viewport: MOBILE_VIEWPORT });

  test("home page: site background, no off-background full-width element", async ({ page }) => {
    await page.goto("/uz");
    expect(await bodyBackground(page)).toBe(PAGE_BG);
    expect(await wideOffBackgroundElements(page)).toEqual([]);
  });

  test("about page: site background, no off-background full-width element", async ({ page }) => {
    await page.goto("/uz/about");
    expect(await bodyBackground(page)).toBe(PAGE_BG);
    expect(await wideOffBackgroundElements(page)).toEqual([]);
  });

  test("category page: site background, no off-background full-width element", async ({ page }) => {
    await page.goto("/uz");
    const firstCategoryLink = page.locator('a[href^="/uz/c/"]').first();
    await expect(firstCategoryLink).toBeVisible();
    await firstCategoryLink.click();
    await page.waitForURL(/\/uz\/c\/[^/]+$/);

    expect(await bodyBackground(page)).toBe(PAGE_BG);
    expect(await wideOffBackgroundElements(page)).toEqual([]);
  });
});
