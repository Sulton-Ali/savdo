import { defaultLocale, type Locale, locales } from "@savdo/i18n";
import * as SecureStore from "expo-secure-store";

/**
 * Shared locale-persistence helpers used by `i18n.ts` (to pick the initial
 * i18next language) and `lib/api.ts` (to send `Accept-Language` on every
 * request) — mirrors `admin/src/lib/locale.ts`. Uses SecureStore's
 * synchronous API (not `getItemAsync`) because `i18n.ts` needs a value
 * before the first render, the same reason `admin` reads `localStorage`
 * synchronously.
 */
const LOCALE_KEY = "savdo.locale";

export function isLocale(value: string | null | undefined): value is Locale {
  return value != null && (locales as readonly string[]).includes(value);
}

/** Reads the persisted locale, falling back to `defaultLocale` (uz) when
 * unset or when SecureStore is unavailable. */
export function readStoredLocale(): Locale {
  try {
    const stored = SecureStore.getItem(LOCALE_KEY);
    return isLocale(stored) ? stored : defaultLocale;
  } catch {
    return defaultLocale;
  }
}

export function persistLocale(locale: Locale): void {
  try {
    SecureStore.setItem(LOCALE_KEY, locale);
  } catch {
    // SecureStore unavailable — the choice just won't survive a reload.
  }
}
