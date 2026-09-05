import { describe, expect, it } from "vitest";

import { formatQty } from "./qty";

describe("formatQty", () => {
  it("trims trailing zeros for display", () => {
    expect(formatQty("3.000")).toBe("3");
    expect(formatQty("1.500")).toBe("1.5");
  });

  it("falls back to 0 when the value is missing", () => {
    expect(formatQty(undefined)).toBe("0");
  });

  it("passes through a whole number unchanged", () => {
    expect(formatQty("42")).toBe("42");
  });

  it("returns the original string for a non-numeric value", () => {
    expect(formatQty("not-a-number")).toBe("not-a-number");
  });
});
