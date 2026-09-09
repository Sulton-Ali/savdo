import { expect, test } from "@playwright/test";

/**
 * phase-7.5 T4: the header's two restyled controls — the language switcher
 * (`LanguageSwitcher.tsx`) and the new Telegram button (`Header.tsx`) —
 * still do their job after the visual rework, at both the mobile artboard
 * width (390px, `BMobile.dc.html`) and the desktop one (1280px,
 * `BDesktop.dc.html`). The switcher's visible label is now the two-letter
 * locale code (`UZ`/`RU`/`EN`, not the full `lang.*` name — see
 * `LanguageSwitcher.tsx`'s doc comment) and doubles as its own accessible
 * name, so `getByRole("link", { name: "RU" })` finds it directly.
 */
const VIEWPORTS = [
  { name: "390px phone", width: 390, height: 844 },
  { name: "1280px desktop", width: 1280, height: 900 },
];

for (const viewport of VIEWPORTS) {
  test(`header language switch navigates uz -> ru -> en (${viewport.name})`, async ({ page }) => {
    await page.setViewportSize({ width: viewport.width, height: viewport.height });
    await page.goto("/uz");
    await expect(page.locator("html")).toHaveAttribute("lang", "uz");

    await page
      .getByRole("navigation", { name: "Tilni tanlang" })
      .getByRole("link", { name: "RU" })
      .click();
    await page.waitForURL("/ru");
    await page.waitForLoadState("networkidle");
    await expect(page.locator("html")).toHaveAttribute("lang", "ru");
    await expect(
      page.getByRole("navigation", { name: "Выберите язык" }).getByRole("link", { name: "RU" }),
    ).toHaveAttribute("aria-current", "true");

    await page
      .getByRole("navigation", { name: "Выберите язык" })
      .getByRole("link", { name: "EN" })
      .click();
    await page.waitForURL("/en");
    await page.waitForLoadState("networkidle");
    await expect(page.locator("html")).toHaveAttribute("lang", "en");
    await expect(
      page.getByRole("navigation", { name: "Select language" }).getByRole("link", { name: "EN" }),
    ).toHaveAttribute("aria-current", "true");
  });

  test(`header Telegram button links to the shop's Telegram (${viewport.name})`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: viewport.width, height: viewport.height });
    await page.goto("/uz");

    // Scoped to the `<header>` landmark — the homepage's own hero/closing
    // CTA also carry a Telegram link with the same accessible name
    // (`homepage-landing.spec.ts` covers those); this test is only about
    // the header's own button.
    const telegramLink = page.locator("header").getByRole("link", { name: "Telegramda yozing" });
    await expect(telegramLink).toBeVisible();
    const href = await telegramLink.getAttribute("href");
    expect(href).toMatch(/^https:\/\/t\.me\//);
    expect(await telegramLink.getAttribute("target")).toBe("_blank");
  });
}
