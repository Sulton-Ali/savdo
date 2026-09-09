import { resources } from "@savdo/i18n";
import { createRootRoute, HeadContent, Scripts, useParams } from "@tanstack/react-router";

import { defaultLocale, isLocale } from "../lib/locale";
import appCss from "../styles.css?url";

export const Route = createRootRoute({
  head: () => ({
    meta: [
      { charSet: "utf-8" },
      { name: "viewport", content: "width=device-width, initial-scale=1" },
      { title: resources.uz.app.name },
      {
        name: "description",
        content: "Savdo — light ERP and CRM for small shops in Uzbekistan.",
      },
    ],
    links: [
      { rel: "stylesheet", href: appCss },
      { rel: "icon", href: "/favicon.svg", type: "image/svg+xml" },
    ],
  }),
  shellComponent: RootDocument,
});

function RootDocument({ children }: { children: React.ReactNode }) {
  // The `$locale` layout route validates this param before anything under
  // it renders; reading it loosely here (`strict: false`) just picks the
  // `<html lang>` — it never runs before the layout's own 404 for an
  // invalid locale.
  const { locale } = useParams({ strict: false });
  return (
    <html lang={isLocale(locale) ? locale : defaultLocale}>
      <head>
        <HeadContent />
      </head>
      {/* phase-7.5 T3: the page background lives here, not on a per-route
       * `<main>` — `bg-bg` (#f5f5f4) on `<body>` reaches the full-width
       * viewport (the routed content's own `max-w-*` column sits transparent
       * on top of it), so every public route shares one page background,
       * edge to edge, instead of each route opting in on its own `<main>`. */}
      <body className="bg-bg">
        {children}
        <Scripts />
      </body>
    </html>
  );
}
