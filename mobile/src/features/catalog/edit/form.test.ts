import { describe, expect, it } from "vitest";

import type { Product, Variant } from "../api";
import {
  buildProductPatch,
  buildVariantPatch,
  isValidDateInput,
  isValidDecimal,
  isValidLowStockThreshold,
  productToFormValues,
  validatePromoFields,
  variantToFormValues,
} from "./form";

function makeProduct(overrides: Partial<Product> = {}): Product {
  return {
    id: "11111111-1111-1111-1111-111111111111",
    categoryId: null,
    slug: "koylak",
    sku: "SKU-1",
    unitId: "22222222-2222-2222-2222-222222222222",
    basePrice: "100000.00",
    promoPrice: null,
    promoFrom: null,
    promoTo: null,
    isActive: true,
    isFeatured: false,
    name: "Koylak",
    description: null,
    locale: "uz",
    translationFallback: false,
    lowStockThreshold: null,
    ...overrides,
  };
}

function makeVariant(overrides: Partial<Variant> = {}): Variant {
  return {
    id: "33333333-3333-3333-3333-333333333333",
    sku: "SKU-1-L",
    barcode: null,
    attributes: { size: "L" },
    priceOverride: null,
    isActive: true,
    ...overrides,
  };
}

describe("isValidDecimal", () => {
  it("accepts integers and decimals", () => {
    expect(isValidDecimal("100000")).toBe(true);
    expect(isValidDecimal("100000.50")).toBe(true);
  });

  it("rejects commas, currency symbols and blanks", () => {
    expect(isValidDecimal("100,000")).toBe(false);
    expect(isValidDecimal("100000 UZS")).toBe(false);
    expect(isValidDecimal("")).toBe(false);
  });
});

describe("isValidDateInput", () => {
  it("accepts a real calendar date in YYYY-MM-DD", () => {
    expect(isValidDateInput("2026-09-05")).toBe(true);
  });

  it("rejects malformed and out-of-range dates", () => {
    expect(isValidDateInput("2026-9-5")).toBe(false);
    expect(isValidDateInput("2026-02-30")).toBe(false);
    expect(isValidDateInput("not-a-date")).toBe(false);
  });
});

describe("isValidLowStockThreshold", () => {
  it("accepts blank (shop default) and non-negative integers", () => {
    expect(isValidLowStockThreshold("")).toBe(true);
    expect(isValidLowStockThreshold("0")).toBe(true);
    expect(isValidLowStockThreshold("5")).toBe(true);
  });

  it("rejects negative numbers and decimals", () => {
    expect(isValidLowStockThreshold("-1")).toBe(false);
    expect(isValidLowStockThreshold("1.5")).toBe(false);
  });
});

describe("validatePromoFields", () => {
  it("is valid when all three are blank (no promo)", () => {
    expect(validatePromoFields({ promoPrice: "", promoFrom: "", promoTo: "" })).toBeUndefined();
  });

  it("is valid when all three are filled correctly", () => {
    expect(
      validatePromoFields({ promoPrice: "80000", promoFrom: "2026-09-01", promoTo: "2026-09-10" }),
    ).toBeUndefined();
  });

  it("flags a partially filled trio as incomplete", () => {
    expect(validatePromoFields({ promoPrice: "80000", promoFrom: "", promoTo: "" })).toBe(
      "incomplete",
    );
    expect(
      validatePromoFields({ promoPrice: "", promoFrom: "2026-09-01", promoTo: "2026-09-10" }),
    ).toBe("incomplete");
  });

  it("flags an invalid promo price", () => {
    expect(
      validatePromoFields({ promoPrice: "eighty", promoFrom: "2026-09-01", promoTo: "2026-09-10" }),
    ).toBe("invalidPrice");
  });

  it("flags an invalid date", () => {
    expect(
      validatePromoFields({ promoPrice: "80000", promoFrom: "09/01/2026", promoTo: "2026-09-10" }),
    ).toBe("invalidDate");
  });

  it("flags promoFrom after promoTo", () => {
    expect(
      validatePromoFields({ promoPrice: "80000", promoFrom: "2026-09-10", promoTo: "2026-09-01" }),
    ).toBe("rangeInvalid");
  });
});

