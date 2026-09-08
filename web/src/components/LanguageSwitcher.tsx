import { useLocation } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { locales, switchLocalePath } from "../lib/locale";

/**
 * Swaps the `/uz|/ru|/en` path prefix, keeping the rest of the page
 * (D-100). Plain `<a>` tags rather than `<Link>`: switching locale is a
 * full navigation on purpose — it needs a fresh SSR render (new
 * `Accept-Language`, new i18n instance, new `<html lang>`), not a soft
 * client-side transition. The target path is a computed string, not a
 * statically known route pattern the router can type-check.
 */
export function LanguageSwitcher() {
  const { t, i18n } = useTranslation();
  const location = useLocation();
  const current = i18n.language;

  return (
    <nav aria-label={t("lang.switch")} className="flex gap-3 text-sm">
      {locales.map((locale) => (
        <a
          key={locale}
          href={switchLocalePath(location.pathname, locale)}
          aria-current={locale === current ? "true" : undefined}
          className={locale === current ? "font-semibold text-text" : "text-muted"}
        >
          {t(`lang.${locale}`)}
        </a>
      ))}
    </nav>
  );
}
