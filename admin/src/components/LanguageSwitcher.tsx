import { defaultLocale, locales } from "@savdo/i18n";
import { Segmented } from "antd";
import { useTranslation } from "react-i18next";

import { isLocale } from "../lib/locale";

/**
 * Switches the i18next language (uz/ru/en, D-31). `AppConfigProvider`
 * reacts to the language change to swap the antd and dayjs locales;
 * `i18n.ts` persists the choice to `localStorage["savdo.locale"]`.
 */
export function LanguageSwitcher() {
  const { t, i18n } = useTranslation();
  const current = isLocale(i18n.language) ? i18n.language : defaultLocale;

  return (
    <Segmented
      aria-label={t("lang.switch")}
      value={current}
      onChange={(value) => {
        const next = String(value);
        if (isLocale(next)) {
          void i18n.changeLanguage(next);
        }
      }}
      options={locales.map((locale) => ({
        label: t(`lang.${locale}`),
        value: locale,
      }))}
    />
  );
}
