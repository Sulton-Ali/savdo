import { defaultLocale } from "@savdo/i18n";
import { tokens } from "@savdo/ui-tokens";
import { ConfigProvider } from "antd";
import dayjs from "dayjs";
import { type ReactNode, useEffect } from "react";
import { useTranslation } from "react-i18next";

import { antdLocales, dayjsLocaleIds } from "../lib/antdLocale";
import { isLocale } from "../lib/locale";
import { queryClient } from "../lib/queryClient";

/**
 * Wraps Ant Design's `ConfigProvider` with the theme from `@savdo/ui-tokens`
 * and keeps its `locale` (and dayjs's global locale) in sync with the
 * current i18next language, so date pickers, pagination text etc. follow
 * the `LanguageSwitcher` (D-25).
 *
 * Also invalidates every cached query on an actual language change (D-39):
 * category/product names, descriptions etc. come back from the API already
 * resolved to the `Accept-Language` sent with the request
 * (`lib/api.ts`), so a stale TanStack Query cache entry would keep showing
 * the old language until something else happened to refetch it. Listening
 * to i18next's `languageChanged` event (rather than an effect keyed on the
 * derived `locale`) means the initial language picked at startup — not a
 * user-driven switch — never triggers a refetch.
 */
export function AppConfigProvider({ children }: { children: ReactNode }) {
  const { i18n } = useTranslation();
  const locale = isLocale(i18n.language) ? i18n.language : defaultLocale;

  useEffect(() => {
    dayjs.locale(dayjsLocaleIds[locale]);
  }, [locale]);

  useEffect(() => {
    function handleLanguageChanged() {
      void queryClient.invalidateQueries();
    }
    i18n.on("languageChanged", handleLanguageChanged);
    return () => {
      i18n.off("languageChanged", handleLanguageChanged);
    };
  }, [i18n]);

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
