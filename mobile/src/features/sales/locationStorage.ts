import * as SecureStore from "expo-secure-store";

import { getServerUrl } from "@/lib/serverUrl";

/**
 * Remembers the last location picked for a quick sale, so a cashier who
 * always sells from the same till doesn't have to re-pick it every time.
 * Mirrors `admin/src/routes/app/quick-sale/locationStorage.ts`'s
 * `localStorage`-backed version and this app's own `lib/locale.ts`'s
 * `try`/`catch` shape — if SecureStore is ever unavailable, the choice
 * just won't survive a reload.
 *
 * Namespaced by the current server's origin (`lib/serverUrl.ts`'s D-79
 * editable "Server" field, same URL-binding idea as `lib/token.ts`'s D-81)
 * — a location id is a UUID scoped to one shop's database, so a value
 * remembered while pointed at one server must never be silently reused
 * against a different one (a stale id could coincidentally match an
 * unrelated location, or a validly-shaped-but-wrong-shop one). Async
 * (unlike `lib/locale.ts`'s sync read) because resolving the origin needs
 * `getServerUrl()`'s own SecureStore read first — nothing in `sale/index.tsx`
 * needs this before first paint the way `i18n.ts` needs the locale.
 */
const SALE_LOCATION_KEY_PREFIX = "savdo.sale.locationId.";

async function storageKey(): Promise<string> {
  const serverUrl = await getServerUrl();
  try {
    return SALE_LOCATION_KEY_PREFIX + new URL(serverUrl).origin;
  } catch {
    // A malformed stored URL (shouldn't happen, `serverUrl.ts` validates on
    // save) still gets its own bucket rather than crashing.
    return SALE_LOCATION_KEY_PREFIX + serverUrl;
  }
}

export async function readStoredLocationId(): Promise<string | null> {
  try {
    return await SecureStore.getItemAsync(await storageKey());
  } catch {
    return null;
  }
}

export async function persistLocationId(locationId: string): Promise<void> {
  try {
    await SecureStore.setItemAsync(await storageKey(), locationId);
  } catch {
    // SecureStore unavailable — the choice just won't survive a reload.
  }
}
