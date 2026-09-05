import { createClient } from "@savdo/api-client";

import { i18next } from "../i18n";
import { queryClient } from "./queryClient";
import { TOKEN_QUERY_KEY } from "./queryKeys";
import { getServerUrl } from "./serverUrl";
import { clearToken, getToken } from "./token";

/**
 * The one typed Savdo API client for the mobile app (ADR-002, AGENTS.md hard
 * rule 6). Never call `fetch` directly anywhere else in `mobile/src`.
 *
 * Unlike `admin`, the base URL isn't fixed at client-creation time: it's
 * resolved from `serverUrl.ts` on every request via the `onRequest`
 * middleware below, so editing the "Server" field on the login screen (D-79)
 * takes effect on the very next request without rebuilding the client. The
 * placeholder base is never actually reached over the network — every
 * request is rewritten before it leaves the device.
 *
 * The middleware also attaches `Authorization: Bearer <token>` from
 * SecureStore (no `X-Requested-With` — that CSRF header only applies to the
 * cookie-authenticated `web` client, `api/internal/httpx/auth_test.go`
 * `TestBearerLogoutNeedsNoCSRFHeader`) and `Accept-Language` from the
 * current i18next language.
 *
 * On a `401 UNAUTHENTICATED` for the same token the failing request itself
 * carried, it clears the session (token + whole query cache, ADR-010) — a
 * slow 401 from an already-replaced session must not log out a session that
 * has since logged in again, hence the header comparison. It never
 * navigates: the declarative `Stack.Protected` gate in `app/_layout.tsx`
 * reacts to the cleared token on its own.
 */
export const api = createClient("http://savdo.invalid");

api.use({
  async onRequest({ request }) {
    const [serverUrl, token] = await Promise.all([getServerUrl(), getToken()]);
    const { pathname, search } = new URL(request.url);

    const headers = new Headers(request.headers);
    headers.set("Accept-Language", i18next.language || "uz");
    if (token) {
      headers.set("Authorization", `Bearer ${token}`);
    }

    const hasBody = request.method !== "GET" && request.method !== "HEAD";
    return new Request(`${serverUrl.replace(/\/+$/, "")}${pathname}${search}`, {
      method: request.method,
      headers,
      body: hasBody ? await request.text() : undefined,
      signal: request.signal,
      redirect: request.redirect,
    });
  },
  // Never returns a `Response` here (openapi-fetch throws "onResponse: must
  // return new Response() when modifying the response" for any truthy,
  // non-`Response` return value, and returning the same untouched instance
  // isn't reliably recognised as `instanceof Response` in Hermes) — this
  // middleware only reads the body to decide whether to clear the session,
  // it never rewrites the response.
  async onResponse({ request, response }) {
    if (response.status !== 401) {
      return;
    }
    let code: string | undefined;
    try {
      const body: unknown = await response.clone().json();
      code =
        body && typeof body === "object" && "error" in body
          ? (body as { error?: { code?: string } }).error?.code
          : undefined;
    } catch {
      // Non-JSON body (e.g. a proxy error page) — nothing to key off.
    }
    if (code !== "UNAUTHENTICATED") {
      return;
    }

    // Only clear the session if this request carried the token that's still
    // current. A response for an old/replaced token (in flight when the
    // session changed) must not clear a freshly issued one.
    const requestAuth = request.headers.get("Authorization");
    const currentToken = await getToken();
    if (!requestAuth || !currentToken || requestAuth !== `Bearer ${currentToken}`) {
      return;
    }

    await clearToken();
    // Clear the whole cache before re-seeding the token flag, so the fresh
    // `false` isn't wiped out again by `clear()` and no stale query (another
    // user's `me`, cached lists, …) survives the session boundary (ADR-010).
    queryClient.clear();
    queryClient.setQueryData(TOKEN_QUERY_KEY, false);
  },
});
