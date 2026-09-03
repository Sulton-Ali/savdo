import { defaultLocale } from "@savdo/i18n";
import { tokens } from "@savdo/ui-tokens";
import { ConfigProvider } from "antd";
import dayjs from "dayjs";
import { type ReactNode, useEffect } from "react";
import { useTranslation } from "react-i18next";

import { antdLocales, dayjsLocaleIds } from "../lib/antdLocale";
import { isLocale } from "../lib/locale";

/**
 * Wraps Ant Design's `ConfigProvider` with the theme from `@savdo/ui-tokens`
 * and keeps its `locale` (and dayjs's global locale) in sync with the
 * current i18next language, so date pickers, pagination text etc. follow
 * the `LanguageSwitcher` (D-25).
 */
export function AppConfigProvider({ children }: { children: ReactNode }) {
  const { i18n } = useTranslation();
  const locale = isLocale(i18n.language) ? i18n.language : defaultLocale;

  useEffect(() => {
    dayjs.locale(dayjsLocaleIds[locale]);
  }, [locale]);

  return (
    <ConfigProvider
      locale={antdLocales[locale]}
      theme={{
        token: {
          colorPrimary: tokens.color.primary,
          borderRadius: tokens.radius.md,
          fontFamily: tokens.font.family,
        },
      }}
    >
      {children}
    </ConfigProvider>
  );
}
