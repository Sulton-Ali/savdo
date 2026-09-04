import { describe, expect, it } from "vitest";

import { formatQty, sumQty } from "../api";

describe("sumQty", () => {
  it("sums with fixed-point precision, immune to float drift", () => {
    // Number(0.1) + Number(0.2) + Number(0.3) is 0.6000000000000001 in
    // IEEE 754 float arithmetic — this must land exactly on "0.600".
    expect(sumQty(["0.1", "0.2", "0.3"])).toBe("0.600");
  });

  it("sums whole numbers and decimals with different precisions", () => {
    expect(sumQty(["3", "2.5", "0.005"])).toBe("5.505");
  });

  it("sums negative and positive quantities", () => {
    expect(sumQty(["10.000", "-3.250"])).toBe("6.750");
  });

  it("returns a zero total formatted to 3 decimals", () => {
    expect(sumQty(["5.000", "-5.000"])).toBe("0.000");
  });

  it("returns 0.000 for an empty list", () => {
    expect(sumQty([])).toBe("0.000");
  });
});

describe("formatQty", () => {
  it("trims trailing zeros for display", () => {
    expect(formatQty("3.000")).toBe("3");
    expect(formatQty("1.500")).toBe("1.5");
  });

  it("falls back to 0 when the value is missing", () => {
    expect(formatQty(undefined)).toBe("0");
  });
});
