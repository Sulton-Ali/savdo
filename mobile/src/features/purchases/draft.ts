import type { PurchaseCreate, PurchaseItemCreate } from "./api";

/**
 * Pure, RN-free "new purchase" draft state (D-85) — the whole "create, then
 * receive in the same flow" screen (`app/(app)/stock/purchases/new.tsx`)
 * wires this reducer to `VariantPicker` and a couple of plain `TextInput`s;
 * everything about *what's valid to submit* and *what the wire payload
 * looks like* lives here so it's testable with plain Vitest.
 *
 * Only one `Idempotency-Key` exists in this flow, for `receivePurchase`
 * (`receiveIdempotencyKey` below) — `createPurchase` has no
 * `Idempotency-Key` parameter in the contract at all (`features/purchases/api.ts`'s
 * `createPurchase` doc), so there's nothing to key there; a duplicate tap on
 * "Save and receive" is guarded by the screen disabling the button while a
 * mutation is in flight, not by a second key. Once `createPurchase`
 * succeeds, `createdPurchaseId` is set and every retry of "Save and
 * receive" (e.g. the first `receivePurchase` call failed on a network blip)
 * calls `receivePurchase` again against that same id and the same
 * `receiveIdempotencyKey` — never `createPurchase` a second time. `reset`
 * is the only thing that starts a new draft with a fresh key, e.g. after a
 * successful receive, before composing another purchase on the same screen
 * instance.
 */

export interface DraftLine {
  variantId: string;
  /** Display-only, resolved from the product/variant search when the line
   * was added — never sent to the API (`buildCreateBody` only reads
   * `variantId`/`qty`/`unitCost`). */
  productName: string;
  variantLabel: string;
  sku: string | null;
  /** Raw decimal strings (ADR-007) — ideally already normalised by
   * `normalizeQty`/`normalizeUnitCost` before landing in a line, but kept as
   * plain strings here so an in-progress edit (e.g. a trailing ".") doesn't
   * get silently rewritten while the user is still typing. */
  qty: string;
  unitCost: string;
}

export interface PurchaseDraftState {
  supplierId: string | null;
  locationId: string | null;
  supplierInvoiceNo: string;
  note: string;
  lines: DraftLine[];
  /** Set once `createPurchase` succeeds; `null` while still composing. */
  createdPurchaseId: string | null;
  createdPurchaseNumber: string | null;
  /** Generated once per draft (`reset`'s `idempotencyKey` argument), reused
   * on every retry of the receive step for the same created purchase. */
  receiveIdempotencyKey: string;
}

export type PurchaseDraftAction =
  | { type: "setSupplier"; supplierId: string | null }
  | { type: "setLocation"; locationId: string | null }
  | { type: "setSupplierInvoiceNo"; value: string }
  | { type: "setNote"; value: string }
  | { type: "addLine"; line: DraftLine }
  | { type: "updateLine"; variantId: string; patch: Partial<Pick<DraftLine, "qty" | "unitCost">> }
  | { type: "removeLine"; variantId: string }
  | { type: "purchaseCreated"; id: string; number: string }
  | { type: "reset"; idempotencyKey: string };

export function createInitialDraft(idempotencyKey: string): PurchaseDraftState {
  return {
    supplierId: null,
    locationId: null,
    supplierInvoiceNo: "",
    note: "",
    lines: [],
    createdPurchaseId: null,
    createdPurchaseNumber: null,
    receiveIdempotencyKey: idempotencyKey,
  };
}

export function purchaseDraftReducer(
  state: PurchaseDraftState,
  action: PurchaseDraftAction,
): PurchaseDraftState {
  switch (action.type) {
    case "setSupplier":
      return { ...state, supplierId: action.supplierId };
    case "setLocation":
      return { ...state, locationId: action.locationId };
    case "setSupplierInvoiceNo":
      return { ...state, supplierInvoiceNo: action.value };
    case "setNote":
      return { ...state, note: action.value };
    case "addLine": {
      // Re-picking a variant already in the draft replaces that line
      // (matches `admin/src/routes/app/PurchaseFormPage.tsx`'s
      // `handleAddItem`) rather than adding a second row for it.
      const existingIndex = state.lines.findIndex(
        (line) => line.variantId === action.line.variantId,
      );
      if (existingIndex >= 0) {
        const lines = [...state.lines];
        lines[existingIndex] = action.line;
        return { ...state, lines };
      }
      return { ...state, lines: [...state.lines, action.line] };
    }
    case "updateLine":
      return {
        ...state,
        lines: state.lines.map((line) =>
          line.variantId === action.variantId ? { ...line, ...action.patch } : line,
        ),
      };
    case "removeLine":
      return { ...state, lines: state.lines.filter((line) => line.variantId !== action.variantId) };
    case "purchaseCreated":
      return { ...state, createdPurchaseId: action.id, createdPurchaseNumber: action.number };
    case "reset":
      return createInitialDraft(action.idempotencyKey);
    default:
      return state;
  }
}

