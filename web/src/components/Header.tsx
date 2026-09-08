import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import type { Locale } from "../lib/locale";
import { LanguageSwitcher } from "./LanguageSwitcher";

const navLinkClass = "text-muted transition hover:text-text";

export function Header({ shopName, locale }: { shopName: string; locale: Locale }) {
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
          <LanguageSwitcher />
        </nav>
      </div>
    </header>
  );
}
