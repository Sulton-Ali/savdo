import { describe, expect, it } from "vitest";

import {
  buildCreateBody,
  createInitialDraft,
  type DraftLine,
  isPurchaseDraftValid,
  normalizeQty,
  normalizeUnitCost,
  purchaseDraftReducer,
  validatePurchaseDraft,
} from "./draft";

const LINE_A: DraftLine = {
  variantId: "v1",
  productName: "Shirt",
  variantLabel: "L / Blue",
  sku: "SKU-1",
  qty: "10",
  unitCost: "15000",
};

const LINE_B: DraftLine = {
  variantId: "v2",
  productName: "Shirt",
  variantLabel: "M / Red",
  sku: "SKU-2",
  qty: "5",
  unitCost: "15000",
};

describe("createInitialDraft", () => {
  it("starts empty with the given idempotency key", () => {
    const draft = createInitialDraft("key-1");
    expect(draft).toEqual({
      supplierId: null,
      locationId: null,
      supplierInvoiceNo: "",
      note: "",
      lines: [],
      createdPurchaseId: null,
      createdPurchaseNumber: null,
      receiveIdempotencyKey: "key-1",
    });
  });
});

describe("purchaseDraftReducer", () => {
  it("sets supplier and location", () => {
    let state = createInitialDraft("key-1");
    state = purchaseDraftReducer(state, { type: "setSupplier", supplierId: "s1" });
    state = purchaseDraftReducer(state, { type: "setLocation", locationId: "l1" });
    expect(state.supplierId).toBe("s1");
    expect(state.locationId).toBe("l1");
  });

  it("sets supplierInvoiceNo and note", () => {
    let state = createInitialDraft("key-1");
    state = purchaseDraftReducer(state, { type: "setSupplierInvoiceNo", value: "INV-1" });
    state = purchaseDraftReducer(state, { type: "setNote", value: "hello" });
    expect(state.supplierInvoiceNo).toBe("INV-1");
    expect(state.note).toBe("hello");
  });

  it("adds a line", () => {
    const state = purchaseDraftReducer(createInitialDraft("key-1"), {
      type: "addLine",
      line: LINE_A,
    });
    expect(state.lines).toEqual([LINE_A]);
  });

  it("adding a line for an already-present variant replaces it, not appends", () => {
    let state = purchaseDraftReducer(createInitialDraft("key-1"), {
      type: "addLine",
      line: LINE_A,
    });
    const replacement: DraftLine = { ...LINE_A, qty: "20" };
    state = purchaseDraftReducer(state, { type: "addLine", line: replacement });
    expect(state.lines).toEqual([replacement]);
  });

  it("adds a second, distinct line separately", () => {
    let state = purchaseDraftReducer(createInitialDraft("key-1"), {
      type: "addLine",
      line: LINE_A,
    });
    state = purchaseDraftReducer(state, { type: "addLine", line: LINE_B });
    expect(state.lines).toEqual([LINE_A, LINE_B]);
  });

  it("updates a line's qty/unitCost by variantId", () => {
    let state = purchaseDraftReducer(createInitialDraft("key-1"), {
      type: "addLine",
      line: LINE_A,
    });
    state = purchaseDraftReducer(state, {
      type: "updateLine",
      variantId: "v1",
      patch: { qty: "12" },
    });
    expect(state.lines[0]?.qty).toBe("12");
    expect(state.lines[0]?.unitCost).toBe("15000");
  });

  it("removes a line by variantId", () => {
    let state = purchaseDraftReducer(createInitialDraft("key-1"), {
      type: "addLine",
      line: LINE_A,
    });
    state = purchaseDraftReducer(state, { type: "addLine", line: LINE_B });
    state = purchaseDraftReducer(state, { type: "removeLine", variantId: "v1" });
    expect(state.lines).toEqual([LINE_B]);
  });

  it("purchaseCreated records the id and number without touching the rest", () => {
    const state = purchaseDraftReducer(createInitialDraft("key-1"), {
      type: "purchaseCreated",
      id: "p1",
      number: "P-000001",
    });
    expect(state.createdPurchaseId).toBe("p1");
    expect(state.createdPurchaseNumber).toBe("P-000001");
    expect(state.receiveIdempotencyKey).toBe("key-1");
  });

  it("reset starts a fresh draft with a new idempotency key", () => {
    let state = purchaseDraftReducer(createInitialDraft("key-1"), {
      type: "addLine",
      line: LINE_A,
    });
    state = purchaseDraftReducer(state, {
      type: "purchaseCreated",
      id: "p1",
      number: "P-000001",
    });
    state = purchaseDraftReducer(state, { type: "reset", idempotencyKey: "key-2" });
    expect(state).toEqual(createInitialDraft("key-2"));
  });
});

