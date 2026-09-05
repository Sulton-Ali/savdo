import { describe, expect, it } from "vitest";

import {
  ADJUSTMENT_REASONS,
  emptyAdjustmentForm,
  isAdjustmentFormValid,
  normalizeQty,
  validateAdjustmentForm,
} from "./adjustmentForm";

describe("ADJUSTMENT_REASONS", () => {
  it("matches the contract's fixed enum, in order", () => {
    expect(ADJUSTMENT_REASONS).toEqual(["count_correction", "damaged", "lost", "found", "other"]);
  });
});

describe("normalizeQty", () => {
  it("returns null for empty or whitespace-only input", () => {
    expect(normalizeQty("")).toBeNull();
    expect(normalizeQty("   ")).toBeNull();
  });

  it("passes through a plain integer or decimal unchanged", () => {
    expect(normalizeQty("3")).toBe("3");
    expect(normalizeQty("3.5")).toBe("3.5");
  });

  it("prefixes a leading dot with 0", () => {
    expect(normalizeQty(".5")).toBe("0.5");
    expect(normalizeQty("-.5")).toBe("-0.5");
  });

  it("drops a trailing dot", () => {
    expect(normalizeQty("3.")).toBe("3");
  });

  it("trims surrounding whitespace", () => {
    expect(normalizeQty("  -2  ")).toBe("-2");
  });

  it("keeps a negative sign for lost/damaged adjustments", () => {
    expect(normalizeQty("-2.5")).toBe("-2.5");
  });

  it("never returns a signed zero", () => {
    expect(normalizeQty("-0")).toBe("0");
  });

  it("accepts up to 3 decimal places (quantity NUMERIC(12,3))", () => {
    expect(normalizeQty("1.234")).toBe("1.234");
  });

  it("rejects more than 3 decimal places", () => {
    expect(normalizeQty("1.2345")).toBeNull();
  });

  it("rejects non-numeric input", () => {
    expect(normalizeQty("abc")).toBeNull();
    expect(normalizeQty("1..2")).toBeNull();
    expect(normalizeQty("1-2")).toBeNull();
  });
});

describe("validateAdjustmentForm / isAdjustmentFormValid", () => {
  const valid = {
    variantId: "v1",
    locationId: "l1",
    reason: "found" as const,
    qty: "2",
    note: "",
  };

  it("accepts a fully filled form", () => {
    expect(validateAdjustmentForm(valid)).toEqual({});
    expect(isAdjustmentFormValid(valid)).toBe(true);
  });

  it("requires a variant", () => {
    const errors = validateAdjustmentForm({ ...valid, variantId: null });
    expect(errors.variantId).toBe("stock.errors.variantRequired");
    expect(isAdjustmentFormValid({ ...valid, variantId: null })).toBe(false);
  });

  it("requires a location", () => {
    expect(validateAdjustmentForm({ ...valid, locationId: null }).locationId).toBe(
      "stock.errors.locationRequired",
    );
  });

  it("requires a reason", () => {
    expect(validateAdjustmentForm({ ...valid, reason: null }).reason).toBe(
      "stock.errors.reasonRequired",
    );
  });

  it("requires a valid quantity", () => {
    expect(validateAdjustmentForm({ ...valid, qty: "" }).qty).toBe("stock.errors.qtyRequired");
    expect(validateAdjustmentForm({ ...valid, qty: "abc" }).qty).toBe("stock.errors.qtyRequired");
  });

  it("accepts a negative quantity (lost/damaged) as valid", () => {
    expect(isAdjustmentFormValid({ ...valid, reason: "damaged", qty: "-3" })).toBe(true);
  });

  it("starts empty (emptyAdjustmentForm) and is invalid", () => {
    expect(isAdjustmentFormValid(emptyAdjustmentForm())).toBe(false);
  });
});
