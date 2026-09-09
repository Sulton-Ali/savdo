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
 *
 * phase-7.5 T4 polish: one sticky row at every width, matching the design
 * canvas's `BMobile`/`Components` header block — below `sm` the catalog/
 * about `nav` is hidden (`hidden sm:flex`) rather than wrapped, so the row
 * stays a fixed ~68px (`py-3` + the 44px touch targets, `sm:py-4` once the
 * row's own content — not the 44px touch targets — sets the height) instead
 * of growing
 * to three rows at phone width; both pages stay reachable via the footer's
 * own catalog/about links (`Footer.tsx`). The shop name is `flex-1
 * min-w-0 truncate` so a long name yields space to the switcher/Telegram
 * group (`shrink-0`) rather than wrapping the row.
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
      <div className="mx-auto flex max-w-5xl items-center justify-between gap-3 px-4 py-3 sm:gap-4 sm:py-4">
        <Link
          to="/$locale"
          params={{ locale }}
          className="min-w-0 flex-1 truncate font-semibold text-lg text-text"
        >
          {shopName}
        </Link>
        <div className="flex shrink-0 items-center gap-3 sm:gap-4">
          <nav className="hidden items-center gap-x-4 text-sm sm:flex">
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
