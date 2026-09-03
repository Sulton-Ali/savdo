import { createClient } from "@savdo/api-client";

import { readStoredLocale } from "./locale";

/**
 * The one typed Savdo API client for the admin app (ADR-002, AGENTS.md hard
 * rule 6). Never call `fetch` directly anywhere else in `admin/src`.
 *
 * Base URL defaults to the Vite dev proxy (`/api`, see `vite.config.ts`),
 * which strips the `/api` prefix the same way production Caddy does — the
 * browser always talks to `<base>/v1/...`.
 *
 * `credentials: "same-origin"` sends the `savdo_session` cookie (D-29,
 * ADR-005) without opting into cross-origin cookies. Every request also
 * carries `X-Requested-With: savdo` (the CSRF check the API enforces on
 * mutating requests, `docs/05-API.md` § Conventions) and `Accept-Language`
 * set from the currently selected locale, via middleware so both stay
 * correct even as the locale changes after the client is created.
 */
export const api = createClient(`${import.meta.env.VITE_API_BASE ?? "/api"}/v1`, {
  credentials: "same-origin",
});

api.use({
  onRequest({ request }) {
    request.headers.set("X-Requested-With", "savdo");
    request.headers.set("Accept-Language", readStoredLocale());
    return request;
  },
});
