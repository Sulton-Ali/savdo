import { describe, expect, it } from "vitest";

import { displayPrice, formatMoneyDisplay } from "./money";

describe("formatMoneyDisplay", () => {
  it("adds thousands separators", () => {
    expect(formatMoneyDisplay("125000.00")).toBe("125,000");
    expect(formatMoneyDisplay("1000000.00")).toBe("1,000,000");
  });

  it("drops a trailing .00 but keeps a non-zero fraction", () => {
    expect(formatMoneyDisplay("125000.00")).toBe("125,000");
    expect(formatMoneyDisplay("125000.50")).toBe("125,000.50");
  });

  it("keeps a negative sign only when the magnitude is non-zero", () => {
    expect(formatMoneyDisplay("-500.00")).toBe("-500");
    expect(formatMoneyDisplay("-0.00")).toBe("0");
  });
});

describe("displayPrice", () => {
  it("returns only the current price when no promo is active", () => {
    expect(
      displayPrice({ regular: "100000.00", current: "100000.00", promoActive: false }),
    ).toEqual({ current: "100,000", strikethrough: null });
  });

  it("returns the current (promo) price plus the struck-through regular price when promoActive", () => {
    expect(displayPrice({ regular: "150000.00", current: "120000.00", promoActive: true })).toEqual(
      { current: "120,000", strikethrough: "150,000" },
    );
  });
});
