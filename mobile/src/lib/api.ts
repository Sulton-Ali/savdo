import type { ApiClient } from "@savdo/api-client";
import { createClient } from "@savdo/api-client";
import type { HeadersOptions } from "openapi-fetch";
import { mergeHeaders } from "openapi-fetch";

import { i18next } from "../i18n";
import { queryClient } from "./queryClient";
import { resetSessionCache } from "./queryKeys";
import { getServerUrl } from "./serverUrl";
import { clearToken, getToken, isCurrentToken, peekToken } from "./token";

const raw = createClient("");

/**
 * Every request is bounded so a dead LAN IP (D-81: a token bound to an
 * address that's since gone dark) fails fast into `useSession`'s
 * "unreachable" state instead of hanging forever. RN 0.86's `AbortController`
 * polyfill (`abort-controller`, installed by `setUpXHR.js`) has no
 * `AbortSignal.timeout` — checked its source directly — so this builds the
 * same behaviour by hand, and still honours a caller-supplied `signal`
 * (e.g. a screen unmounting its own in-flight request) by aborting the
 * combined signal the moment either one fires.
 */
const REQUEST_TIMEOUT_MS = 15_000;

function withTimeout(
  callerSignal?: AbortSignal | null,
  timeoutMs: number = REQUEST_TIMEOUT_MS,
): {
  signal: AbortSignal;
  clear: () => void;
} {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  const clear = () => clearTimeout(timer);
  controller.signal.addEventListener("abort", clear, { once: true });
  if (callerSignal) {
    if (callerSignal.aborted) {
      controller.abort();
    } else {
      callerSignal.addEventListener("abort", () => controller.abort(), { once: true });
    }
  }
  // The abort listener above only clears the timer if the request is
  // actually aborted; a request that settles normally (success or a non-abort
  // error) within the bound must not leave the timer running until it fires
  // on its own — the caller clears it once the wrapped call settles.
  return { signal: controller.signal, clear };
}

/**
 * Resolves the server URL exactly once per call and builds the per-request
 * init: `baseUrl` (openapi-fetch 0.17 reads a per-call `baseUrl` from the
 * options object — verified in `coreFetch`, `openapi-fetch/src/index.js`
 * lines ~59-62 — so the client never has to reconstruct the framework's
 * `Request`; an earlier version did that with `request.text()`, which
 * throws on `FormData`, e.g. T3's photo upload), the auth/locale headers
 * merged with openapi-fetch's own `mergeHeaders` (so a caller's own
 * `headers` override still behaves exactly like the framework's documented
 * `null`-deletes/array-appends semantics, instead of a plain `new
 * Headers()` silently dropping that), and a bounded `signal`. `getToken`
 * gets this same resolved URL (D-81's token-URL binding), so the
 * destination and the binding check can never straddle a `setServerUrl` and
 * use two different URLs.
 *
 * A caller may override the 15s default via an optional `timeoutMs` on its
 * per-call `init` (openapi-fetch has no such option itself — checked
 * `openapi-fetch` 0.17's `FetchOptions` type directly, not training data;
 * this is a Savdo-only extension read here and stripped before the rest of
 * `init` is handed to the generated client, so it never reaches the actual
 * `fetch`/`Request`). `POST /media` uploads pass a longer one: a picked
 * photo can take longer than 15s to transfer over a slow shop LAN/mobile
 * link, and failing that fast would abort uploads that were about to
 * succeed.
 */
async function withServerContext<Init extends Record<string, unknown> | undefined>(
  init: Init,
): Promise<{
  init: Omit<Init, "timeoutMs"> & { baseUrl: string; headers: Headers; signal: AbortSignal };
  clearTimer: () => void;
}> {
  const serverUrl = await getServerUrl();
  const token = await getToken(serverUrl);
  const headers = mergeHeaders(
    { "Accept-Language": i18next.language || "uz" },
    token ? { Authorization: `Bearer ${token}` } : undefined,
    init?.headers as HeadersOptions | undefined,
  );
  const { timeoutMs, ...rest } = (init ?? {}) as Record<string, unknown> & { timeoutMs?: number };
  const { signal, clear } = withTimeout(init?.signal as AbortSignal | null | undefined, timeoutMs);
  return {
    init: { ...rest, baseUrl: serverUrl, headers, signal } as Omit<Init, "timeoutMs"> & {
      baseUrl: string;
      headers: Headers;
      signal: AbortSignal;
    },
    clearTimer: clear,
  };
}

