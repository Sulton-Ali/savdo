/**
 * Pure date-range helpers for the home screen's summary card (T6). Reports
 * `from`/`to` are `YYYY-MM-DD`, inclusive, in the shop's timezone
 * (`docs/05-API.md` § Conventions) — computed here via the shared
 * `lib/date.ts`'s `calendarDateInTimeZone` (also used by
 * `features/catalog/pricing.ts`'s D-67/D-68 promo-window checks). No RN
 * import (D-85) — plain Vitest-testable logic.
 */
import { calendarDateInTimeZone } from "../../lib/date";

export type ReportPeriod = "today" | "last7Days";

export interface PeriodRange {
  from: string;
  to: string;
}

/**
 * Adds `days` (negative to subtract) to a `YYYY-MM-DD` calendar date.
 * Pure calendar arithmetic on the date's own year/month/day components via
 * a UTC-anchored `Date` — never re-reads a timezone offset, so this can't
 * be thrown off by a DST transition the way adding `days * 86_400_000`
 * milliseconds to a real instant could be in a timezone that observes it
 * (the shop's isn't one, D-04's UZS-only MVP is Uzbekistan-only, but this
 * doesn't need to assume that to be correct).
 */
function addCalendarDays(isoDate: string, days: number): string {
  const [year, month, day] = isoDate.split("-").map(Number) as [number, number, number];
  const shifted = new Date(Date.UTC(year, month - 1, day));
  shifted.setUTCDate(shifted.getUTCDate() + days);
  return shifted.toISOString().slice(0, 10);
}

/**
 * The `from`/`to` bounds for `period`, ending on today's calendar date in
 * `timeZone`. `last7Days` is a 7-day inclusive window (today and the 6
 * days before it), matching `admin/src/routes/app/ReportsPage.tsx`'s
 * `presetRange`'s `last7Days` preset exactly.
 */
export function periodRange(
  period: ReportPeriod,
  timeZone: string,
  now: Date = new Date(),
): PeriodRange {
  const to = calendarDateInTimeZone(now, timeZone);
  if (period === "today") {
    return { from: to, to };
  }
  return { from: addCalendarDays(to, -6), to };
}
