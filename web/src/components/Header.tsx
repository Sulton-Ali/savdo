import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import type { Locale } from "../lib/locale";
import { LanguageSwitcher } from "./LanguageSwitcher";
import { TelegramButton } from "./landing/TelegramButton";

const navLinkClass = "text-muted transition hover:text-text";

/**
 * phase-7.5 T4: adds the design canvas's header Telegram button next to the
 * (now restyled) language switcher — `telegramHref` is already
 * `isSafeHttpsUrl`-checked by the `$locale` layout, same source
 * (`shop.blocks.social.telegram`) and the same null-means-hide rule as the
 * hero's own button (`HomePage`'s `telegramHref`); `null` here just means
 * "no Telegram link" (the shop has not set one), not an error, so the
 * button is omitted rather than rendered disabled — no layout shift either
 * way, since the header never reserves space for it. The header itself
 * stays the full-bleed white (`bg-surface/95` + blur) sticky surface from
 * before this task; only the switcher and the new button are restyled.
 */
export function Header({
  shopName,
  locale,
  telegramHref,
}: {
  shopName: string;
  locale: Locale;
  telegramHref: string | null;
}) {
  const { t } = useTranslation();
  return (
    <header className="sticky top-0 z-10 border-bg border-b bg-surface/95 backdrop-blur">
      <div className="mx-auto flex max-w-5xl flex-col gap-3 px-4 py-4 sm:flex-row sm:items-center sm:justify-between sm:gap-4">
        <Link
          to="/$locale"
          params={{ locale }}
          className="whitespace-nowrap font-semibold text-lg text-text"
        >
          {shopName}
        </Link>
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
          <nav className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm">
            <a href={`/${locale}#products`} className={navLinkClass}>
              {t("web.nav.catalog")}
            </a>
            <Link
              to="/$locale/about"
              params={{ locale }}
              className={navLinkClass}
              activeProps={{ className: "font-semibold text-text" }}
            >
              {t("web.nav.about")}
            </Link>
          </nav>
          <LanguageSwitcher />
          {telegramHref != null && (
            <TelegramButton href={telegramHref} label={t("web.telegramCta")} responsive />
          )}
        </div>
      </div>
    </header>
  );
}
