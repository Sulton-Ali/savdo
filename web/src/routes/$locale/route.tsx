import { resources } from "@savdo/i18n";
import { createFileRoute, notFound, Outlet, useParams } from "@tanstack/react-router";
import { useMemo } from "react";
import { I18nextProvider } from "react-i18next";

import { Footer } from "../../components/Footer";
import { Header } from "../../components/Header";
import { createI18nInstance } from "../../lib/i18n";
import { isLocale } from "../../lib/locale";
import { getPublicShop } from "../../lib/publicApi.functions";

/**
 * The `$locale` layout: validates the locale segment (404 for anything
 * outside uz/ru/en), fetches `PublicShop` once per request and shares it
 * with every child route via router context (deliverable 2 — one call per
 * request), and wraps the page in a per-request `I18nextProvider` (SSR-safe
 * — see `lib/i18n.ts`'s doc comment for why this is never the global
 * `i18next` singleton).
 */
export const Route = createFileRoute("/$locale")({
  beforeLoad: async ({ params }) => {
    if (!isLocale(params.locale)) {
      throw notFound();
    }
    const locale = params.locale;
    const shop = await getPublicShop({ data: { locale } });
    return { locale, shop };
  },
  component: LocaleLayout,
  notFoundComponent: LocaleNotFound,
});

function LocaleLayout() {
  const { locale, shop } = Route.useRouteContext();
  const i18n = useMemo(() => createI18nInstance(locale), [locale]);

  return (
    <I18nextProvider i18n={i18n}>
      <Header shopName={shop.name} locale={locale} />
      <Outlet />
      <Footer shopName={shop.name} />
    </I18nextProvider>
  );
}

function LocaleNotFound() {
  // The locale itself may be invalid here (that's exactly what lands a
  // visitor on this page), so there is no i18n instance to translate
  // with — read the static `uz` dictionary directly, same as the `/`
  // root route's own fallback title.
  const { locale } = useParams({ strict: false });
  const text = resources[isLocale(locale) ? locale : "uz"].web.notFound;
  return (
    <main className="mx-auto flex min-h-screen max-w-xl flex-col items-center justify-center gap-3 px-4 text-center">
      <h1 className="font-bold text-2xl text-text">{text.title}</h1>
      <p className="text-muted">{text.description}</p>
      <a href="/uz" className="text-primary underline">
        {text.homeLink}
      </a>
    </main>
  );
}
