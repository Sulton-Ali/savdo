import type { ApiClient } from "@savdo/api-client";
import { createClient } from "@savdo/api-client";

import { i18next } from "../i18n";
import { queryClient } from "./queryClient";
import { TOKEN_QUERY_KEY } from "./queryKeys";
import { getServerUrl } from "./serverUrl";
import { clearToken, getToken } from "./token";

const raw = createClient("");

/**
 * Resolves the server URL exactly once per call and builds the per-request
 * init: `baseUrl` (openapi-fetch 0.17 reads a per-call `baseUrl` from the
 * options object — verified in `coreFetch`, `openapi-fetch/src/index.js`
 * lines ~59-62 — so the client never has to reconstruct the framework's
 * `Request`; an earlier version did that with `request.text()`, which
 * throws on `FormData`, e.g. T3's photo upload) and the auth/locale
 * headers. `getToken` gets this same resolved URL (D-81's token-URL
 * binding), so the destination and the binding check can never straddle a
 * `setServerUrl` and use two different URLs.
 */
async function withServerContext<Init extends Record<string, unknown> | undefined>(
  init: Init,
): Promise<Init & { baseUrl: string; headers: Headers }> {
  const serverUrl = await getServerUrl();
  const token = await getToken(serverUrl);
  const headers = new Headers(init?.headers as HeadersInit | undefined);
  headers.set("Accept-Language", i18next.language || "uz");
  if (token) {
    headers.set("Authorization", `Bearer ${token}`);
  }
  return { ...init, baseUrl: serverUrl, headers } as Init & { baseUrl: string; headers: Headers };
}

// A verb method's shape, loosely: `(url, init?) => Promise<result>`.
// openapi-fetch's own generics are keyed to the exact OpenAPI path/method
// and can't be preserved through a runtime wrapper, so this is the one place
// that steps outside them — `api`'s exported type below is still the exact
// generated `ApiClient`, so every call site (`lib/authApi.ts`, future media
// uploads, …) keeps full contract-derived type-checking (ADR-002).
type VerbMethod = (url: string, init?: Record<string, unknown>) => Promise<unknown>;

function withDynamicBaseUrl(method: VerbMethod): VerbMethod {
  return async (url, init) => method(url, await withServerContext(init));
}

/**
 * The one typed Savdo API client for the mobile app (ADR-002, AGENTS.md hard
 * rule 6). Never call `fetch` directly anywhere else in `mobile/src`.
 *
 * Unlike `admin`, the base URL isn't fixed at client-creation time (D-79's
 * editable "Server" field takes effect on the very next request): each verb
 * method is wrapped by `withDynamicBaseUrl` to resolve it per call instead.
 *
 * The `onResponse` middleware below clears the session on a `401` for the
 * same token the failing request itself carried (a slow 401 from an
 * already-replaced session must not log out a session that has since logged
 * in again, hence the header comparison) — every `401` this API returns
 * means `UNAUTHENTICATED` (`contracts/openapi.yaml`'s `Unauthenticated`
 * response is the only thing mapped to status 401), so this doesn't need to
 * parse the body at all, and a non-JSON error page from a misconfigured
 * proxy in front of the API still clears the token rather than being
 * retried forever as `errorCodeFrom`'s `INTERNAL` fallback in
 * `lib/authApi.ts`. It never navigates: the `app/_layout.tsx` root layout
 * reacts to the cleared token on its own.
 */
export const api: ApiClient = {
  ...raw,
  GET: withDynamicBaseUrl(raw.GET as VerbMethod),
  PUT: withDynamicBaseUrl(raw.PUT as VerbMethod),
  POST: withDynamicBaseUrl(raw.POST as VerbMethod),
  DELETE: withDynamicBaseUrl(raw.DELETE as VerbMethod),
  OPTIONS: withDynamicBaseUrl(raw.OPTIONS as VerbMethod),
  HEAD: withDynamicBaseUrl(raw.HEAD as VerbMethod),
  PATCH: withDynamicBaseUrl(raw.PATCH as VerbMethod),
  TRACE: withDynamicBaseUrl(raw.TRACE as VerbMethod),
} as ApiClient;

raw.use({
  async onResponse({ request, response }) {
    if (response.status !== 401) {
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
