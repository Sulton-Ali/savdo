import { createFileRoute, redirect } from "@tanstack/react-router";

import { defaultLocale } from "../lib/locale";

/** `/` redirects to the default locale's home page (D-100 — uz is the
 * default and canonical locale). A real HTTP 302 (the default would be
 * 307): a temporary redirect the crawler should not cache as canonical. */
export const Route = createFileRoute("/")({
  beforeLoad: () => {
    throw redirect({ to: "/$locale", params: { locale: defaultLocale }, statusCode: 302 });
  },
});
