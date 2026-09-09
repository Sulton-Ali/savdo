import { expect, test } from "@playwright/test";

/**
 * D-121 (Variant B "Warm cards"): the redesigned homepage's five sections —
 * hero, categories + featured products, hours + contacts, about + sample
 * quotes, closing Telegram CTA. Copy mirrors `packages/i18n/src/locales/*.json`
 * `web.home`/`web.hero`/`web.telegramCta` — kept in sync by hand, same
 * pattern as `product-availability.spec.ts`'s `AVAILABILITY_TEXT_UZ`.
 */
const COPY = {
  uz: {
    categoriesTitle: "Kategoriyalar",
    featuredTitle: "Tavsiya etilgan mahsulotlar",
    hoursTitle: "Ish vaqti",
    contactsTitle: "Aloqa",
    aboutTitle: "Bizning oilaviy do'konimiz",
    ctaHeading: "Savol bormi? Telegramda so'rang",
    telegramCta: "Telegramda yozing",
  },
  ru: {
    categoriesTitle: "Категории",
    featuredTitle: "Популярные товары",
    hoursTitle: "Часы работы",
    contactsTitle: "Контакты",
    aboutTitle: "Наш семейный магазин",
    ctaHeading: "Есть вопрос? Спросите в Telegram",
    telegramCta: "Написать в Telegram",
  },
  en: {
    categoriesTitle: "Categories",
    featuredTitle: "Featured products",
    hoursTitle: "Opening hours",
    contactsTitle: "Contacts",
    aboutTitle: "Our family shop",
    ctaHeading: "Got a question? Ask on Telegram",
    telegramCta: "Chat on Telegram",
  },
} as const;

const VIEWPORTS = [
  { name: "390px phone", width: 390, height: 844 },
  { name: "1280px desktop", width: 1280, height: 900 },
];

for (const locale of ["uz", "ru", "en"] as const) {
  const copy = COPY[locale];

  for (const viewport of VIEWPORTS) {
    test(`homepage renders the five Variant-B sections — ${locale}, ${viewport.name}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width: viewport.width, height: viewport.height });
      await page.goto(`/${locale}`);

      // 1. Hero — a non-empty <h1> and, since the seeded demo shop's
      // `social.telegram` is a safe https link, the Telegram CTA button.
      const h1 = page.getByRole("heading", { level: 1 });
      await expect(h1).toBeVisible();
      expect((await h1.textContent())?.trim().length).toBeGreaterThan(0);
      const telegramLinks = page.getByRole("link", { name: copy.telegramCta });
      await expect(telegramLinks.first()).toBeVisible();

      // 2. Categories + featured products.
      await expect(
        page.getByRole("heading", { level: 2, name: copy.categoriesTitle }),
      ).toBeVisible();
      await expect(page.getByRole("heading", { level: 2, name: copy.featuredTitle })).toBeVisible();

      // 3. Hours + contacts twin cards.
      await expect(page.getByRole("heading", { level: 2, name: copy.hoursTitle })).toBeVisible();
      await expect(page.getByRole("heading", { level: 2, name: copy.contactsTitle })).toBeVisible();

      // 4. About card + sample quotes (each quote card carries a "sample"
      // pill — the exact label differs per locale, but there are always two).
      await expect(page.getByRole("heading", { level: 2, name: copy.aboutTitle })).toBeVisible();
      await expect(page.locator("figure blockquote")).toHaveCount(2);

      // 5. Closing Telegram CTA card.
      await expect(page.getByText(copy.ctaHeading)).toBeVisible();

      // Every Telegram CTA on the page (hero + closing card) points at the
      // shop's own `https://t.me/...` link, never a placeholder `#`.
      const hrefs = await telegramLinks.evaluateAll((nodes) =>
        nodes.map((node) => node.getAttribute("href")),
      );
      expect(hrefs.length).toBeGreaterThan(0);
      for (const href of hrefs) {
        expect(href).toMatch(/^https:\/\/t\.me\//);
      }
    });
  }
}

test("category row card navigates to the category page", async ({ page }) => {
  await page.goto("/uz");

  const firstCategoryLink = page.locator('a[href^="/uz/c/"]').first();
  await expect(firstCategoryLink).toBeVisible();
  const categoryName = (await firstCategoryLink.textContent())?.trim();
  await firstCategoryLink.click();

  await page.waitForURL(/\/uz\/c\/[^/]+$/);
  await page.waitForLoadState("networkidle");

  const h1 = page.getByRole("heading", { level: 1 });
  await expect(h1).toBeVisible();
  const h1Text = (await h1.textContent())?.trim();
  expect(h1Text?.length).toBeGreaterThan(0);
  if (categoryName) {
    expect(categoryName).toContain(h1Text ?? "");
  }
});
