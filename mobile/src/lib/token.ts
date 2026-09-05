import * as SecureStore from "expo-secure-store";

import { getServerUrl } from "./serverUrl";

/**
 * The mobile bearer token (D-29, ADR-005), sent as `Authorization: Bearer
 * <token>` by `lib/api.ts`. Stored only in SecureStore — never AsyncStorage,
 * logs, or React state that survives a reload (AGENTS.md hard rule 9).
 *
 * D-81: the token is bound to the server URL it was issued by. `getToken`
 * treats a token issued for a different URL as absent (and clears it) so no
 * call site has to remember to clear the token when the server address
 * changes — e.g. the login screen just persists the new URL and logs in;
 * it never has to touch the token directly.
 */
const TOKEN_KEY = "savdo.token";
const TOKEN_URL_KEY = "savdo.token.url";

async function readStoredToken(): Promise<{ token: string | null; issuedFor: string | null }> {
  const [token, issuedFor] = await Promise.all([
    SecureStore.getItemAsync(TOKEN_KEY),
    SecureStore.getItemAsync(TOKEN_URL_KEY),
  ]);
  return { token, issuedFor };
}

/**
 * Pure read of the raw stored token — whatever URL it was issued for, with
 * no side effect. For predicates that only need to *compare* a value (the
 * `lib/api.ts` 401 handler's Authorization check): that comparison must
 * never itself decide "this token doesn't match the current URL, delete
 * it" while some other part of the app (the query cache) still says
 * authenticated — that decision belongs solely to `getToken` below, called
 * from the one place that's about to actually use the token for a request.
 */
export async function peekToken(): Promise<string | null> {
  const { token } = await readStoredToken();
  return token;
}

/**
 * The D-81 URL-binding check: returns the token only if it was issued for
 * `currentUrl` (or, if omitted, whatever `getServerUrl()` currently
 * resolves to), clearing it as a side effect on a mismatch.
 *
 * @param currentUrl The server URL to check the token against. Callers that
 * already resolved it for the same request (`lib/api.ts`) must pass that
 * exact value, so the URL-binding check and the request's destination can
 * never straddle a `setServerUrl` and use two different URLs. Callers with
 * no request of their own (`lib/session.ts`'s token-presence check) can
 * omit it and let this function resolve its own.
 */
export async function getToken(currentUrl?: string): Promise<string | null> {
  const [{ token, issuedFor }, resolvedUrl] = await Promise.all([
    readStoredToken(),
    currentUrl ? Promise.resolve(currentUrl) : getServerUrl(),
  ]);
  if (!token) {
    return null;
  }
  if (issuedFor !== resolvedUrl) {
    await clearToken();
    return null;
  }
  return token;
}

export async function setToken(token: string): Promise<void> {
  const url = await getServerUrl();
  await Promise.all([
    SecureStore.setItemAsync(TOKEN_KEY, token),
    SecureStore.setItemAsync(TOKEN_URL_KEY, url),
  ]);
}

export async function clearToken(): Promise<void> {
  await Promise.all([
    SecureStore.deleteItemAsync(TOKEN_KEY),
    SecureStore.deleteItemAsync(TOKEN_URL_KEY),
  ]);
}

/** Pure comparison for the `lib/api.ts` 401 handler: does this
 * `Authorization` header value match the currently stored token? Split out
 * so it's unit-testable without SecureStore. */
export function isCurrentToken(
  authorizationHeader: string | null | undefined,
  storedToken: string | null | undefined,
): boolean {
  return !!authorizationHeader && !!storedToken && authorizationHeader === `Bearer ${storedToken}`;
}
