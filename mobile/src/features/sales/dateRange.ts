/**
 * Pure date-range helpers for the "Sales list" tab (T12/D-91): presets
 * (Today, Yesterday, Last 7 days, This month) plus validation for the two
 * editable `YYYY-MM-DD` fields — no date-picker dependency, same approach
 * as `features/catalog/edit/form.ts`'s promo-date fields, whose
 * `isValidDateInput` this reuses rather than duplicating. `from`/`to` are
 * computed here via the shared `lib/date.ts` (`calendarDateInTimeZone`,
 * `addCalendarDays`), in the shop's own timezone, matching
 * `features/reports/period.ts`'s `periodRange`. No RN import (D-85) —
 * plain Vitest-testable logic.
 */

import { addCalendarDays, calendarDateInTimeZone } from "../../lib/date";
import { isValidDateInput } from "../catalog/edit/form";

export type DateRangePreset = "today" | "yesterday" | "last7Days" | "thisMonth";

export const DATE_RANGE_PRESETS: DateRangePreset[] = [
  "today",
  "yesterday",
  "last7Days",
  "thisMonth",
];

export interface DateRange {
  from: string;
  to: string;
}

/**
 * The `from`/`to` bounds for `preset`, ending on today's calendar date in
 * `timeZone` (all inclusive). `last7Days` is a 7-day inclusive window
 * (today and the 6 days before it), matching
 * `features/reports/period.ts`'s `periodRange` and
 * `admin/src/routes/app/ReportsPage.tsx`'s `presetRange` exactly.
 * `thisMonth` runs from the 1st of the current calendar month through
 * today.
 */
export function presetRange(
  preset: DateRangePreset,
  timeZone: string,
  now: Date = new Date(),
): DateRange {
  const today = calendarDateInTimeZone(now, timeZone);
  switch (preset) {
    case "today":
      return { from: today, to: today };
    case "yesterday": {
      const yesterday = addCalendarDays(today, -1);
      return { from: yesterday, to: yesterday };
    }
    case "last7Days":
      return { from: addCalendarDays(today, -6), to: today };
    case "thisMonth": {
      const [year, month] = today.split("-");
      return { from: `${year}-${month}-01`, to: today };
    }
  }
}

/**
 * `true` when both `from`/`to` are well-formed calendar dates (round-
 * tripped through `Date`, same as `isValidDateInput` — rejects e.g.
 * `2026-02-30`) and `from` is on or before `to`. Callers only ever query
 * `GET /sales` with a range that passes this — an invalid or reversed
 * range is left unqueried, showing a validation message instead (the
 * server would otherwise just return an empty page for a reversed range,
 * which reads as "no sales" rather than "fix your input").
 */
export function isValidRangeInput(from: string, to: string): boolean {
  const trimmedFrom = from.trim();
  const trimmedTo = to.trim();
  return isValidDateInput(trimmedFrom) && isValidDateInput(trimmedTo) && trimmedFrom <= trimmedTo;
}
