/**
 * Trims a decimal quantity string's trailing zeros for display (`"3.000"`
 * -> `"3"`, `"1.500"` -> `"1.5"`), falling back to `"0"` when absent.
 * Display only — quantity arithmetic (if this app ever needs to sum
 * several) should use a decimal-safe method the way
 * `admin/src/stock/api.ts`'s `sumQty` does, not this. Mirrors that same
 * file's `formatQty` (this app has no shared package with `admin` beyond
 * the generated API client, so it's a small, deliberate duplicate).
 */
export function formatQty(value: string | undefined): string {
  if (value == null) {
    return "0";
  }
  const num = Number(value);
  if (!Number.isFinite(num)) {
    return value;
  }
  return String(num);
}