// A verb method's shape, loosely: `(url, init?) => Promise<result>`.
type VerbMethod = (url: string, init?: Record<string, unknown>) => Promise<unknown>;
type VerbName = "GET" | "PUT" | "POST" | "DELETE" | "OPTIONS" | "HEAD" | "PATCH" | "TRACE";
type Verbs = Record<VerbName, VerbMethod>;

const VERB_NAMES: VerbName[] = [
  "GET",
  "PUT",
  "POST",
  "DELETE",
  "OPTIONS",
  "HEAD",
  "PATCH",
  "TRACE",
];

function withDynamicBaseUrl(method: VerbMethod): VerbMethod {
  return async (url, init) => {
    const { init: fullInit, clearTimer } = await withServerContext(init);
    try {
      return await method(url, fullInit);
    } finally {
      clearTimer();
    }
  };
}

// openapi-fetch's verb methods are generic, overloaded and keyed to the exact
// OpenAPI path/method — a runtime wrapper can't preserve that type, so this
// is the one place (not once per verb, and again for the assembled object)
// the client is erased down to the loose `VerbMethod` shape and back:
// erasing `raw`'s methods once here to build the wrapped verbs, then
// re-asserting the exact generated `ApiClient` type once on the exported
// `api` below, which is what every call site (`lib/authApi.ts`, future media
// uploads, …) actually imports and type-checks against (ADR-002).
const rawVerbs = raw as unknown as Verbs;
const dynamicVerbs = Object.fromEntries(
  VERB_NAMES.map((verb) => [verb, withDynamicBaseUrl(rawVerbs[verb])]),
) as Verbs;

/**
 * The one typed Savdo API client for the mobile app (ADR-002, AGENTS.md hard
 * rule 6). Never call `fetch` directly anywhere else in `mobile/src`.
 *
 * Unlike `admin`, the base URL isn't fixed at client-creation time (D-79's
 * editable "Server" field takes effect on the very next request): each verb
 * method is wrapped by `withDynamicBaseUrl` to resolve it per call instead.
 * `request` (openapi-fetch's untyped, method-as-argument escape hatch) is
 * deliberately excluded from this exported type rather than wrapped: nothing
 * in the app calls it, and leaving it reachable would bypass `baseUrl`/auth
 * entirely, unwrapped straight from `raw`.
 *
 * The `onResponse` middleware below clears the session on a `401` for the
 * same token the failing request itself carried (a slow 401 from an
 * already-replaced session must not log out a session that has since logged
 * in again, hence the header comparison — using `peekToken`'s pure read, not
 * `getToken`'s URL-binding check, so this comparison can never itself delete
 * the token as a side effect while the query cache still says
 * authenticated) — every `401` this API returns means `UNAUTHENTICATED`
 * (`contracts/openapi.yaml`'s `Unauthenticated` response is the only thing
 * mapped to status 401), so this doesn't need to parse the body at all, and
 * a non-JSON error page from a misconfigured proxy in front of the API still
 * clears the token rather than being retried forever as `errorCodeFrom`'s
 * `INTERNAL` fallback in `lib/authApi.ts`. It never navigates: the
 * `app/_layout.tsx` root layout reacts to the cleared token on its own.
 */
export const api: Omit<ApiClient, "request"> = {
  ...dynamicVerbs,
  use: raw.use,
  eject: raw.eject,
} as Omit<ApiClient, "request">;

raw.use({
  async onResponse({ request, response }) {
    if (response.status !== 401) {
      return;
    }

    // Only clear the session if this request carried the token that's still
    // current. A response for an old/replaced token (in flight when the
    // session changed) must not clear a freshly issued one.
    const requestAuth = request.headers.get("Authorization");
    const currentToken = await peekToken();
    if (!isCurrentToken(requestAuth, currentToken)) {
      return;
    }

    await clearToken();
    // `resetSessionCache` notifies the still-mounted `useSession` observers
    // directly and drops every other cached query, so no stale query
    // (another user's `me`, cached lists, …) survives the session boundary
    // (ADR-010) without detaching those observers the way a `clear()` would.
    resetSessionCache(queryClient, false);
  },
});
