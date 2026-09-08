import { expect, test } from "@playwright/test";

/** uz copy for `Availability` (`packages/i18n/src/locales/uz.json` `web.availability`) —
 * mirrors `web/src/lib/availability.ts`'s three-state badge. */
const AVAILABILITY_TEXT_UZ = ["Mavjud", "Kam qoldi", "Tugagan"];

test("product page renders variants with availability badges and prices", async ({ page }) => {
  await page.goto("/uz");

  const firstProductLink = page.locator('a[href^="/uz/p/"]').first();
  await expect(firstProductLink).toBeVisible();
  const productName = (await firstProductLink.locator(".line-clamp-2").textContent())?.trim();
  await firstProductLink.click();

  // The product page's own loader (an RPC call) runs client-side after the
  // URL updates — wait for it to settle before reading the heading, or a
  // read can race the transition and still see the home page's h1.
  await page.waitForURL(/\/uz\/p\/[^/]+$/);
  await page.waitForLoadState("networkidle");

  const h1 = page.getByRole("heading", { level: 1 });
  await expect(h1).toBeVisible();
  const h1Text = (await h1.textContent())?.trim();
  expect(h1Text?.length).toBeGreaterThan(0);
  if (productName) {
    expect(h1Text).toBe(productName);
  }

  const variantRows = page
    .locator("h2", { hasText: "Variantlar" })
    .locator("xpath=following-sibling::ul[1]/li");
  await expect(variantRows.first()).toBeVisible();
  const rowCount = await variantRows.count();
  expect(rowCount).toBeGreaterThan(0);

  const firstRow = variantRows.first();
  await expect(firstRow).toContainText("UZS");
  const rowText = (await firstRow.textContent()) ?? "";
  expect(AVAILABILITY_TEXT_UZ.some((text) => rowText.includes(text))).toBe(true);

  // SSR check: fetch the same URL directly (no client JS execution) and
  // confirm the product name is already in the raw HTML — proves the page
  // is server-rendered, not filled in only after hydration.
  const response = await page.request.get(page.url());
  expect(response.status()).toBe(200);
  const html = await response.text();
  expect(h1Text).not.toBeUndefined();
  expect(html).toContain(h1Text as string);
});
