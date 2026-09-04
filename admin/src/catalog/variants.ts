import { formatMoney } from "../lib/money";
import type { AttributeDefinition, AttributeValues, Variant, VariantPatch } from "./api";

/**
 * A sorted, key-order-independent string key for an `AttributeValues`
 * object — two variants with the same values in a different key order (or a
 * matrix combination built in a different attribute order) must compare
 * equal (`docs/04-DATA-MODEL.md` § 2, unique `(product_id, attributes)`).
 *
 * Serializes as JSON `[key, value]` pairs rather than a delimited string:
 * a plain `"key=value"` join can collide across different attribute sets
 * that happen to contain the same characters (e.g. a single `color`
 * attribute valued `"size=M"` versus separate `color`/`size` attributes),
 * which JSON's own escaping and structural nesting rules out.
 */
export function canonicalAttributesKey(attributes: AttributeValues): string {
  return JSON.stringify(
    Object.keys(attributes)
      .sort()
      .map((key) => [key, attributes[key]]),
  );
}

/** The Cartesian product of each attribute code's selected values, e.g.
 * `{ size: ["S", "M"], color: ["blue"] }` → `[{size:"S",color:"blue"},
 * {size:"M",color:"blue"}]`. An attribute with no values contributes
 * nothing — the matrix only varies attributes the user actually filled in. */
export function cartesianAttributeCombinations(
  valuesByCode: Record<string, string[]>,
): AttributeValues[] {
  const codes = Object.keys(valuesByCode).filter((code) => (valuesByCode[code] ?? []).length > 0);
  if (codes.length === 0) {
    return [];
  }
  let combinations: AttributeValues[] = [{}];
  for (const code of codes) {
    const next: AttributeValues[] = [];
    for (const combination of combinations) {
      for (const value of valuesByCode[code] ?? []) {
        next.push({ ...combination, [code]: value });
      }
    }
    combinations = next;
  }
  return combinations;
}

/** Drops any combination the product already has a variant for, comparing
 * by `canonicalAttributesKey` (order-independent). */
export function combinationsToCreate(
  combinations: AttributeValues[],
  existingVariants: Pick<Variant, "attributes">[],
): AttributeValues[] {
  const existingKeys = new Set(
    existingVariants.map((variant) => canonicalAttributesKey(variant.attributes)),
  );
  return combinations.filter(
    (combination) => !existingKeys.has(canonicalAttributesKey(combination)),
  );
}

/** A short, human label for a variant in a `Select` (image variant tag,
 * matrix preview): its attribute values in attribute-definition order
 * (e.g. "L / Blue"), falling back to its SKU, then its id. */
export function variantLabel(
  variant: Pick<Variant, "id" | "sku" | "attributes">,
  attributeDefinitions: AttributeDefinition[],
): string {
  const parts = attributeDefinitions
    .map((def) => variant.attributes[def.code])
    .filter((value): value is string => Boolean(value));
  if (parts.length > 0) {
    return parts.join(" / ");
  }
  return variant.sku ?? variant.id;
}

/** Form values for the variant edit drawer — attributes keyed by
 * `attribute_definitions.code`, one text field each. */
export interface VariantEditFormValues {
  sku?: string;
  barcode?: string;
  priceOverride?: number;
  costOverride?: number;
  isActive: boolean;
  attributes?: Record<string, string>;
}

/**
 * Diffs the edit drawer's values against the variant being edited and
 * builds the smallest `VariantPatch` that applies them — only changed
 * fields are included, and a cleared override is sent as explicit `null`
 * (D-35), never omitted. `costOverride` is only ever included when the
 * original `Variant` carries the key at all (`cost.read`, ADR-010) — a
 * caller without that permission never sees the field to clear it.
 */
export function buildVariantPatch(
  original: Variant,
  values: VariantEditFormValues,
  attributeCodes: string[],
): VariantPatch {
  const patch: VariantPatch = {};

  const normSku = values.sku?.trim() || null;
  if (normSku !== (original.sku ?? null)) {
    patch.sku = normSku;
  }

  const normBarcode = values.barcode?.trim() || null;
  if (normBarcode !== (original.barcode ?? null)) {
    patch.barcode = normBarcode;
  }

  const normPrice =
    values.priceOverride != null ? (formatMoney(values.priceOverride) ?? null) : null;
  if (normPrice !== (original.priceOverride ?? null)) {
    patch.priceOverride = normPrice;
  }

  if (original.costOverride !== undefined) {
    const normCost =
      values.costOverride != null ? (formatMoney(values.costOverride) ?? null) : null;
    if (normCost !== (original.costOverride ?? null)) {
      patch.costOverride = normCost;
    }
  }

  if (values.isActive !== original.isActive) {
    patch.isActive = values.isActive;
  }

  let attributesChanged = false;
  const nextAttributes: AttributeValues = {};
  for (const code of attributeCodes) {
    const value = values.attributes?.[code]?.trim() ?? "";
    nextAttributes[code] = value;
    if (value !== (original.attributes[code] ?? "")) {
      attributesChanged = true;
    }
  }
  if (attributesChanged) {
    patch.attributes = nextAttributes;
  }

  return patch;
}
