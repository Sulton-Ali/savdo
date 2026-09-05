import { describe, expect, it } from "vitest";

import {
  type CartState,
  cartReducer,
  estimateCartTotals,
  idempotencyOutcome,
  initialCartState,
  isValidDiscountValue,
  isZeroDecimalString,
  multiplyMoneyByQty,
  percentOfMoney,
  qtyExceedsAvailable,
  subtractMoney,
  sumMoney,
} from "./cart";

function addA(state: CartState, qty?: number): CartState {
  return cartReducer(state, {
    type: "addItem",
    variantId: "a",
    label: "Variant A",
    productName: "Product A",
    unitPrice: "10000.00",
    availableQty: "5.000",
    qty,
  });
}

function addB(state: CartState, qty?: number): CartState {
  return cartReducer(state, {
    type: "addItem",
    variantId: "b",
    label: "Variant B",
    productName: "Product B",
    unitPrice: "25000.00",
    availableQty: "2.000",
    qty,
  });
}

describe("initialCartState", () => {
  it("starts empty with no discount and a fresh idempotency key", () => {
    const state = initialCartState();
    expect(state.lines).toEqual([]);
    expect(state.discount).toBeNull();
    expect(state.idempotencyKey).toEqual(expect.any(String));
    expect(state.idempotencyKey.length).toBeGreaterThan(0);
  });

  it("mints a different key on every call", () => {
    const a = initialCartState();
    const b = initialCartState();
    expect(a.idempotencyKey).not.toBe(b.idempotencyKey);
  });
});

describe("addItem", () => {
  it("adds a new line with qty 1 by default, carrying product/price/availability", () => {
    const state = addA(initialCartState());
    expect(state.lines).toEqual([
      {
        variantId: "a",
        label: "Variant A",
        productName: "Product A",
        unitPrice: "10000.00",
        availableQty: "5.000",
        qty: 1,
      },
    ]);
  });

  it("adds a new line with the given qty", () => {
    const state = addA(initialCartState(), 3);
    expect(state.lines[0]?.qty).toBe(3);
  });

  it("increments an existing line's qty instead of duplicating it", () => {
    let state = addA(initialCartState());
    state = addA(state, 2);
    expect(state.lines).toHaveLength(1);
    expect(state.lines[0]?.qty).toBe(3);
  });

  it("keeps two different variants as separate lines", () => {
    let state = addA(initialCartState());
    state = addB(state);
    expect(state.lines).toHaveLength(2);
  });

  it("ignores a non-positive qty", () => {
    const state = addA(initialCartState(), 0);
    expect(state.lines).toEqual([]);
  });

  it("mints a fresh idempotency key when filling an empty cart", () => {
    const before = initialCartState();
    const after = addA(before);
    expect(after.idempotencyKey).not.toBe(before.idempotencyKey);
  });

  it("keeps the same idempotency key across further edits", () => {
    const first = addA(initialCartState());
    const second = addB(first, 2);
    const third = addA(second, 1);
    expect(second.idempotencyKey).toBe(first.idempotencyKey);
    expect(third.idempotencyKey).toBe(first.idempotencyKey);
  });
});

describe("incrementQty / decrementQty / setQty", () => {
  it("incrementQty adds one to the line", () => {
    const state = cartReducer(addA(initialCartState()), { type: "incrementQty", variantId: "a" });
    expect(state.lines[0]?.qty).toBe(2);
  });

  it("decrementQty removes one from the line", () => {
    const state = cartReducer(addA(initialCartState(), 2), {
      type: "decrementQty",
      variantId: "a",
    });
    expect(state.lines[0]?.qty).toBe(1);
  });

  it("decrementQty from 1 removes the line entirely", () => {
    const state = cartReducer(addA(initialCartState()), { type: "decrementQty", variantId: "a" });
    expect(state.lines).toEqual([]);
  });

  it("decrementQty on a missing variant is a no-op", () => {
    const before = addA(initialCartState());
    const after = cartReducer(before, { type: "decrementQty", variantId: "missing" });
    expect(after.lines).toEqual(before.lines);
  });

  it("setQty replaces the line's qty", () => {
    const state = cartReducer(addA(initialCartState()), { type: "setQty", variantId: "a", qty: 5 });
    expect(state.lines[0]?.qty).toBe(5);
  });

  it("setQty to 0 removes the line", () => {
    const state = cartReducer(addA(initialCartState()), { type: "setQty", variantId: "a", qty: 0 });
    expect(state.lines).toEqual([]);
  });

  it("incrementQty/decrementQty never change the idempotency key", () => {
    const before = addA(initialCartState());
    const incremented = cartReducer(before, { type: "incrementQty", variantId: "a" });
    const decremented = cartReducer(incremented, { type: "decrementQty", variantId: "a" });
    expect(incremented.idempotencyKey).toBe(before.idempotencyKey);
    expect(decremented.idempotencyKey).toBe(before.idempotencyKey);
  });
});

