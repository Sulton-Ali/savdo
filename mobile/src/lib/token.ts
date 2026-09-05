import * as SecureStore from "expo-secure-store";

/**
 * The mobile bearer token (D-29, ADR-005), sent as `Authorization: Bearer
 * <token>` by `lib/api.ts`. Stored only in SecureStore — never AsyncStorage,
 * logs, or React state that survives a reload (AGENTS.md hard rule 9).
 */
const TOKEN_KEY = "savdo.token";

export async function getToken(): Promise<string | null> {
  return SecureStore.getItemAsync(TOKEN_KEY);
}

export async function setToken(token: string): Promise<void> {
  await SecureStore.setItemAsync(TOKEN_KEY, token);
}

export async function clearToken(): Promise<void> {
  await SecureStore.deleteItemAsync(TOKEN_KEY);
}
