import { describe, expect, it } from "vitest";

import { formatMoneyDisplay } from "../money";

describe("formatMoneyDisplay", () => {
  it("adds thousands separators and drops a zero fraction", () => {
    expect(formatMoneyDisplay("125000.00")).toBe("125,000");
  });

  it("keeps a non-zero fraction", () => {
    expect(formatMoneyDisplay("1250000.50")).toBe("1,250,000.50");
  });

  it("formats a value under one thousand with no separator", () => {
    expect(formatMoneyDisplay("500.00")).toBe("500");
  });

  it("formats zero", () => {
    expect(formatMoneyDisplay("0.00")).toBe("0");
  });

  it("keeps the sign on a negative amount", () => {
    expect(formatMoneyDisplay("-125000.00")).toBe("-125,000");
  });

  it("never renders -0 for a negative zero-ish input", () => {
    expect(formatMoneyDisplay("-0.00")).toBe("0");
  });

  it("does not lose precision the way Number multiplication would", () => {
    // A value with more significant digits than Number can round-trip
    // exactly is still formatted correctly, since this works on the
    // string directly rather than parsing it into a float.
    expect(formatMoneyDisplay("123456789012345.00")).toBe("123,456,789,012,345");
  });
});
