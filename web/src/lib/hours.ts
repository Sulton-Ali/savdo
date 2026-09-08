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

/** i18n key for each weekday's short form (`web.weekdayShort.*`) — used by
 * `summarizeHours`'s compact footer line, e.g. "Du–Ju". */
const WEEKDAY_SHORT_KEY: Record<ContentHoursDay["day"], TranslationKey> = {
  mon: "web.weekdayShort.mon",
  tue: "web.weekdayShort.tue",
  wed: "web.weekdayShort.wed",
  thu: "web.weekdayShort.thu",
  fri: "web.weekdayShort.fri",
  sat: "web.weekdayShort.sat",
  sun: "web.weekdayShort.sun",
};

export function weekdayShortTranslationKey(day: ContentHoursDay["day"]): TranslationKey {
  return WEEKDAY_SHORT_KEY[day];
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

/**
 * Collapses `days` (any order, per `ContentHours.days`) into one compact
 * line for the footer — consecutive days (in fixed mon→sun order) that
 * share the same open/close range, or are both closed, become one range
 * segment (`"Du–Ju 09:00–19:00"`); a day that differs from its neighbours
 * stays its own segment (`"Sh 10:00–18:00"`, `"Ya dam olish"`). Segments
 * are joined with " · ". `shortDayLabel`/`closedLabel` are supplied by the
 * caller — this module stays i18n-free, same separation as
 * `formatHoursRange`.
 */
export function summarizeHours(
  days: readonly ContentHoursDay[],
  shortDayLabel: (day: ContentHoursDay["day"]) => string,
  closedLabel: string,
): string {
  type Segment = { firstShort: string; lastShort: string; text: string };
  const segments: Segment[] = [];
  for (const day of sortHoursDays(days)) {
    const text = formatHoursRange(day) ?? closedLabel;
    const short = shortDayLabel(day.day);
    const last = segments.at(-1);
    if (last != null && last.text === text) {
      last.lastShort = short;
    } else {
      segments.push({ firstShort: short, lastShort: short, text });
    }
  }
  return segments
    .map((segment) =>
      segment.firstShort === segment.lastShort
        ? `${segment.firstShort} ${segment.text}`
        : `${segment.firstShort}–${segment.lastShort} ${segment.text}`,
    )
    .join(" · ");
}
