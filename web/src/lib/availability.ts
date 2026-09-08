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

/**
 * Badge classes for each value — `.badge-success`/`.badge-warning`/
 * `.badge-danger` (`styles.css`), not the raw `bg-success/10 text-success`
 * `packages/ui-tokens` pairing: at badge text size (`text-xs`) that pairing
 * measures ~2.9:1 contrast, short of WCAG AA's 4.5:1 for normal text. The
 * badge classes use calibrated light-background/dark-text pairs instead
 * (deliverable 4 — "badges with accessible colours + text"), scoped to
 * `web/` only; the shared tokens (buttons, links, icons elsewhere) are
 * unchanged.
 */
const TONE_CLASS: Record<Availability, string> = {
  in_stock: "badge-success",
  low: "badge-warning",
  out_of_stock: "badge-danger",
};

export function availabilityTranslationKey(value: Availability): TranslationKey {
  return TRANSLATION_KEY[value];
}

export function availabilityToneClass(value: Availability): string {
  return TONE_CLASS[value];
}