describe("normalizeQty (purchase line, positive only)", () => {
  it("accepts a positive integer or decimal", () => {
    expect(normalizeQty("10")).toBe("10");
    expect(normalizeQty("2.5")).toBe("2.5");
  });

  it("rejects zero", () => {
    expect(normalizeQty("0")).toBeNull();
  });

  it("rejects a negative quantity", () => {
    expect(normalizeQty("-1")).toBeNull();
  });

  it("prefixes a leading dot with 0", () => {
    expect(normalizeQty(".5")).toBe("0.5");
  });

  it("rejects more than 3 decimal places", () => {
    expect(normalizeQty("1.2345")).toBeNull();
  });

  it("rejects empty input", () => {
    expect(normalizeQty("")).toBeNull();
    expect(normalizeQty("   ")).toBeNull();
  });
});

describe("normalizeUnitCost (money, non-negative, 2 decimals)", () => {
  it("accepts zero (a free item)", () => {
    expect(normalizeUnitCost("0")).toBe("0");
  });

  it("accepts a positive amount", () => {
    expect(normalizeUnitCost("15000")).toBe("15000");
    expect(normalizeUnitCost("15000.50")).toBe("15000.50");
  });

  it("rejects a negative amount", () => {
    expect(normalizeUnitCost("-5")).toBeNull();
  });

  it("rejects more than 2 decimal places", () => {
    expect(normalizeUnitCost("15000.999")).toBeNull();
  });

  it("rejects empty input", () => {
    expect(normalizeUnitCost("")).toBeNull();
  });
});

describe("validatePurchaseDraft / isPurchaseDraftValid", () => {
  it("requires a supplier, a location and at least one line", () => {
    const errors = validatePurchaseDraft(createInitialDraft("key-1"));
    expect(errors.supplierId).toBe("errors.field.required");
    expect(errors.locationId).toBe("errors.field.required");
    expect(errors.lines).toBe("purchases.form.items.required");
  });

  it("is valid once supplier, location and a line are all set", () => {
    let state = createInitialDraft("key-1");
    state = purchaseDraftReducer(state, { type: "setSupplier", supplierId: "s1" });
    state = purchaseDraftReducer(state, { type: "setLocation", locationId: "l1" });
    state = purchaseDraftReducer(state, { type: "addLine", line: LINE_A });
    expect(isPurchaseDraftValid(state)).toBe(true);
  });
});

describe("buildCreateBody", () => {
  function validDraft() {
    let state = createInitialDraft("key-1");
    state = purchaseDraftReducer(state, { type: "setSupplier", supplierId: "s1" });
    state = purchaseDraftReducer(state, { type: "setLocation", locationId: "l1" });
    state = purchaseDraftReducer(state, { type: "addLine", line: LINE_A });
    return state;
  }

  it("returns null for an invalid draft", () => {
    expect(buildCreateBody(createInitialDraft("key-1"))).toBeNull();
  });

  it("builds only variantId/qty/unitCost per line — never the display fields", () => {
    const body = buildCreateBody(validDraft());
    expect(body).toEqual({
      supplierId: "s1",
      locationId: "l1",
      items: [{ variantId: "v1", qty: "10", unitCost: "15000" }],
    });
  });

  it("omits supplierInvoiceNo/note when blank", () => {
    const body = buildCreateBody(validDraft());
    expect(body).not.toHaveProperty("supplierInvoiceNo");
    expect(body).not.toHaveProperty("note");
  });

  it("includes trimmed supplierInvoiceNo/note when present", () => {
    let state = validDraft();
    state = purchaseDraftReducer(state, { type: "setSupplierInvoiceNo", value: "  INV-1  " });
    state = purchaseDraftReducer(state, { type: "setNote", value: "  hello  " });
    const body = buildCreateBody(state);
    expect(body?.supplierInvoiceNo).toBe("INV-1");
    expect(body?.note).toBe("hello");
  });

  it("never sends a client-computed total (hard rule 8)", () => {
    const body = buildCreateBody(validDraft());
    expect(body).not.toHaveProperty("totalCost");
  });
});
