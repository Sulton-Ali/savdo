import * as SecureStore from "expo-secure-store";

/**
 * Remembers the last location picked on the Stock tab across app restarts
 * (deliverable 3: "location selector remembered") — not sensitive data, but
 * `expo-secure-store` is the only on-device persistence already in this
 * app's dependency list (`lib/token.ts`, `lib/serverUrl.ts`; D-78 approves
 * no `AsyncStorage`), so it's reused here rather than adding one. A missing
 * or unreadable value (first launch, or a location since deleted) is
 * treated as "no preference" — the caller (`useLocations` in
 * `features/catalog/hooks.ts` combined with this) falls back to the first
 * active location.
 */
const PREFERRED_LOCATION_KEY = "savdo.stock.preferredLocationId";

export async function getPreferredLocationId(): Promise<string | null> {
  return SecureStore.getItemAsync(PREFERRED_LOCATION_KEY);
}

export async function setPreferredLocationId(locationId: string): Promise<void> {
  await SecureStore.setItemAsync(PREFERRED_LOCATION_KEY, locationId);
}
