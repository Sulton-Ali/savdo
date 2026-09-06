/**
 * `date`'s calendar date (`YYYY-MM-DD`) in `timeZone`, via `Intl`'s `en-CA`
 * formatting (which happens to be ISO order) — no date library needed, and
 * the resulting strings compare correctly with plain `<=`/`>=`.
 *
 * Shared between `features/catalog/pricing.ts` (D-67/D-68 promo-window
 * checks) and `features/reports/period.ts` (report date ranges) — both used
 * to keep their own copy of this exact function; consolidated here since
 * both are plain TypeScript with no React Native import (D-85).
 */
export function calendarDateInTimeZone(date: Date, timeZone: string): string {
  return new Intl.DateTimeFormat("en-CA", {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(date);
}

/**
 * Adds `days` (negative to subtract) to a `YYYY-MM-DD` calendar date. Pure
 * calendar arithmetic on the date's own year/month/day components via a
 * UTC-anchored `Date` — never re-reads a timezone offset, so this can't be
 * thrown off by a DST transition the way adding `days * 86_400_000`
 * milliseconds to a real instant could be in a timezone that observes one
 * (the shop's isn't one, D-04's UZS-only MVP is Uzbekistan-only, but this
 * doesn't need to assume that to be correct).
 *
 * Shared between `features/reports/period.ts` (T6's home-screen summary
 * period) and `features/sales/dateRange.ts` (T12's sales-list presets) —
 * both used to keep (or would otherwise each need) their own copy of this
 * exact function, same reasoning as `calendarDateInTimeZone` above.
 */
export function addCalendarDays(isoDate: string, days: number): string {
  const [year, month, day] = isoDate.split("-").map(Number) as [number, number, number];
  const shifted = new Date(Date.UTC(year, month - 1, day));
  shifted.setUTCDate(shifted.getUTCDate() + days);
  return shifted.toISOString().slice(0, 10);
}