describe("productToFormValues", () => {
  it("maps a product with no promo", () => {
    expect(productToFormValues(makeProduct())).toEqual({
      basePrice: "100000.00",
      promoPrice: "",
      promoFrom: "",
      promoTo: "",
      isActive: true,
      lowStockThreshold: "",
    });
  });

  it("maps a product with an active promo and a threshold override", () => {
    const product = makeProduct({
      promoPrice: "80000.00",
      promoFrom: "2026-09-01T00:00:00.000Z",
      promoTo: "2026-09-10T00:00:00.000Z",
      lowStockThreshold: 3,
    });
    expect(productToFormValues(product)).toEqual({
      basePrice: "100000.00",
      promoPrice: "80000.00",
      promoFrom: "2026-09-01",
      promoTo: "2026-09-10",
      isActive: true,
      lowStockThreshold: "3",
    });
  });
});

describe("buildProductPatch", () => {
  it("returns an empty patch when nothing changed", () => {
    const product = makeProduct();
    const values = productToFormValues(product);
    expect(buildProductPatch(product, values)).toEqual({});
  });

  it("only includes the changed base price", () => {
    const product = makeProduct();
    const values = { ...productToFormValues(product), basePrice: "120000.00" };
    expect(buildProductPatch(product, values)).toEqual({ basePrice: "120000.00" });
  });

  it("adds a new promo as all three fields", () => {
    const product = makeProduct();
    const values = {
      ...productToFormValues(product),
      promoPrice: "80000.00",
      promoFrom: "2026-09-01",
      promoTo: "2026-09-10",
    };
    expect(buildProductPatch(product, values)).toEqual({
      promoPrice: "80000.00",
      promoFrom: "2026-09-01T00:00:00.000Z",
      promoTo: "2026-09-10T00:00:00.000Z",
    });
  });

  it("clears an existing promo by nulling all three fields together", () => {
    const product = makeProduct({
      promoPrice: "80000.00",
      promoFrom: "2026-09-01T00:00:00.000Z",
      promoTo: "2026-09-10T00:00:00.000Z",
    });
    const values = { ...productToFormValues(product), promoPrice: "", promoFrom: "", promoTo: "" };
    expect(buildProductPatch(product, values)).toEqual({
      promoPrice: null,
      promoFrom: null,
      promoTo: null,
    });
  });

  it("leaves an unchanged promo out of the patch entirely", () => {
    const product = makeProduct({
      promoPrice: "80000.00",
      promoFrom: "2026-09-01T00:00:00.000Z",
      promoTo: "2026-09-10T00:00:00.000Z",
    });
    const values = productToFormValues(product);
    expect(buildProductPatch(product, values)).toEqual({});
  });

  it("clears the low-stock threshold override back to the shop default", () => {
    const product = makeProduct({ lowStockThreshold: 5 });
    const values = { ...productToFormValues(product), lowStockThreshold: "" };
    expect(buildProductPatch(product, values)).toEqual({ lowStockThreshold: null });
  });

  it("sets isActive only when it changed", () => {
    const product = makeProduct({ isActive: true });
    const values = { ...productToFormValues(product), isActive: false };
    expect(buildProductPatch(product, values)).toEqual({ isActive: false });
  });
});

describe("variantToFormValues / buildVariantPatch", () => {
  it("maps a variant with no price override", () => {
    expect(variantToFormValues(makeVariant())).toEqual({ priceOverride: "", isActive: true });
  });

  it("returns an empty patch when nothing changed", () => {
    const variant = makeVariant();
    expect(buildVariantPatch(variant, variantToFormValues(variant))).toEqual({});
  });

  it("sets a new price override", () => {
    const variant = makeVariant();
    const values = { ...variantToFormValues(variant), priceOverride: "95000.00" };
    expect(buildVariantPatch(variant, values)).toEqual({ priceOverride: "95000.00" });
  });

  it("clears an existing price override", () => {
    const variant = makeVariant({ priceOverride: "95000.00" });
    const values = { ...variantToFormValues(variant), priceOverride: "" };
    expect(buildVariantPatch(variant, values)).toEqual({ priceOverride: null });
  });

  it("toggles isActive", () => {
    const variant = makeVariant({ isActive: true });
    const values = { ...variantToFormValues(variant), isActive: false };
    expect(buildVariantPatch(variant, values)).toEqual({ isActive: false });
  });
});
