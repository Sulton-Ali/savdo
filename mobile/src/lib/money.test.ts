import { describe, expect, it } from "vitest";

import { formatMoney } from "./money";

describe("formatMoney", () => {
  it("adds thousands separators and drops a zero fraction for UZS", () => {
    expect(formatMoney("125000.00", "UZS")).toBe("125 000");
  });

  it("drops a non-zero fraction for UZS (no everyday subunit, D-04)", () => {
    expect(formatMoney("1250000.50", "UZS")).toBe("1 250 000");
  });

  it("keeps a non-zero fraction for a currency other than UZS", () => {
    expect(formatMoney("1250000.50", "USD")).toBe("1 250 000.50");
  });

  it("drops a zero fraction for a currency other than UZS too", () => {
    expect(formatMoney("125000.00", "USD")).toBe("125 000");
  });

  it("formats a value under one thousand with no separator", () => {
    expect(formatMoney("500.00", "UZS")).toBe("500");
  });

  it("formats zero", () => {
    expect(formatMoney("0.00", "UZS")).toBe("0");
  });

  it("keeps the sign on a negative amount", () => {
    expect(formatMoney("-125000.00", "UZS")).toBe("-125 000");
  });

  it("never renders -0 for a negative zero-ish input", () => {
    expect(formatMoney("-0.00", "UZS")).toBe("0");
  });

  it("keeps the sign on a negative amount with a kept fraction", () => {
    expect(formatMoney("-1250000.50", "USD")).toBe("-1 250 000.50");
  });

  it("does not lose precision the way Number multiplication would", () => {
    // A value with more significant digits than Number can round-trip
    // exactly is still formatted correctly, since this works on the string
    // directly rather than parsing it into a float.
    expect(formatMoney("123456789012345.00", "UZS")).toBe("123 456 789 012 345");
  });

  it("strips a leading-zero integer part", () => {
    expect(formatMoney("0500.00", "UZS")).toBe("500");
  });
});
