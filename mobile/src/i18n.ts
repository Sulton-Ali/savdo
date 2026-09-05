import { defaultLocale, resources } from "@savdo/i18n";
import i18next from "i18next";
import { initReactI18next } from "react-i18next";

import { isLocale, persistLocale, readStoredLocale } from "./lib/locale";

/**
 * i18next instance for the mobile app (ADR-012, D-78). `@savdo/i18n` is the
 * single UI dictionary shared with `web/` and `admin/`; this file only
 * wires it into react-i18next and picks the initial language (mirrors
 * `admin/src/i18n.ts`).
 */
void i18next.use(initReactI18next).init({
  resources: {
    uz: { translation: resources.uz },
    ru: { translation: resources.ru },
    en: { translation: resources.en },
  },
  lng: readStoredLocale(),
  fallbackLng: defaultLocale,
  interpolation: {
    // React Native already escapes interpolated values.
    escapeValue: false,
  },
});

// Persist every language change so a reload keeps the chosen locale.
i18next.on("languageChanged", (lng) => {
  if (isLocale(lng)) {
    persistLocale(lng);
  }
});

export { i18next };
