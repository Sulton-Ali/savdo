import type { Product, Variant } from "../api";
import type { ProductPatch, VariantPatch } from "./api";

/** Matches `contracts/openapi.yaml`'s `Decimal`/`format: decimal` pattern
 * exactly (ADR-007: money is a decimal string, never parsed to a number on
 * this screen). */
const DECIMAL_PATTERN = /^-?\d+(\.\d+)?$/;
const DATE_PATTERN = /^\d{4}-\d{2}-\d{2}$/;
const INTEGER_PATTERN = /^\d+$/;

export function isValidDecimal(value: string): boolean {
  return DECIMAL_PATTERN.test(value.trim());
}

/** A calendar date typed as `YYYY-MM-DD` (D-77: no date-picker dependency),
 * rejecting both malformed strings and out-of-range ones a naive regex
 * would let through (e.g. `2026-02-30`) by round-tripping through `Date`. */
export function isValidDateInput(value: string): boolean {
  const trimmed = value.trim();
  if (!DATE_PATTERN.test(trimmed)) {
    return false;
  }
  const date = new Date(`${trimmed}T00:00:00.000Z`);
  return !Number.isNaN(date.getTime()) && date.toISOString().slice(0, 10) === trimmed;
}

export function isValidLowStockThreshold(value: string): boolean {
  const trimmed = value.trim();
  return trimmed === "" || INTEGER_PATTERN.test(trimmed);
}

export type PromoFieldError = "incomplete" | "invalidPrice" | "invalidDate" | "rangeInvalid";

/**
 * Validates the product's promo trio as one unit: either all three
 * (`promoPrice`/`promoFrom`/`promoTo`) are blank (no promo) or all three
 * are filled and individually valid, with `promoFrom` on or before
 * `promoTo`. This all-or-nothing rule is deliberate, not just a UI
 * shortcut: the server (`api/internal/catalog/products.go`
 * `updateProductAttempt`, `api/db/queries/products.sql`'s `clear_promo`)
 * clears `promo_price`/`promo_from`/`promo_to` together as one concept the
 * moment any single one of them is explicitly nulled, so a client that let
 * the three drift independently could silently wipe two fields the user
 * never touched.
 */
export function validatePromoFields(values: {
  promoPrice: string;
  promoFrom: string;
  promoTo: string;
}): PromoFieldError | undefined {
  const price = values.promoPrice.trim();
  const from = values.promoFrom.trim();
  const to = values.promoTo.trim();
  const filledCount = [price, from, to].filter((value) => value !== "").length;
  if (filledCount === 0) {
    return undefined;
  }
  if (filledCount < 3) {
    return "incomplete";
  }
  if (!isValidDecimal(price)) {
    return "invalidPrice";
  }
  if (!isValidDateInput(from) || !isValidDateInput(to)) {
    return "invalidDate";
  }
  if (from > to) {
    return "rangeInvalid";
  }
  return undefined;
}

/** `promoFrom`/`promoTo` (`Product`) are `date-time` on the wire; this
 * screen only ever edits the calendar-day part (D-68: the time part is
 * ignored for promo activity), so display/comparison both work off the
 * first 10 characters of the stored ISO string rather than converting
 * through the shop's timezone — simpler, and never wrong for this screen's
 * purpose since the exact instant is never shown or compared to anything
 * else here. */
function isoDatePart(iso: string): string {
  return iso.slice(0, 10);
}

export interface ProductFormValues {
  basePrice: string;
  promoPrice: string;
  promoFrom: string;
  promoTo: string;
  isActive: boolean;
  lowStockThreshold: string;
}

export function productToFormValues(product: Product): ProductFormValues {
  return {
    basePrice: product.basePrice,
    promoPrice: product.promoPrice ?? "",
    promoFrom: product.promoFrom ? isoDatePart(product.promoFrom) : "",
    promoTo: product.promoTo ? isoDatePart(product.promoTo) : "",
    isActive: product.isActive,
    lowStockThreshold: product.lowStockThreshold != null ? String(product.lowStockThreshold) : "",
  };
}

/**
 * Diffs `values` against `product` into a `ProductPatch` carrying only the
 * fields that actually changed — never the rest of the product (this quick
 * edit screen renders neither `translations` nor `categoryId`, and must
 * not clobber them). Callers must have already checked
 * `validatePromoFields`/`isValidDecimal`/`isValidLowStockThreshold`; this
 * function assumes `values` is valid.
 */
export function buildProductPatch(product: Product, values: ProductFormValues): ProductPatch {
  const patch: ProductPatch = {};

  const basePrice = values.basePrice.trim();
  if (basePrice && basePrice !== product.basePrice) {
    patch.basePrice = basePrice;
  }

  const promoPrice = values.promoPrice.trim() || null;
  const promoFromDate = values.promoFrom.trim() || null;
  const promoToDate = values.promoTo.trim() || null;
  const currentPromoPrice = product.promoPrice ?? null;
  const currentPromoFromDate = product.promoFrom ? isoDatePart(product.promoFrom) : null;
  const currentPromoToDate = product.promoTo ? isoDatePart(product.promoTo) : null;

  if (promoPrice === null && currentPromoPrice !== null) {
    // Clearing the promo: send an explicit `null` for all three — the
    // server treats them as one concept (see `validatePromoFields`'s
    // comment), so omitting any of them here would leave it untouched.
    patch.promoPrice = null;
    patch.promoFrom = null;
    patch.promoTo = null;
  } else if (promoPrice !== null) {
    if (promoPrice !== currentPromoPrice) {
      patch.promoPrice = promoPrice;
    }
    if (promoFromDate !== currentPromoFromDate) {
      patch.promoFrom = promoFromDate ? `${promoFromDate}T00:00:00.000Z` : null;
    }
    if (promoToDate !== currentPromoToDate) {
      patch.promoTo = promoToDate ? `${promoToDate}T00:00:00.000Z` : null;
    }
  }

  if (values.isActive !== product.isActive) {
    patch.isActive = values.isActive;
  }

  const trimmedThreshold = values.lowStockThreshold.trim();
  const lowStockThreshold = trimmedThreshold ? Number(trimmedThreshold) : null;
  if (lowStockThreshold !== (product.lowStockThreshold ?? null)) {
    patch.lowStockThreshold = lowStockThreshold;
  }

  return patch;
}

export interface VariantFormValues {
  priceOverride: string;
  isActive: boolean;
}

export function variantToFormValues(variant: Variant): VariantFormValues {
  return {
    priceOverride: variant.priceOverride ?? "",
    isActive: variant.isActive,
  };
}

/** Diffs `values` against `variant` into a `VariantPatch` carrying only the
 * changed fields. Callers must have already checked `isValidDecimal` on a
 * non-blank `priceOverride`. */
export function buildVariantPatch(variant: Variant, values: VariantFormValues): VariantPatch {
  const patch: VariantPatch = {};

  const priceOverride = values.priceOverride.trim() || null;
  if (priceOverride !== (variant.priceOverride ?? null)) {
    patch.priceOverride = priceOverride;
  }

  if (values.isActive !== variant.isActive) {
    patch.isActive = values.isActive;
  }

  return patch;
}
