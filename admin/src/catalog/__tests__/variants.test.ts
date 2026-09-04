import { describe, expect, it } from "vitest";

import type { AttributeDefinition, Variant } from "../api";
import {
  buildVariantPatch,
  canonicalAttributesKey,
  cartesianAttributeCombinations,
  combinationsToCreate,
  type VariantEditFormValues,
  variantLabel,
} from "../variants";

function variant(overrides: Partial<Variant> = {}): Variant {
  return {
    id: "v1",
    sku: null,
    barcode: null,
    attributes: {},
    priceOverride: null,
    isActive: true,
    ...overrides,
  };
}

describe("cartesianAttributeCombinations", () => {
  it("builds the full cross product of every attribute's values", () => {
    const combinations = cartesianAttributeCombinations({
      size: ["S", "M"],
      color: ["blue"],
    });
    expect(combinations).toEqual([
      { size: "S", color: "blue" },
      { size: "M", color: "blue" },
    ]);
  });

  it("skips attributes with no values entirely", () => {
    const combinations = cartesianAttributeCombinations({ size: ["S"], color: [] });
    expect(combinations).toEqual([{ size: "S" }]);
  });

  it("returns nothing when no attribute has any values", () => {
    expect(cartesianAttributeCombinations({ size: [], color: [] })).toEqual([]);
  });
});

describe("canonicalAttributesKey", () => {
  it("is independent of key order", () => {
    expect(canonicalAttributesKey({ size: "S", color: "blue" })).toBe(
      canonicalAttributesKey({ color: "blue", size: "S" }),
    );
  });
});

describe("combinationsToCreate", () => {
  it("skips combinations that already exist as a variant, regardless of key order", () => {
    const combinations = cartesianAttributeCombinations({
      size: ["S", "M"],
      color: ["blue"],
    });
    const existing = [variant({ attributes: { color: "blue", size: "S" } })];

    expect(combinationsToCreate(combinations, existing)).toEqual([{ size: "M", color: "blue" }]);
  });

  it("keeps every combination when none exist yet", () => {
    const combinations = cartesianAttributeCombinations({ size: ["S"] });
    expect(combinationsToCreate(combinations, [])).toEqual(combinations);
  });
});

describe("variantLabel", () => {
  const defs: AttributeDefinition[] = [
    {
      id: "a1",
      code: "size",
      sortOrder: 0,
      name: "Size",
      locale: "en",
      translationFallback: false,
    },
    {
      id: "a2",
      code: "color",
      sortOrder: 1,
      name: "Colour",
      locale: "en",
      translationFallback: false,
    },
  ];

  it("joins attribute values in definition order", () => {
    expect(variantLabel(variant({ attributes: { color: "blue", size: "L" } }), defs)).toBe(
      "L / blue",
    );
  });

  it("falls back to the SKU when there are no attribute values", () => {
    expect(variantLabel(variant({ sku: "TS-1" }), defs)).toBe("TS-1");
  });

  it("falls back to the id when there is neither", () => {
    expect(variantLabel(variant({ id: "v9" }), defs)).toBe("v9");
  });
});

describe("buildVariantPatch", () => {
  const original = variant({
    id: "v1",
    sku: "OLD",
    barcode: "123",
    attributes: { size: "M" },
    priceOverride: "100.00",
    isActive: true,
  });

  it("sends null to clear priceOverride", () => {
    const values: VariantEditFormValues = {
      sku: "OLD",
      barcode: "123",
      priceOverride: undefined,
      isActive: true,
      attributes: { size: "M" },
    };
    expect(buildVariantPatch(original, values, ["size"])).toEqual({ priceOverride: null });
  });

  it("omits costOverride entirely when the caller has no cost.read permission", () => {
    // `original` has no `costOverride` key at all (ADR-010) — clearing a
    // field the caller never received must never be attempted.
    const values: VariantEditFormValues = {
      sku: "OLD",
      barcode: "123",
      priceOverride: 100,
      costOverride: undefined,
      isActive: true,
      attributes: { size: "M" },
    };
    const patch = buildVariantPatch(original, values, ["size"]);
    expect(patch).not.toHaveProperty("costOverride");
  });

  it("sends null to clear costOverride when it was previously set", () => {
    const withCost = variant({ ...original, costOverride: "50.00" });
    const values: VariantEditFormValues = {
      sku: "OLD",
      barcode: "123",
      priceOverride: 100,
      costOverride: undefined,
      isActive: true,
      attributes: { size: "M" },
    };
    expect(buildVariantPatch(withCost, values, ["size"])).toEqual({ costOverride: null });
  });

  it("only includes fields that actually changed", () => {
    const values: VariantEditFormValues = {
      sku: "OLD",
      barcode: "123",
      priceOverride: 100,
      isActive: true,
      attributes: { size: "M" },
    };
    expect(buildVariantPatch(original, values, ["size"])).toEqual({});
  });

  it("includes a full attributes replacement when any value changed", () => {
    const values: VariantEditFormValues = {
      sku: "OLD",
      barcode: "123",
      priceOverride: 100,
      isActive: true,
      attributes: { size: "L" },
    };
    expect(buildVariantPatch(original, values, ["size"])).toEqual({ attributes: { size: "L" } });
  });

  it("sends null to clear sku and barcode", () => {
    const values: VariantEditFormValues = {
      sku: "",
      barcode: "",
      priceOverride: 100,
      isActive: true,
      attributes: { size: "M" },
    };
    expect(buildVariantPatch(original, values, ["size"])).toEqual({ sku: null, barcode: null });
  });
});
