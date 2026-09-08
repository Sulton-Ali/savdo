import { resources } from "@savdo/i18n";
import i18next, { type i18n as I18nInstance } from "i18next";
import { initReactI18next } from "react-i18next";

import type { Locale } from "./locale";

/**
 * Creates a fresh i18next instance bound to `locale` — SSR-safe by
 * construction: `i18next.createInstance()` never touches the default
 * singleton export, so a request in flight for one locale/shop can never
 * see another request's language (the failure mode `admin/src/i18n.ts`'s
 * `i18next.use(...).init(...)` + `i18next.changeLanguage()` pattern would
 * have on a server that serves many requests from one process). Callers
 * (the `$locale` layout route's component) memoise this per render and
 * provide it via `I18nextProvider`; no component below ever imports
 * `i18next`'s default export.
 *
 * `resources` are the static `@savdo/i18n` JSON dictionaries (no network
 * fetch), so `init` resolves synchronously and `t()` is safe to use
 * immediately without awaiting the returned promise — same assumption
 * `admin/src/i18n.ts` already relies on.
 */
export function createI18nInstance(locale: Locale): I18nInstance {
  const instance = i18next.createInstance();
  void instance.use(initReactI18next).init({
    resources: {
      uz: { translation: resources.uz },
      ru: { translation: resources.ru },
      en: { translation: resources.en },
    },
    lng: locale,
    fallbackLng: "uz",
    interpolation: {
      // React already escapes interpolated values.
      escapeValue: false,
    },
  });
  return instance;
}
