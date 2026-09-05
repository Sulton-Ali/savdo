import * as SecureStore from "expo-secure-store";

/**
 * The API server address (D-79): editable on the login screen, prefilled
 * from `EXPO_PUBLIC_API_URL` (baked in at build time) and remembered on the
 * device via SecureStore so the owner's LAN IP can change without a
 * rebuild. `10.0.2.2` is the Android-emulator alias for the host machine's
 * `localhost`; a real device on the same network needs the host's LAN IP.
 */
const SERVER_URL_KEY = "savdo.serverUrl";

export function defaultServerUrl(): string {
  return process.env.EXPO_PUBLIC_API_URL ?? "http://10.0.2.2:8080/v1";
}

export async function getServerUrl(): Promise<string> {
  const stored = await SecureStore.getItemAsync(SERVER_URL_KEY);
  return stored ?? defaultServerUrl();
}

export async function setServerUrl(url: string): Promise<void> {
  await SecureStore.setItemAsync(SERVER_URL_KEY, url);
}

/** Accepts only well-formed `http(s)://...` addresses — the login form's
 * inline validation rule for the "Server" field. */
export function isValidServerUrl(value: string): boolean {
  try {
    const url = new URL(value);
    return url.protocol === "http:" || url.protocol === "https:";
  } catch {
    return false;
  }
}