describe("removeItem", () => {
  it("removes the given line only", () => {
    let state = addA(initialCartState());
    state = addB(state);
    state = cartReducer(state, { type: "removeItem", variantId: "a" });
    expect(state.lines.map((line) => line.variantId)).toEqual(["b"]);
  });
});

describe("isValidDiscountValue", () => {
  it.each([
    ["percent", "0", true],
    ["percent", "12.5", true],
    ["percent", "100", true],
    ["percent", "100.01", false],
    ["percent", "101", false],
    ["percent", "-1", false],
    ["percent", "abc", false],
    ["percent", "", false],
    // At most 2 fractional digits, matching the server's own discount
    // `value` validation (T4 review) — a 3rd decimal digit must fail here
    // so the screen shows its own field error instead of a round trip.
    ["percent", "12.345", false],
    ["fixed", "0", true],
    ["fixed", "150000", true],
    ["fixed", "150000.50", true],
    ["fixed", "150000.555", false],
    ["fixed", "-1", false],
    ["fixed", "1e5", false],
  ] as const)("%s %s -> %s", (kind, value, expected) => {
    expect(isValidDiscountValue(kind, value)).toBe(expected);
  });
});

describe("idempotencyOutcome", () => {
  it("rekeys only for IDEMPOTENCY_KEY_REUSED", () => {
    expect(idempotencyOutcome("IDEMPOTENCY_KEY_REUSED")).toBe("rekey");
  });

  it("keeps the key for an undecoded error (network drop/timeout — code undefined)", () => {
    expect(idempotencyOutcome(undefined)).toBe("keepKey");
  });

  it("keeps the key for any other decoded error code", () => {
    expect(idempotencyOutcome("STOCK_INSUFFICIENT")).toBe("keepKey");
    expect(idempotencyOutcome("DISCOUNT_EXCEEDS_SUBTOTAL")).toBe("keepKey");
    expect(idempotencyOutcome("VALIDATION_FAILED")).toBe("keepKey");
    expect(idempotencyOutcome("INTERNAL")).toBe("keepKey");
  });
});

describe("isZeroDecimalString", () => {
  it.each([
    ["0", true],
    ["0.0", true],
    ["00.000", true],
    ["0.01", false],
    ["1", false],
    ["", false],
  ] as const)("%s -> %s", (value, expected) => {
    expect(isZeroDecimalString(value)).toBe(expected);
  });
});

describe("setDiscount", () => {
  it("stores a valid discount", () => {
    const state = cartReducer(initialCartState(), {
      type: "setDiscount",
      discount: { kind: "percent", value: "10", reason: "" },
    });
    expect(state.discount).toEqual({ kind: "percent", value: "10", reason: "" });
  });

  it("stores a discount exactly as given, even mid-typing/invalid", () => {
    // The screen binds its discount value field directly to
    // `state.discount.value` (`sale/index.tsx`) so every keystroke is
    // visible while typing, e.g. a trailing "12." on the way to "12.5" —
    // `isValidDiscountValue` is what a caller uses to gate on validity,
    // not the reducer itself.
    const state = cartReducer(initialCartState(), {
      type: "setDiscount",
      discount: { kind: "percent", value: "150", reason: "" },
    });
    expect(state.discount).toEqual({ kind: "percent", value: "150", reason: "" });
    expect(isValidDiscountValue("percent", state.discount?.value ?? "")).toBe(false);
  });

  it("clears the discount with null", () => {
    const withDiscount = cartReducer(initialCartState(), {
      type: "setDiscount",
      discount: { kind: "fixed", value: "5000", reason: "loyal customer" },
    });
    const cleared = cartReducer(withDiscount, { type: "setDiscount", discount: null });
    expect(cleared.discount).toBeNull();
  });

  it("does not change the idempotency key", () => {
    const before = addA(initialCartState());
    const after = cartReducer(before, {
      type: "setDiscount",
      discount: { kind: "percent", value: "10", reason: "" },
    });
    expect(after.idempotencyKey).toBe(before.idempotencyKey);
  });
});

describe("clear / completed", () => {
  it("clear empties the cart and mints a fresh idempotency key", () => {
    const before = addA(initialCartState(), 2);
    const after = cartReducer(before, { type: "clear" });
    expect(after.lines).toEqual([]);
    expect(after.discount).toBeNull();
    expect(after.idempotencyKey).not.toBe(before.idempotencyKey);
  });

  it("completed empties the cart and mints a fresh idempotency key", () => {
    const before = addA(initialCartState(), 2);
    const after = cartReducer(before, { type: "completed" });
    expect(after.lines).toEqual([]);
    expect(after.discount).toBeNull();
    expect(after.idempotencyKey).not.toBe(before.idempotencyKey);
  });
});

