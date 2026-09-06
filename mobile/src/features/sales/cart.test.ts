import { describe, expect, it } from "vitest";

import {
  buildDraftCreateBody,
  buildDraftPatchBody,
  type CartState,
  cartDiscountFromDraft,
  cartLinesFromDraftItems,
  cartLinesToSaleItems,
  cartReducer,
  entityOf,
  estimateCartTotals,
  generateIdempotencyKey,
  idempotencyOutcome,
  initialCartState,
  isCartReadOnly,
  isValidDiscountValue,
  isZeroDecimalString,
  multiplyMoneyByQty,
  nextAfterDraftPayError,
  percentOfMoney,
  planDraftPay,
  qtyExceedsAvailable,
  resolveSubmitDiscount,
  resolveSubmitDiscountReason,
  subtractMoney,
  sumMoney,
} from "./cart";

function addA(
  state: CartState,
  qty?: number,
  idempotencyKey = generateIdempotencyKey(),
): CartState {
  return cartReducer(state, {
    type: "addItem",
    variantId: "a",
    label: "Variant A",
    productName: "Product A",
    unitPrice: "10000.00",
    availableQty: "5.000",
    qty,
    idempotencyKey,
  });
}

function addB(
  state: CartState,
  qty?: number,
  idempotencyKey = generateIdempotencyKey(),
): CartState {
  return cartReducer(state, {
    type: "addItem",
    variantId: "b",
    label: "Variant B",
    productName: "Product B",
    unitPrice: "25000.00",
    availableQty: "2.000",
    qty,
    idempotencyKey,
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
        available: true,
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

  it("adopts the action's own idempotencyKey when filling an empty cart — the reducer never generates one itself", () => {
    const before = initialCartState();
    const after = addA(before, undefined, "caller-minted-key");
    expect(after.idempotencyKey).toBe("caller-minted-key");
  });

  it("ignores the action's idempotencyKey when the cart was already non-empty", () => {
    const first = addA(initialCartState(), undefined, "first-key");
    const second = addB(first, 2, "second-key-should-be-ignored");
    expect(second.idempotencyKey).toBe("first-key");
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
  it("adopts the action's idempotencyKey without touching lines or discount", () => {
    let before = addA(initialCartState(), 2);
    before = cartReducer(before, {
      type: "setDiscount",
      discount: { kind: "percent", value: "10", reason: "loyal" },
    });
    const after = cartReducer(before, { type: "rekey", idempotencyKey: "rekeyed" });
    expect(after.idempotencyKey).toBe("rekeyed");
    expect(after.idempotencyKey).not.toBe(before.idempotencyKey);
    expect(after.lines).toEqual(before.lines);
    expect(after.discount).toEqual(before.discount);
  });
});

describe("generateIdempotencyKey", () => {
  it("mints a non-empty, practically-unique key on every call", () => {
    const a = generateIdempotencyKey();
    const b = generateIdempotencyKey();
    expect(a.length).toBeGreaterThan(0);
    expect(a).not.toBe(b);
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

describe("T14 draft integration", () => {
  it("initialCartState starts with an empty note, no draftId, clean and unattempted", () => {
    const state = initialCartState();
    expect(state.note).toBe("");
    expect(state.draftId).toBeNull();
    expect(state.dirty).toBe(false);
    expect(state.completionAttempted).toBe(false);
  });

  it("setNote updates the note, marks dirty, without touching anything else", () => {
    const state = addA(initialCartState(), 1);
    const next = cartReducer(state, { type: "setNote", note: "Deliver by 6pm" });
    expect(next.note).toBe("Deliver by 6pm");
    expect(next.lines).toEqual(state.lines);
    expect(next.idempotencyKey).toBe(state.idempotencyKey);
    expect(next.dirty).toBe(true);
  });

  it("addItem/incrementQty/decrementQty/setQty/removeItem/setDiscount all mark dirty", () => {
    const draftLine = {
      variantId: "v9",
      label: "Variant Z",
      productName: "Product Z",
      unitPrice: "7000.00",
      availableQty: "999999.000",
      qty: 2,
      available: true,
    };
    const clean = cartReducer(initialCartState(), {
      type: "loadDraft",
      draftId: "draft-1",
      lines: [draftLine],
      discount: null,
      note: "",
      idempotencyKey: "key-1",
    });
    expect(clean.dirty).toBe(false);

    expect(cartReducer(clean, { type: "incrementQty", variantId: "v9" }).dirty).toBe(true);
    expect(cartReducer(clean, { type: "decrementQty", variantId: "v9" }).dirty).toBe(true);
    expect(cartReducer(clean, { type: "setQty", variantId: "v9", qty: 5 }).dirty).toBe(true);
    expect(cartReducer(clean, { type: "removeItem", variantId: "v9" }).dirty).toBe(true);
    expect(
      cartReducer(clean, {
        type: "setDiscount",
        discount: { kind: "fixed", value: "1", reason: "" },
      }).dirty,
    ).toBe(true);
    expect(
      addA(clean, 1, "key-2").dirty, // addItem
    ).toBe(true);
  });

  it("markDirty sets dirty without touching anything else", () => {
    const state = initialCartState();
    const next = cartReducer(state, { type: "markDirty" });
    expect(next.dirty).toBe(true);
    expect(next.lines).toEqual(state.lines);
    expect(next.idempotencyKey).toBe(state.idempotencyKey);
  });

  it("completionAttempted sets the flag and is idempotent", () => {
    const state = initialCartState();
    const once = cartReducer(state, { type: "completionAttempted" });
    expect(once.completionAttempted).toBe(true);
    const twice = cartReducer(once, { type: "completionAttempted" });
    expect(twice).toBe(once); // same reference — a true-to-true dispatch is a no-op
  });

  it("loadDraft replaces the whole cart with the draft's own state, clean and unattempted", () => {
    const before = addA(initialCartState(), 3);
    const loaded = cartReducer(before, {
      type: "loadDraft",
      draftId: "draft-1",
      lines: [
        {
          variantId: "v9",
          label: "Variant Z",
          productName: "Product Z",
          unitPrice: "7000.00",
          availableQty: "999999.000",
          qty: 2,
          available: true,
        },
      ],
      discount: { kind: "fixed", value: "1000.00", reason: "loyalty" },
      note: "from draft",
      idempotencyKey: "key-from-draft",
    });
    expect(loaded).toEqual({
      lines: [
        {
          variantId: "v9",
          label: "Variant Z",
          productName: "Product Z",
          unitPrice: "7000.00",
          availableQty: "999999.000",
          qty: 2,
          available: true,
        },
      ],
      discount: { kind: "fixed", value: "1000.00", reason: "loyalty" },
      note: "from draft",
      draftId: "draft-1",
      dirty: false,
      completionAttempted: false,
      idempotencyKey: "key-from-draft",
    });
  });

  it("clear resets draftId/note/dirty/completionAttempted back to a fresh cart", () => {
    const loaded = cartReducer(
      cartReducer(initialCartState(), {
        type: "loadDraft",
        draftId: "draft-1",
        lines: [],
        discount: null,
        note: "from draft",
        idempotencyKey: "key-from-draft",
      }),
      { type: "completionAttempted" },
    );
    const cleared = cartReducer(loaded, { type: "clear" });
    expect(cleared.draftId).toBeNull();
    expect(cleared.note).toBe("");
    expect(cleared.dirty).toBe(false);
    expect(cleared.completionAttempted).toBe(false);
  });

  it("completed resets draftId/note/dirty/completionAttempted back to a fresh cart", () => {
    const loaded = cartReducer(
      cartReducer(initialCartState(), {
        type: "loadDraft",
        draftId: "draft-1",
        lines: [],
        discount: null,
        note: "from draft",
        idempotencyKey: "key-from-draft",
      }),
      { type: "completionAttempted" },
    );
    const completed = cartReducer(loaded, { type: "completed" });
    expect(completed.draftId).toBeNull();
    expect(completed.note).toBe("");
    expect(completed.dirty).toBe(false);
    expect(completed.completionAttempted).toBe(false);
  });

  it("unlinkDraft drops draftId and resets dirty/completionAttempted but keeps lines/discount/note", () => {
    const loaded = cartReducer(
      cartReducer(initialCartState(), {
        type: "loadDraft",
        draftId: "draft-1",
        lines: [
          {
            variantId: "v9",
            label: "Variant Z",
            productName: "Product Z",
            unitPrice: "7000.00",
            availableQty: "999999.000",
            qty: 2,
            available: true,
          },
        ],
        discount: { kind: "fixed", value: "500", reason: "" },
        note: "keep me",
        idempotencyKey: "key-from-draft",
      }),
      { type: "completionAttempted" },
    );
    const unlinked = cartReducer(loaded, { type: "unlinkDraft", idempotencyKey: "fresh-key" });
    expect(unlinked.draftId).toBeNull();
    expect(unlinked.dirty).toBe(false);
    expect(unlinked.completionAttempted).toBe(false);
    expect(unlinked.idempotencyKey).toBe("fresh-key");
    expect(unlinked.lines).toEqual(loaded.lines);
    expect(unlinked.discount).toEqual(loaded.discount);
    expect(unlinked.note).toBe("keep me");
  });
});

describe("cartLinesFromDraftItems / cartDiscountFromDraft", () => {
  it("maps a SaleDraftItem to a CartLine, truncating a fractional qty to a whole unit (never rounding up)", () => {
    const lines = cartLinesFromDraftItems([
      {
        variantId: "v1",
        productId: "p1",
        productName: "Shirt",
        variantLabel: "M / Blue",
        qty: "2.900",
        unitPrice: "15000.00",
        lineTotal: "39000.00",
        available: true,
      },
    ]);
    expect(lines).toEqual([
      {
        variantId: "v1",
        label: "M / Blue",
        productName: "Shirt",
        unitPrice: "15000.00",
        availableQty: "999999.000",
        qty: 2,
        available: true,
      },
    ]);
  });

  it("never truncates a line's qty down to 0", () => {
    const lines = cartLinesFromDraftItems([
      {
        variantId: "v1",
        productId: "p1",
        productName: "Shirt",
        variantLabel: "M / Blue",
        qty: "0.200",
        unitPrice: "0.00",
        lineTotal: "0.00",
        available: false,
      },
    ]);
    expect(lines[0]?.qty).toBe(1);
    expect(lines[0]?.available).toBe(false);
  });

  it("maps a null draft discount to null", () => {
    expect(cartDiscountFromDraft(null, null)).toBeNull();
  });

  it("maps a draft discount and its separate reason", () => {
    expect(cartDiscountFromDraft({ type: "percent", value: "15" }, "regular")).toEqual({
      kind: "percent",
      value: "15",
      reason: "regular",
    });
  });

  it("defaults the reason to an empty string when the draft has none", () => {
    expect(cartDiscountFromDraft({ type: "fixed", value: "2000" }, null)).toEqual({
      kind: "fixed",
      value: "2000",
      reason: "",
    });
  });
});

describe("resolveSubmitDiscount / resolveSubmitDiscountReason", () => {
  it("is undefined for no discount", () => {
    expect(resolveSubmitDiscount(null)).toBeUndefined();
    expect(resolveSubmitDiscountReason(null)).toBeUndefined();
  });

  it("is undefined for an invalid discount value", () => {
    const discount = { kind: "percent" as const, value: "150", reason: "" };
    expect(resolveSubmitDiscount(discount)).toBeUndefined();
  });

  it("is undefined for a zero discount value", () => {
    const discount = { kind: "fixed" as const, value: "0", reason: "" };
    expect(resolveSubmitDiscount(discount)).toBeUndefined();
  });

  it("resolves a real discount, trimmed", () => {
    const discount = { kind: "percent" as const, value: " 10 ", reason: "" };
    expect(resolveSubmitDiscount(discount)).toEqual({ type: "percent", value: "10" });
  });

  it("omits the reason when blank even for an active discount", () => {
    const discount = { kind: "percent" as const, value: "10", reason: "   " };
    expect(resolveSubmitDiscount(discount)).toEqual({ type: "percent", value: "10" });
    expect(resolveSubmitDiscountReason(discount)).toBeUndefined();
  });

  it("trims and returns a non-blank reason", () => {
    const discount = { kind: "percent" as const, value: "10", reason: " loyalty " };
    expect(resolveSubmitDiscountReason(discount)).toBe("loyalty");
  });
});

describe("cartLinesToSaleItems", () => {
  it("maps lines to variantId/qty only, never a price", () => {
    const state = addB(addA(initialCartState(), 2), 1);
    expect(cartLinesToSaleItems(state.lines)).toEqual([
      { variantId: "a", qty: "2" },
      { variantId: "b", qty: "1" },
    ]);
  });
});

describe("buildDraftCreateBody / buildDraftPatchBody", () => {
  it("builds a minimal create body with no customer/discount/note", () => {
    const state = addA(initialCartState(), 2);
    expect(buildDraftCreateBody(state, "loc-1", null)).toEqual({
      locationId: "loc-1",
      items: [{ variantId: "a", qty: "2" }],
    });
  });

  it("builds a full create body with customer, discount, reason and note", () => {
    let state = addA(initialCartState(), 2);
    state = cartReducer(state, {
      type: "setDiscount",
      discount: { kind: "percent", value: "10", reason: "loyalty" },
    });
    state = cartReducer(state, { type: "setNote", note: "  Gift wrap  " });
    expect(buildDraftCreateBody(state, "loc-1", "cust-1")).toEqual({
      locationId: "loc-1",
      items: [{ variantId: "a", qty: "2" }],
      customerId: "cust-1",
      discount: { type: "percent", value: "10" },
      discountReason: "loyalty",
      note: "Gift wrap",
    });
  });

  it("builds a patch body that explicitly clears customer/discount/note when absent", () => {
    const state = addA(initialCartState(), 1);
    expect(buildDraftPatchBody(state, "loc-1", null)).toEqual({
      locationId: "loc-1",
      items: [{ variantId: "a", qty: "1" }],
      customerId: null,
      discountType: null,
      discountValue: null,
      discountReason: null,
      note: null,
    });
  });

  it("builds a patch body carrying an active discount and note", () => {
    let state = addA(initialCartState(), 1);
    state = cartReducer(state, {
      type: "setDiscount",
      discount: { kind: "fixed", value: "1000", reason: "" },
    });
    state = cartReducer(state, { type: "setNote", note: "call before delivery" });
    expect(buildDraftPatchBody(state, "loc-1", "cust-2")).toEqual({
      locationId: "loc-1",
      items: [{ variantId: "a", qty: "1" }],
      customerId: "cust-2",
      discountType: "fixed",
      discountValue: "1000",
      discountReason: null,
      note: "call before delivery",
    });
  });
});

describe("planDraftPay", () => {
  it.each([
    [false, false, "complete"],
    [false, true, "complete"],
    [true, false, "patchThenComplete"],
    [true, true, "complete"],
  ] as const)("dirty=%s completionAttempted=%s -> %s", (dirty, completionAttempted, expected) => {
    expect(planDraftPay({ draftId: "draft-1", dirty, completionAttempted })).toBe(expected);
  });
});

describe("isCartReadOnly", () => {
  it("is false for a fresh cart", () => {
    expect(isCartReadOnly(initialCartState())).toBe(false);
  });

  it("is false for a dirty, not-yet-attempted draft", () => {
    const loaded = cartReducer(addA(initialCartState(), 1), {
      type: "loadDraft",
      draftId: "draft-1",
      lines: [],
      discount: null,
      note: "",
      idempotencyKey: "k",
    });
    expect(isCartReadOnly(loaded)).toBe(false);
  });

  it("is true the moment completionAttempted is set, even if the cart is edited again after", () => {
    const attempted = cartReducer(initialCartState(), { type: "completionAttempted" });
    expect(isCartReadOnly(attempted)).toBe(true);
    const editedAfter = cartReducer(attempted, { type: "markDirty" });
    expect(isCartReadOnly(editedAfter)).toBe(true);
  });

  it("is false again once the draft is unlinked (confirmed gone)", () => {
    const attempted = cartReducer(initialCartState(), { type: "completionAttempted" });
    const unlinked = cartReducer(attempted, { type: "unlinkDraft", idempotencyKey: "k2" });
    expect(isCartReadOnly(unlinked)).toBe(false);
  });
});

describe("entityOf", () => {
  it("recognizes 'draft'", () => {
    expect(entityOf({ entity: "draft" })).toBe("draft");
  });

  it("recognizes 'variant'", () => {
    expect(entityOf({ entity: "variant" })).toBe("variant");
  });

  it("recognizes 'location'", () => {
    expect(entityOf({ entity: "location" })).toBe("location");
  });

  it("recognizes 'customer'", () => {
    expect(entityOf({ entity: "customer" })).toBe("customer");
  });

  it("falls back to 'other' for an entity value this app has no specific handling for", () => {
    expect(entityOf({ entity: "sale" })).toBe("other");
  });

  it("falls back to 'other' for undefined details", () => {
    expect(entityOf(undefined)).toBe("other");
  });

  it("falls back to 'other' for details with no entity field", () => {
    expect(entityOf({ fields: { items: "invalid" } })).toBe("other");
  });
});

describe("nextAfterDraftPayError", () => {
  it.each([
    ["NOT_FOUND" as const, "variant" as const, "lineVariantGone"],
    ["NOT_FOUND" as const, "location" as const, "locationGone"],
    ["NOT_FOUND" as const, "customer" as const, "customerGone"],
    ["NOT_FOUND" as const, "draft" as const, "draftGone"],
    ["NOT_FOUND" as const, "other" as const, "draftGone"],
    ["FORBIDDEN" as const, "other" as const, "forbiddenToEditDraft"],
    [undefined, "other" as const, "patchNetworkSafe"],
    ["VALIDATION_FAILED" as const, "other" as const, "patchGenericError"],
    ["DISCOUNT_EXCEEDS_SUBTOTAL" as const, "other" as const, "patchGenericError"],
  ] as const)("patch leg: code=%s entity=%s -> %s", (code, entity, expected) => {
    expect(nextAfterDraftPayError({ leg: "patch", code, entity, isRetry: false })).toBe(expected);
  });

  it.each([
    ["NOT_FOUND" as const, "variant" as const, false, "lineVariantGone"],
    ["NOT_FOUND" as const, "location" as const, false, "locationGone"],
    ["NOT_FOUND" as const, "customer" as const, false, "customerGone"],
    ["NOT_FOUND" as const, "draft" as const, false, "draftGone"],
    ["NOT_FOUND" as const, "other" as const, false, "draftGone"],
    // A decoded 404 never replays, on either leg — `isRetry` is ignored
    // once `code` is `NOT_FOUND` (T14 fix round, Opus review MAJOR 2).
    ["NOT_FOUND" as const, "draft" as const, true, "draftGone"],
    [undefined, "other" as const, false, "retryCompleteSameKey"],
    [undefined, "other" as const, true, "completeNetworkAmbiguous"],
    ["STOCK_INSUFFICIENT" as const, "other" as const, false, "useIdempotencyOutcome"],
    ["VALIDATION_FAILED" as const, "other" as const, false, "useIdempotencyOutcome"],
    ["DISCOUNT_EXCEEDS_SUBTOTAL" as const, "other" as const, false, "useIdempotencyOutcome"],
    ["IDEMPOTENCY_KEY_REUSED" as const, "other" as const, false, "useIdempotencyOutcome"],
  ] as const)(
    "complete leg: code=%s entity=%s isRetry=%s -> %s",
    (code, entity, isRetry, expected) => {
      expect(nextAfterDraftPayError({ leg: "complete", code, entity, isRetry })).toBe(expected);
    },
  );
});