/** `quantity NUMERIC(12,3)` (`docs/04-DATA-MODEL.md` § 8). A purchase line's
 * quantity must be strictly positive — unlike a stock adjustment's signed
 * quantity (`features/stock/adjustmentForm.ts`), there's no such thing as
 * receiving a negative or zero amount of an item. Returns `null` for
 * anything else, including empty input. */
export function normalizeQty(raw: string): string | null {
  const trimmed = raw.trim();
  if (!trimmed) {
    return null;
  }
  let unsigned = trimmed;
  if (unsigned.startsWith(".")) {
    unsigned = `0${unsigned}`;
  }
  if (unsigned.endsWith(".")) {
    unsigned = unsigned.slice(0, -1);
  }
  if (!/^\d+(\.\d+)?$/.test(unsigned)) {
    return null;
  }
  const [, fracPart = ""] = unsigned.split(".");
  if (fracPart.length > 3) {
    return null;
  }
  return Number(unsigned) > 0 ? unsigned : null;
}

/** Money is `NUMERIC(14,2)` (ADR-007) — a purchase line's unit cost may be
 * zero (e.g. a free promotional item) but never negative, and carries at
 * most 2 decimal places. Returns `null` for anything else, including empty
 * input. */
export function normalizeUnitCost(raw: string): string | null {
  const trimmed = raw.trim();
  if (!trimmed) {
    return null;
  }
  let unsigned = trimmed;
  if (unsigned.startsWith(".")) {
    unsigned = `0${unsigned}`;
  }
  if (unsigned.endsWith(".")) {
    unsigned = unsigned.slice(0, -1);
  }
  if (!/^\d+(\.\d+)?$/.test(unsigned)) {
    return null;
  }
  const [, fracPart = ""] = unsigned.split(".");
  if (fracPart.length > 2) {
    return null;
  }
  return unsigned;
}

export interface PurchaseDraftFieldErrors {
  supplierId?: string;
  locationId?: string;
  lines?: string;
}

/** Validates the composable part of the draft (before `createPurchase` is
 * ever called) — translation-key errors per field, empty object = valid.
 * Per-line qty/unit-cost validity is enforced at the point a line is
 * added/edited (`normalizeQty`/`normalizeUnitCost` return `null` for
 * anything the UI must refuse to add), so a line already in `state.lines`
 * is assumed valid here. */
export function validatePurchaseDraft(state: PurchaseDraftState): PurchaseDraftFieldErrors {
  const errors: PurchaseDraftFieldErrors = {};
  if (!state.supplierId) {
    errors.supplierId = "errors.field.required";
  }
  if (!state.locationId) {
    errors.locationId = "errors.field.required";
  }
  if (state.lines.length === 0) {
    errors.lines = "purchases.form.items.required";
  }
  return errors;
}

export function isPurchaseDraftValid(state: PurchaseDraftState): boolean {
  return Object.keys(validatePurchaseDraft(state)).length === 0;
}

/**
 * Builds the exact `POST /purchases` payload — only `variantId`/`qty`/
 * `unitCost` per line (never the display-only `productName`/`variantLabel`/
 * `sku`), and `supplierInvoiceNo`/`note` omitted entirely when blank rather
 * than sent as `""` (matches `admin/src/routes/app/PurchaseFormPage.tsx`'s
 * `handleFinish`). Returns `null` when the draft fails
 * `validatePurchaseDraft`, so a caller can't accidentally send a request
 * the contract would reject with `400 VALIDATION_FAILED` for an empty
 * `items` array or a missing `supplierId`/`locationId` — hard rule 8: never
 * trust client input into a request without validating it first.
 */
export function buildCreateBody(state: PurchaseDraftState): PurchaseCreate | null {
  if (!isPurchaseDraftValid(state) || !state.supplierId || !state.locationId) {
    return null;
  }
  const items: PurchaseItemCreate[] = state.lines.map((line) => ({
    variantId: line.variantId,
    qty: line.qty,
    unitCost: line.unitCost,
  }));
  const trimmedInvoiceNo = state.supplierInvoiceNo.trim();
  const trimmedNote = state.note.trim();
  return {
    supplierId: state.supplierId,
    locationId: state.locationId,
    ...(trimmedInvoiceNo ? { supplierInvoiceNo: trimmedInvoiceNo } : {}),
    ...(trimmedNote ? { note: trimmedNote } : {}),
    items,
  };
}
