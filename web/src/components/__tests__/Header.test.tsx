import { render, screen, within } from "@testing-library/react";
import type { ReactNode } from "react";
import { I18nextProvider } from "react-i18next";
import { describe, expect, it, vi } from "vitest";

import { createI18nInstance } from "../../lib/i18n";
import { Header } from "../Header";

// The router isn't under test here — `Header`/`LanguageSwitcher` only use
// `Link` (rendered as a plain anchor) and `useLocation` (a fixed pathname,
// same shape `switchLocalePath` needs) from it, so a real `RouterProvider`
// would add ceremony without adding coverage.
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, className, to }: { children: ReactNode; className?: string; to: string }) => (
    <a href={to} className={className}>
      {children}
    </a>
  ),
  useLocation: () => ({ pathname: "/uz" }),
}));

function renderHeader(locale: "uz" | "ru" | "en", telegramHref: string | null) {
  render(
    <I18nextProvider i18n={createI18nInstance(locale)}>
      <Header shopName="Savdo Demo" locale={locale} telegramHref={telegramHref} />
    </I18nextProvider>,
  );
}

describe("Header", () => {
  it("renders all three locales, marking the active one", () => {
    renderHeader("uz", null);

    const switcher = within(screen.getByRole("navigation", { name: "Tilni tanlang" }));
    const uzLink = switcher.getByRole("link", { name: "UZ" });
    const ruLink = switcher.getByRole("link", { name: "RU" });
    const enLink = switcher.getByRole("link", { name: "EN" });

    expect(uzLink.getAttribute("aria-current")).toBe("true");
    expect(ruLink.getAttribute("aria-current")).toBeNull();
    expect(enLink.getAttribute("aria-current")).toBeNull();

    expect(ruLink.getAttribute("href")).toBe("/ru");
    expect(enLink.getAttribute("href")).toBe("/en");
  });

  it("omits the Telegram button when the shop has no Telegram link", () => {
    renderHeader("uz", null);
    expect(screen.queryByRole("link", { name: "Telegramda yozing" })).toBeNull();
  });

  it("renders the Telegram button with the shop's link when present", () => {
    renderHeader("uz", "https://t.me/savdo_demo");
    const link = screen.getByRole("link", { name: "Telegramda yozing" });
    expect(link.getAttribute("href")).toBe("https://t.me/savdo_demo");
    expect(link.getAttribute("target")).toBe("_blank");
  });
});
