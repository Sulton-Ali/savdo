/**
 * Client-side *preview* of the price the server will charge for a variant
 * (`docs/03-ARCHITECTURE.md` § Quick sale: "loads current prices and promo
 * prices for each variant"; D-56: "line prices always come from the
 * catalogue (promo price when active)"). `POST /sales` never sends a price
 * — only `variantId`/`qty` — so getting this resolution slightly wrong
 * never breaks correctness, only the cart preview; the server recomputes
 * every total authoritatively (hard rule 8).
 *
 * Precedence (D-67): the shop-wide promo price when currently active (promo
 * pricing is product-level only, `docs/05-API.md` § Conventions, and there
 * is a single `promoPrice` for the whole product regardless of variant),
 * else the variant's own `priceOverride` when set, else the product's
 * `basePrice`.
 */
export interface PriceableProduct {
  basePrice: string;
  promoPrice: string | null;
  promoFrom: string | null;
  promoTo: string | null;
}

export interface PriceableVariant {
  priceOverride: string | null;
}

/** `date`'s calendar date (`YYYY-MM-DD`) in `timeZone`, via `Intl`'s `en-CA`
 * formatting (which happens to be ISO order) — no date library needed, and
 * the resulting strings compare correctly with plain `<=`/`>=`. */
function calendarDateInTimeZone(date: Date, timeZone: string): string {
  return new Intl.DateTimeFormat("en-CA", {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(date);
}

/** D-68: `promoFrom`/`promoTo` are calendar-day bounds in the shop
 * timezone, not exact instants — a promo is active on every calendar day
 * from `promoFrom`'s date to `promoTo`'s date inclusive, time part
 * ignored. */
function isPromoActive(product: PriceableProduct, timeZone: string, now: Date): boolean {
  if (product.promoPrice == null || product.promoFrom == null || product.promoTo == null) {
    return false;
  }
  const today = calendarDateInTimeZone(now, timeZone);
  const fromDay = calendarDateInTimeZone(new Date(product.promoFrom), timeZone);
  const toDay = calendarDateInTimeZone(new Date(product.promoTo), timeZone);
  return today >= fromDay && today <= toDay;
}

export function resolveEffectivePrice(
  product: PriceableProduct,
  variant: PriceableVariant,
  timeZone: string,
  now: Date = new Date(),
): string {
  if (isPromoActive(product, timeZone, now) && product.promoPrice != null) {
    return product.promoPrice;
  }
  return variant.priceOverride ?? product.basePrice;
}
