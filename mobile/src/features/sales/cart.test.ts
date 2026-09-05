import { describe, expect, it } from "vitest";

import {
  type CartState,
  cartReducer,
  initialCartState,
  isValidDiscountValue,
  isZeroDecimalString,
} from "./cart";

function addA(state: CartState, qty?: number): CartState {
  return cartReducer(state, { type: "addItem", variantId: "a", label: "Variant A", qty });
}

function addB(state: CartState, qty?: number): CartState {
  return cartReducer(state, { type: "addItem", variantId: "b", label: "Variant B", qty });
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
  it("adds a new line with qty 1 by default", () => {
    const state = addA(initialCartState());
    expect(state.lines).toEqual([{ variantId: "a", label: "Variant A", qty: 1 }]);
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
    expect(state.lines).toEqual([{ variantId: "b", label: "Variant B", qty: 1 }]);
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
    ["fixed", "0", true],
    ["fixed", "150000", true],
    ["fixed", "150000.50", true],
    ["fixed", "-1", false],
    ["fixed", "1e5", false],
  ] as const)("%s %s -> %s", (kind, value, expected) => {
    expect(isValidDiscountValue(kind, value)).toBe(expected);
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
