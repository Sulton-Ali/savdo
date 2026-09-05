import * as SecureStore from "expo-secure-store";

/**
 * Remembers the last location picked for a quick sale, so a cashier who
 * always sells from the same till doesn't have to re-pick it every time.
 * Mirrors `admin/src/routes/app/quick-sale/locationStorage.ts`'s
 * `localStorage`-backed version and this app's own `lib/locale.ts`'s
 * `try`/`catch` shape — if SecureStore is ever unavailable, the choice
 * just won't survive a reload.
 */
const SALE_LOCATION_KEY = "savdo.sale.locationId";

export function readStoredLocationId(): string | null {
  try {
    return SecureStore.getItem(SALE_LOCATION_KEY);
  } catch {
    return null;
  }
}

export function persistLocationId(locationId: string): void {
  try {
    SecureStore.setItem(SALE_LOCATION_KEY, locationId);
  } catch {
    // SecureStore unavailable — the choice just won't survive a reload.
  }
}
