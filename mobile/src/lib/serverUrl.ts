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

/** Trims and drops a trailing slash so the same address always compares
 * equal — `token.ts`'s D-81 URL-binding relies on this being the only place
 * a server URL is normalised before it's persisted. */
export function normalizeServerUrl(value: string): string {
  return value.trim().replace(/\/+$/, "");
}

export async function getServerUrl(): Promise<string> {
  const stored = await SecureStore.getItemAsync(SERVER_URL_KEY);
  return stored ?? defaultServerUrl();
}

export async function setServerUrl(url: string): Promise<void> {
  await SecureStore.setItemAsync(SERVER_URL_KEY, normalizeServerUrl(url));
}

/** Loopback and private hosts (D-81) where plain `http://` is still
 * allowed; every other host must use `https://`. */
function isLoopbackOrPrivateHost(hostname: string): boolean {
  if (hostname === "localhost" || hostname === "127.0.0.1") {
    return true;
  }
  const octets = hostname.split(".");
  if (octets.length !== 4 || !octets.every((part) => /^\d+$/.test(part))) {
    return false;
  }
  const [a, b] = octets.map(Number);
  return a === 10 || (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168);
}

export type ServerUrlValidation = "valid" | "invalid" | "insecure";

/**
 * Validates the login screen's "Server" field (D-79, D-81): a well-formed
 * `http(s)://host[:port][/path]` with no query string or fragment, where
 * plain `http://` is accepted only for a loopback/private host — everything
 * else must be `https://`. Returns which rule failed so the form can show
 * the right translated message instead of a generic "invalid".
 */
export function validateServerUrl(value: string): ServerUrlValidation {
  let url: URL;
  try {
    url = new URL(normalizeServerUrl(value));
  } catch {
    return "invalid";
  }
  if (url.search || url.hash) {
    return "invalid";
  }
  if (url.protocol === "https:") {
    return "valid";
  }
  if (url.protocol === "http:") {
    return isLoopbackOrPrivateHost(url.hostname) ? "valid" : "insecure";
  }
  return "invalid";
}

export function isValidServerUrl(value: string): boolean {
  return validateServerUrl(value) === "valid";
}
