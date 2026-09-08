/**
 * Formats a `Decimal` wire string (`"125000.00"`, ADR-007 — money is a
 * string on the wire, never a float) for display, with thousands
 * separators (`"125000.00"` -> `"125,000"`), dropping a trailing
 * `.00`/`.000` but keeping a non-zero fraction. Mirrors
 * `admin/src/lib/money.ts`'s `formatMoneyDisplay` — works on the string
 * directly (no `Number` parsing, hence no precision loss); display-only,
 * never used for arithmetic (hard rule 4/8 — never trust or recompute
 * client-side).
 */
export function formatMoneyDisplay(value: string): string {
  const trimmed = value.trim();
  const negative = trimmed.startsWith("-");
  const unsigned = negative ? trimmed.slice(1) : trimmed;
  const [intPartRaw, fracPart = ""] = unsigned.split(".");
  const intPart = (intPartRaw || "0").replace(/^0+(?=\d)/, "");
  const withSeparators = intPart.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  const hasFraction = /[1-9]/.test(fracPart);
  const suffix = hasFraction ? `.${fracPart}` : "";
  const sign = negative && (withSeparators !== "0" || hasFraction) ? "-" : "";
  return `${sign}${withSeparators}${suffix}`;
}

/** A `PublicPrice` (`components["schemas"]["PublicPrice"]`), narrowed to the
 * fields `displayPrice` needs — avoids importing the generated schema type
 * just for this helper's signature. */
export type PriceLike = {
  regular: string;
  current: string;
  promoActive: boolean;
};

/**
 * Splits a `PublicPrice` into what a product card / product page shows: the
 * price a customer pays now, and — only while a promo is active (D-67/D-68,
 * resolved server-side) — the regular price to render struck through next
 * to it (D-103).
 */
export function displayPrice(price: PriceLike): {
  current: string;
  strikethrough: string | null;
} {
  return {
    current: formatMoneyDisplay(price.current),
    strikethrough: price.promoActive ? formatMoneyDisplay(price.regular) : null,
  };
}
