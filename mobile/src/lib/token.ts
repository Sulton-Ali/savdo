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

export async function getToken(): Promise<string | null> {
  const [token, issuedFor, currentUrl] = await Promise.all([
    SecureStore.getItemAsync(TOKEN_KEY),
    SecureStore.getItemAsync(TOKEN_URL_KEY),
    getServerUrl(),
  ]);
  if (!token) {
    return null;
  }
  if (issuedFor !== currentUrl) {
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