describe("rekey", () => {
  it("mints a fresh idempotency key without touching lines or discount", () => {
    let before = addA(initialCartState(), 2);
    before = cartReducer(before, {
      type: "setDiscount",
      discount: { kind: "percent", value: "10", reason: "loyal" },
    });
    const after = cartReducer(before, { type: "rekey" });
    expect(after.idempotencyKey).not.toBe(before.idempotencyKey);
    expect(after.lines).toEqual(before.lines);
    expect(after.discount).toEqual(before.discount);
  });
});

describe("money helpers", () => {
  it("multiplyMoneyByQty is exact for whole-unit quantities", () => {
    expect(multiplyMoneyByQty("199000.00", 2)).toBe("398000.00");
    expect(multiplyMoneyByQty("10000.50", 3)).toBe("30001.50");
    expect(multiplyMoneyByQty("100.00", 0)).toBe("0.00");
  });

  it("sumMoney adds a list, and sums to 0.00 for an empty list", () => {
    expect(sumMoney(["100.00", "50.25", "0.75"])).toBe("151.00");
    expect(sumMoney([])).toBe("0.00");
  });

  it("subtractMoney subtracts, allowing a negative result", () => {
    expect(subtractMoney("100.00", "40.00")).toBe("60.00");
    expect(subtractMoney("40.00", "100.00")).toBe("-60.00");
  });

  it("percentOfMoney rounds half-up to 2 places", () => {
    expect(percentOfMoney("100.00", "10")).toBe("10.00");
    // 33.335 rounds up, not banker's-rounds to even.
    expect(percentOfMoney("333.35", "10")).toBe("33.34");
    expect(percentOfMoney("398000.00", "10")).toBe("39800.00");
  });

  it("qtyExceedsAvailable compares a whole-unit qty against a decimal availableQty", () => {
    expect(qtyExceedsAvailable(2, "5.000")).toBe(false);
    expect(qtyExceedsAvailable(5, "5.000")).toBe(false);
    expect(qtyExceedsAvailable(6, "5.000")).toBe(true);
    expect(qtyExceedsAvailable(1, "0.500")).toBe(true);
  });
});

describe("estimateCartTotals", () => {
  it("sums line totals with no discount", () => {
    let state = addA(initialCartState(), 2); // 10000.00 x 2 = 20000.00
    state = addB(state, 1); // 25000.00 x 1 = 25000.00
    expect(estimateCartTotals(state.lines, state.discount)).toEqual({
      subtotal: "45000.00",
      discountAmount: "0.00",
      total: "45000.00",
    });
  });

  it("applies a percent discount", () => {
    const state = addA(initialCartState(), 2); // subtotal 20000.00
    const estimate = estimateCartTotals(state.lines, {
      kind: "percent",
      value: "10",
      reason: "",
    });
    expect(estimate).toEqual({
      subtotal: "20000.00",
      discountAmount: "2000.00",
      total: "18000.00",
    });
  });

  it("applies a fixed discount", () => {
    const state = addA(initialCartState(), 2); // subtotal 20000.00
    const estimate = estimateCartTotals(state.lines, {
      kind: "fixed",
      value: "5000",
      reason: "",
    });
    expect(estimate).toEqual({
      subtotal: "20000.00",
      discountAmount: "5000.00",
      total: "15000.00",
    });
  });

  it("clamps the total at zero when a fixed discount exceeds the subtotal", () => {
    const state = addA(initialCartState(), 1); // subtotal 10000.00
    const estimate = estimateCartTotals(state.lines, {
      kind: "fixed",
      value: "50000",
      reason: "",
    });
    expect(estimate.total).toBe("0.00");
  });

  it("ignores an invalid or zero discount", () => {
    const state = addA(initialCartState(), 1);
    const invalid = estimateCartTotals(state.lines, { kind: "percent", value: "150", reason: "" });
    const zero = estimateCartTotals(state.lines, { kind: "fixed", value: "0", reason: "" });
    const emptyValue = estimateCartTotals(state.lines, { kind: "percent", value: "", reason: "" });
    expect(invalid.discountAmount).toBe("0.00");
    expect(zero.discountAmount).toBe("0.00");
    expect(emptyValue.discountAmount).toBe("0.00");
  });

  it("is 0.00/0.00/0.00 for an empty cart", () => {
    expect(estimateCartTotals([], null)).toEqual({
      subtotal: "0.00",
      discountAmount: "0.00",
      total: "0.00",
    });
  });
});
