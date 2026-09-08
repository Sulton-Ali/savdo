import type { components } from "@savdo/api-client";
import type { TranslationKey } from "@savdo/i18n";

export type Availability = components["schemas"]["Availability"];

/** i18n key for each `Availability` value (never a raw quantity, hard rule
 * 4/5 — the public API already reduced stock to this three-state signal;
 * this module only maps it to copy and colour). */
const TRANSLATION_KEY: Record<Availability, TranslationKey> = {
  in_stock: "web.availability.inStock",
  low: "web.availability.low",
  out_of_stock: "web.availability.outOfStock",
};

/** Tailwind classes for the badge, using the `packages/ui-tokens` colours
 * mapped into Tailwind's theme in `styles.css` (success/warning/danger). */
const TONE_CLASS: Record<Availability, string> = {
  in_stock: "bg-success/10 text-success",
  low: "bg-warning/10 text-warning",
  out_of_stock: "bg-danger/10 text-danger",
};

export function availabilityTranslationKey(value: Availability): TranslationKey {
  return TRANSLATION_KEY[value];
}

export function availabilityToneClass(value: Availability): string {
  return TONE_CLASS[value];
}
