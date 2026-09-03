import en from "./locales/en.json";
import ru from "./locales/ru.json";
import uz from "./locales/uz.json";

export const locales = ["uz", "ru", "en"] as const;

export type Locale = (typeof locales)[number];

export const defaultLocale: Locale = "uz";

/**
 * Shape every locale dictionary must satisfy, derived from the `en` JSON file so
 * `uz` and `ru` are checked against it structurally at compile time.
 */
export type Dictionary = typeof en;

export const resources = { uz, ru, en } as const satisfies Record<Locale, Dictionary>;

/**
 * Dotted key union derived from `Dictionary`, e.g. "common.hello" | "lang.uz" | ...
 */
export type TranslationKey<T = Dictionary, Prefix extends string = ""> = {
  [K in keyof T & string]: T[K] extends string
    ? `${Prefix}${K}`
    : TranslationKey<T[K], `${Prefix}${K}.`>;
}[keyof T & string];
