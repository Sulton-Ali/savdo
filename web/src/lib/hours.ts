import type { components } from "@savdo/api-client";
import type { TranslationKey } from "@savdo/i18n";

export type ContentHoursDay = components["schemas"]["ContentHoursDay"];

/** i18n key for each weekday — day rows are locale-independent (D-107): the
 * seven weekday labels come from `@savdo/i18n`, never from the stored
 * content block. */
const WEEKDAY_KEY: Record<ContentHoursDay["day"], TranslationKey> = {
  mon: "web.hours.mon",
  tue: "web.hours.tue",
  wed: "web.hours.wed",
  thu: "web.hours.thu",
  fri: "web.hours.fri",
  sat: "web.hours.sat",
  sun: "web.hours.sun",
};

export function weekdayTranslationKey(day: ContentHoursDay["day"]): TranslationKey {
  return WEEKDAY_KEY[day];
}

/**
 * Formats one `ContentHoursDay` row's open/close range (`"09:00–19:00"`),
 * or `null` when the day is closed (or missing a time) — the caller renders
 * the `web.hours.closed` label in that case instead.
 */
export function formatHoursRange(day: ContentHoursDay): string | null {
  if (day.closed || !day.open || !day.close) {
    return null;
  }
  return `${day.open}–${day.close}`;
}
