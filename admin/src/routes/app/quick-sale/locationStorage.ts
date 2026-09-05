/** Remembers the last location picked for a quick sale, so a cashier who
 * always sells from the same till doesn't have to re-pick it every time.
 * Same `try`/`catch` shape as `lib/locale.ts`'s persisted locale — a
 * private-browsing session where `localStorage` throws just means the
 * choice won't survive a reload. */
const QUICK_SALE_LOCATION_KEY = "savdo.quickSale.locationId";

export function readStoredLocationId(): string | null {
  try {
    return localStorage.getItem(QUICK_SALE_LOCATION_KEY);
  } catch {
    return null;
  }
}

export function persistLocationId(locationId: string): void {
  try {
    localStorage.setItem(QUICK_SALE_LOCATION_KEY, locationId);
  } catch {
    // localStorage unavailable — the choice just won't survive a reload.
  }
}
