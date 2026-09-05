/**
 * Formats a `Decimal` wire string (ADR-007 — money is a string on the wire,
 * never a float) for display, given the shop's `currency`
 * (`useSession().me.shop.currency`, `Shop.currency`). Works on the string
 * directly (no `Number` parsing, hence no precision loss) — display-only,
 * never used for arithmetic. Mirrors `admin/src/lib/money.ts`'s
 * `formatMoneyDisplay`, with one addition: UZS (the only currency this MVP
 * ever has, D-04) has no everyday subunit, so its fractional part is
 * dropped outright rather than kept when non-zero.
 */
export function formatMoney(value: string, currency: string): string {
  const trimmed = value.trim();
  const negative = trimmed.startsWith("-");
  const unsigned = negative ? trimmed.slice(1) : trimmed;
  const [intPartRaw, fracPart = ""] = unsigned.split(".");
  const intPart = (intPartRaw || "0").replace(/^0+(?=\d)/, "");
  const withSeparators = intPart.replace(/\B(?=(\d{3})+(?!\d))/g, " ");
  const showFraction = currency !== "UZS" && /[1-9]/.test(fracPart);
  const suffix = showFraction ? `.${fracPart}` : "";
  const sign = negative && (withSeparators !== "0" || showFraction) ? "-" : "";
  return `${sign}${withSeparators}${suffix}`;
}
