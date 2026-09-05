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
