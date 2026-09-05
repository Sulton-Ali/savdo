import type { AdjustmentReason } from "./api";

/**
 * Pure, RN-free adjustment-form state and validation (D-85) — the reducer
 * lives entirely in this module so it's testable with plain Vitest; the
 * screen (`app/(app)/stock/adjust.tsx`) only wires it to `react-hook-form`
 * `Controller`s and `VariantPicker`. Mirrors the admin's
 * `StockActionsDrawer.tsx` `StockAdjustmentDrawer` rules exactly (same
 * fixed reason enum, no `min`/nonzero constraint on `qty` beyond "present
 * and a valid decimal" — the admin's `InputNumber` doesn't clamp a lower
 * bound on it either, since a negative value is exactly how "lost/damaged"
 * is recorded).
 */
export const ADJUSTMENT_REASONS: AdjustmentReason[] = [
  "count_correction",
  "damaged",
  "lost",
  "found",
  "other",
];

/** `quantity NUMERIC(12,3)` (`docs/04-DATA-MODEL.md` § 8) — at most 3
 * decimal places on the wire. */
const MAX_QTY_DECIMALS = 3;

export interface AdjustmentFormValues {
  variantId: string | null;
  locationId: string | null;
  reason: AdjustmentReason | null;
  /** Raw text-field input, not yet validated/normalised. */
  qty: string;
  note: string;
}

export function emptyAdjustmentForm(): AdjustmentFormValues {
  return { variantId: null, locationId: null, reason: null, qty: "", note: "" };
}

/**
 * Normalises a raw quantity input into the wire `Decimal` shape
 * (`^-?\d+(\.\d+)?$`, `contracts/openapi.yaml`'s `Decimal` schema) — e.g.
 * `".5"` -> `"0.5"`, `"3."` -> `"3"`, `" -2 "` -> `"-2"`. Returns `null` for
 * anything that isn't a valid decimal with at most `MAX_QTY_DECIMALS`
 * places (including empty input) — display-only normalisation, never
 * arithmetic, so this never risks the float-precision issues `Number` would
 * (`admin/src/stock/api.ts`'s `toMilliUnits` comment).
 */
export function normalizeQty(raw: string): string | null {
  const trimmed = raw.trim();
  if (!trimmed) {
    return null;
  }
  const negative = trimmed.startsWith("-");
  let unsigned = negative ? trimmed.slice(1) : trimmed;
  if (unsigned.startsWith(".")) {
    unsigned = `0${unsigned}`;
  }
  if (unsigned.endsWith(".")) {
    unsigned = unsigned.slice(0, -1);
  }
  if (!/^\d+(\.\d+)?$/.test(unsigned)) {
    return null;
  }
  const [, fracPart = ""] = unsigned.split(".");
  if (fracPart.length > MAX_QTY_DECIMALS) {
    return null;
  }
  return `${negative && unsigned !== "0" ? "-" : ""}${unsigned}`;
}

export type AdjustmentFieldErrors = Partial<
  Record<"variantId" | "locationId" | "reason" | "qty", string>
>;

/**
 * Validates the whole form, returning a translation-key error per invalid
 * field (empty object = valid) — the keys are exactly the shared
 * `stock.errors.*` ones the admin form already uses (deliverable 5: reuse,
 * don't duplicate translations), so the screen just calls `t(key)`.
 */
export function validateAdjustmentForm(values: AdjustmentFormValues): AdjustmentFieldErrors {
  const errors: AdjustmentFieldErrors = {};
  if (!values.variantId) {
    errors.variantId = "stock.errors.variantRequired";
  }
  if (!values.locationId) {
    errors.locationId = "stock.errors.locationRequired";
  }
  if (!values.reason) {
    errors.reason = "stock.errors.reasonRequired";
  }
  if (normalizeQty(values.qty) == null) {
    errors.qty = "stock.errors.qtyRequired";
  }
  return errors;
}

export function isAdjustmentFormValid(values: AdjustmentFormValues): boolean {
  return Object.keys(validateAdjustmentForm(values)).length === 0;
}
