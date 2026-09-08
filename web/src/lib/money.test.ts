import { describe, expect, it } from "vitest";

import { displayPrice, formatMoney, formatMoneyWithCurrency } from "./money";

describe("formatMoney", () => {
  it("adds thousands separators with spaces", () => {
    expect(formatMoney("125000.00", "UZS")).toBe("125 000");
    expect(formatMoney("1000000.00", "UZS")).toBe("1 000 000");
  });

  it("always drops the fraction for UZS, even when non-zero", () => {
    expect(formatMoney("125000.00", "UZS")).toBe("125 000");
    expect(formatMoney("125000.50", "UZS")).toBe("125 000");
  });

  it("keeps a non-zero fraction for a non-UZS currency", () => {
    expect(formatMoney("125000.00", "USD")).toBe("125 000");
    expect(formatMoney("125000.50", "USD")).toBe("125 000.50");
  });

  it("keeps a negative sign only when the magnitude is non-zero", () => {
    expect(formatMoney("-500.00", "UZS")).toBe("-500");
    expect(formatMoney("-0.00", "UZS")).toBe("0");
  });
});

describe("formatMoneyWithCurrency", () => {
  it("appends the shop's currency, never hard-coded", () => {
    expect(formatMoneyWithCurrency("1200000.00", "UZS")).toBe("1 200 000 UZS");
    expect(formatMoneyWithCurrency("1200000.00", "USD")).toBe("1 200 000 USD");
  });
});

describe("displayPrice", () => {
  it("returns only the current price when no promo is active", () => {
    expect(
      displayPrice({ regular: "100000.00", current: "100000.00", promoActive: false }, "UZS"),
    ).toEqual({ current: "100 000 UZS", strikethrough: null });
  });

  it("returns the current (promo) price plus the struck-through regular price when promoActive", () => {
    expect(
      displayPrice({ regular: "150000.00", current: "120000.00", promoActive: true }, "UZS"),
    ).toEqual({ current: "120 000 UZS", strikethrough: "150 000 UZS" });
  });
});
