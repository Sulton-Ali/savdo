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
 *
 * phase-7.5 T4: restyled as the design canvas's compact segmented pill
 * (`Components.dc.html`'s Header block) — a rounded track
 * (`landing-switch-track`, #f0e9e0) holding one pill per locale, the active
 * one filled solid. The visible label is the two-letter code (`UZ`/`RU`/
 * `EN`, same on every artboard regardless of the current locale — it is not
 * translated); `locale.toUpperCase()` is written into the DOM directly
 * rather than relying on a CSS `uppercase` transform on the lowercase
 * `locale` string, since the accessible-name algorithm is inconsistent
 * about reflecting `text-transform` (Chromium does, jsdom — this
 * component's own unit test — does not run layout at all) — this way the
 * rendered text and the accessible name are identical everywhere. No
 * `aria-label` override either, so the visible text and the accessible name
 * always match (WCAG 2.5.3). Each pill is `min-h-11 min-w-11` (44px, the
 * touch target floor) below `sm`, stepping down to the canvas's own 36px on
 * `sm` and up, where a mouse pointer — not a fingertip — is the common
 * input. Filled with `landing-accent-hover`, not the base `landing-accent`:
 * same reasoning as `landing/TelegramButton.tsx`'s doc comment — white on
 * the base tone is ~3.67:1 (large text/UI only), the darker step ~4.64:1
 * (passes this pill's 13px/600 label too, which is not "large text" by the
 * WCAG threshold).
 */
export function LanguageSwitcher() {
  const { t, i18n } = useTranslation();
  const location = useLocation();
  const current = i18n.language;

  return (
    <nav
      aria-label={t("lang.switch")}
      className="inline-flex items-center gap-0.5 rounded-full bg-landing-switch-track p-[3px]"
    >
      {locales.map((locale) => {
        const active = locale === current;
        return (
          <a
            key={locale}
            href={switchLocalePath(location.pathname, locale)}
            aria-current={active ? "true" : undefined}
            className={`inline-flex min-h-11 min-w-11 items-center justify-center rounded-full px-2 font-semibold text-xs tracking-wide transition sm:min-h-9 sm:min-w-10 sm:text-[13px] ${
              active
                ? "bg-landing-accent-hover text-landing-on-accent"
                : "text-muted hover:text-text"
            }`}
          >
            {locale.toUpperCase()}
          </a>
        );
      })}
    </nav>
  );
}
