import { describe, expect, it } from "vitest";

import {
  addMoney,
  clampMoneyAtZero,
  isMoneyGreaterThan,
  multiplyMoneyByQty,
  percentOfMoney,
  subtractMoney,
  sumMoney,
} from "../quick-sale/decimal";

describe("multiplyMoneyByQty", () => {
  it("multiplies a money decimal string by an integer qty exactly", () => {
    expect(multiplyMoneyByQty("125000.00", "3")).toBe("375000.00");
  });

  it("multiplies without the float rounding error Number arithmetic has", () => {
    // 0.1 + 0.2 !== 0.3 in float; 0.10 * 3 also drifts (0.30000000000000004).
    // The decimal-safe path must not.
    expect(multiplyMoneyByQty("0.10", "3")).toBe("0.30");
  });

  it("rounds a fractional qty half-up to 2 decimal places", () => {
    expect(multiplyMoneyByQty("10000.00", "1.5")).toBe("15000.00");
  });
});

describe("addMoney / subtractMoney / sumMoney", () => {
  it("adds two money strings", () => {
    expect(addMoney("100.50", "50.25")).toBe("150.75");
  });

  it("subtracts two money strings, allowing a negative result", () => {
    expect(subtractMoney("100.00", "150.00")).toBe("-50.00");
  });

  it("sums a list of money strings, defaulting to 0.00 for an empty cart", () => {
    expect(sumMoney([])).toBe("0.00");
    expect(sumMoney(["10.00", "20.50", "0.25"])).toBe("30.75");
  });
});

describe("isMoneyGreaterThan / clampMoneyAtZero", () => {
  it("compares two money strings", () => {
    expect(isMoneyGreaterThan("100.01", "100.00")).toBe(true);
    expect(isMoneyGreaterThan("100.00", "100.00")).toBe(false);
    expect(isMoneyGreaterThan("99.99", "100.00")).toBe(false);
  });

  it("clamps a negative amount at zero", () => {
    expect(clampMoneyAtZero("-10.00")).toBe("0.00");
    expect(clampMoneyAtZero("10.00")).toBe("10.00");
  });
});

describe("percentOfMoney", () => {
  it("computes a whole percent", () => {
    expect(percentOfMoney("100000.00", "10")).toBe("10000.00");
  });

  it("computes a fractional percent, rounding half-up to 2 decimals", () => {
    expect(percentOfMoney("99.99", "12.5")).toBe("12.50");
  });

  it("returns 0.00 for a 0% discount", () => {
    expect(percentOfMoney("100000.00", "0")).toBe("0.00");
  });
});
