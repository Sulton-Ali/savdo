/**
 * Client-side resolution of the price to *display* for a product/variant
 * (never sent to the server — `POST /sales` only ever carries `variantId`/
 * `qty`, hard rule 8). Precedence (D-67): the product-wide promo price when
 * currently active, else the variant's own `priceOverride`, else the
 * product's `basePrice`. Mirrors
 * `admin/src/routes/app/quick-sale/pricing.ts` exactly (same rules, same
 * D-68 calendar-day-in-shop-timezone semantics) — kept as a separate copy
 * rather than a shared import because `admin` and `mobile` are separate
 * packages with no shared "business logic" package (only `@savdo/i18n`,
 * `@savdo/ui-tokens` and the generated `@savdo/api-client` are shared).
 */
import { calendarDateInTimeZone } from "../../lib/date";

export interface PriceableProduct {
  basePrice: string;
  promoPrice: string | null;
  promoFrom: string | null;
  promoTo: string | null;
}

export interface PriceableVariant {
  priceOverride: string | null;
}

/** D-68: `promoFrom`/`promoTo` are calendar-day bounds in the shop
 * timezone, not exact instants — a promo is active on every calendar day
 * from `promoFrom`'s date to `promoTo`'s date inclusive, time part
 * ignored. */
export function isPromoActive(product: PriceableProduct, timeZone: string, now: Date): boolean {
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
