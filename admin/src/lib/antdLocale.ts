import type { Locale as SavdoLocale } from "@savdo/i18n";
import type { Locale as AntdLocale } from "antd/es/locale/index";
import enUS from "antd/locale/en_US";
import ruRU from "antd/locale/ru_RU";
import uzUZ from "antd/locale/uz_UZ";

// Side-effect imports: dayjs needs each locale registered before
// `dayjs.locale(id)` can switch to it (Vite does not tree-shake these away).
import "dayjs/locale/en";
import "dayjs/locale/ru";
import "dayjs/locale/uz-latn";

/** Ant Design's built-in locale pack per Savdo locale (D-31). */
export const antdLocales: Record<SavdoLocale, AntdLocale> = {
  uz: uzUZ,
  ru: ruRU,
  en: enUS,
};

/**
 * dayjs locale id per Savdo locale. Ant Design's own `uz_UZ` pack is
 * `uz-latn` internally (Uzbek Latin, verified in
 * `antd/lib/locale/uz_UZ.js`) — dayjs ships the matching `uz-latn` locale
 * file, distinct from its Cyrillic `uz`.
 */
export const dayjsLocaleIds: Record<SavdoLocale, string> = {
  uz: "uz-latn",
  ru: "ru",
  en: "en",
};
