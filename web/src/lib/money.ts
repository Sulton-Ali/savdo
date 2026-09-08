/**
 * Formats a `Decimal` wire string (`"125000.00"`, ADR-007 — money is a
 * string on the wire, never a float) for display, given the shop's
 * `currency` (`PublicShop.currency`, threaded through route context —
 * never hard-coded). Mirrors `mobile/src/lib/money.ts`'s `formatMoney`
 * exactly: thousands-separated with spaces, and UZS (the only currency
 * this MVP ever has, D-04) drops its fractional part outright rather than
 * keeping it when non-zero, since it has no everyday subunit. Works on the
 * string directly (no `Number` parsing, hence no precision loss) —
 * display-only, never used for arithmetic (hard rule 4/8).
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

/** `formatMoney` plus the currency code itself (e.g. `"1 200 000 UZS"`) —
 * every price shown to a visitor carries the shop's currency (never
 * assumed), per review. */
export function formatMoneyWithCurrency(value: string, currency: string): string {
  return `${formatMoney(value, currency)} ${currency}`;
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
 * to it (D-103). Both carry the shop's `currency`.
 */
export function displayPrice(
  price: PriceLike,
  currency: string,
): {
  current: string;
  strikethrough: string | null;
} {
  return {
    current: formatMoneyWithCurrency(price.current, currency),
    strikethrough: price.promoActive ? formatMoneyWithCurrency(price.regular, currency) : null,
  };
}
