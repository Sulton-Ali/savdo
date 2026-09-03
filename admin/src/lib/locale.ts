import { defaultLocale, type Locale, locales } from "@savdo/i18n";

/**
 * Shared locale-persistence helpers used by both `i18n.ts` (to pick the
 * initial i18next language) and `lib/api.ts` (to send `Accept-Language` on
 * every request). Kept dependency-free of `i18n.ts` itself so the two never
 * import each other.
 */
export const LOCALE_STORAGE_KEY = "savdo.locale";

export function isLocale(value: string | null | undefined): value is Locale {
  return value != null && (locales as readonly string[]).includes(value);
}

/** Reads the persisted locale, falling back to `defaultLocale` (uz) when
 * unset or when `localStorage` is unavailable (e.g. private browsing). */
export function readStoredLocale(): Locale {
  try {
    const stored = localStorage.getItem(LOCALE_STORAGE_KEY);
    return isLocale(stored) ? stored : defaultLocale;
  } catch {
    return defaultLocale;
  }
}

export function persistLocale(locale: Locale): void {
  try {
    localStorage.setItem(LOCALE_STORAGE_KEY, locale);
  } catch {
    // localStorage unavailable — the choice just won't survive a reload.
  }
}
