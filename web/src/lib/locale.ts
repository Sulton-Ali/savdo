import { defaultLocale, type Locale, locales } from "@savdo/i18n";

export type { Locale };
export { defaultLocale, locales };

/** Narrows an arbitrary route param / string to `Locale` (D-100: the URL
 * carries the locale as a path prefix — `/uz`, `/ru`, `/en` — and anything
 * else is a 404, never a silent fallback). */
export function isLocale(value: string | null | undefined): value is Locale {
  return value != null && (locales as readonly string[]).includes(value);
}

/**
 * Swaps the leading `/uz|/ru|/en` segment of `pathname` for `target`,
 * keeping the rest of the path untouched — the language switcher "keeps the
 * page" (D-100). `pathname` is expected to already start with a locale
 * segment (every route lives under the `$locale` layout); if it does not,
 * `target`'s root page is returned as a safe fallback.
 */
export function switchLocalePath(pathname: string, target: Locale): string {
  const segments = pathname.split("/");
  if (segments.length > 1 && isLocale(segments[1])) {
    segments[1] = target;
    const joined = segments.join("/");
    return joined === "" ? "/" : joined;
  }
  return `/${target}`;
}
