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

/** Fixed mon→sun order — the API does not guarantee row order (O-19 only
 * requires "exactly 7 rows, one per distinct weekday"), so the client
 * always sorts before rendering. */
const WEEKDAY_ORDER: ReadonlyArray<ContentHoursDay["day"]> = [
  "mon",
  "tue",
  "wed",
  "thu",
  "fri",
  "sat",
  "sun",
];

/** Sorts `ContentHours.days` into a new array in fixed mon→sun order. */
export function sortHoursDays(days: readonly ContentHoursDay[]): ContentHoursDay[] {
  const byDay = new Map(days.map((day) => [day.day, day]));
  return WEEKDAY_ORDER.map((day) => byDay.get(day)).filter(
    (day): day is ContentHoursDay => day != null,
  );
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
