/**
 * Pure cart state for the one-handed quick-sale screen
 * (`app/(app)/sale/index.tsx`) — no React, no React Native import, so it is
 * testable under plain Vitest with no Expo/RN mocking (this task's brief
 * cites this as "D-85"; `docs/00-DECISIONS.md` does not have that row yet —
 * flagged in this task's report against `D-73`, which says mobile has no
 * unit test runner at all, so the two need reconciling in the docs).
 *
 * A cart line carries no price and no product name: the shared
 * `features/catalog/VariantPicker.tsx` (T2) reports back only the picked
 * `Variant` and its available qty at the chosen location, never the
 * `Product` it belongs to — and `Variant` itself has no `productId`/name
 * field, so there is no way for this caller to resolve one without
 * fetching every product's variants. This is a real gap between that
 * component's shape and this cart's needs (also flagged in the report); it
 * is not fixed here since `VariantPicker.tsx` is outside this task's file
 * scope (a T2 file). The line's `label` is built from the variant's own
 * SKU/attributes instead (`sale/index.tsx`'s `variantLabel`), the same
 * fallback shape `admin/src/routes/app/QuickSalePage.tsx`'s own
 * `variantLabel` uses.
 *
 * `POST /sales` only ever carries `variantId`/`qty` — never a price (D-56,
 * hard rule 8) — so this cart never needed one for the request either; the
 * server computes and returns every total. No money value is computed or
 * displayed anywhere in this module for the same reason (deliberately
 * simpler than `admin`'s quick-sale cart, which does keep a decimal-safe
 * preview total — this module has no product/price data to preview from at
 * all, see above).
 */

export type DiscountKind = "percent" | "fixed";

export interface CartDiscount {
  kind: DiscountKind;
  /** Decimal string (ADR-007): 0-100 for `percent`, a non-negative money
   * amount for `fixed` — the server caps a `fixed` discount at the
   * computed subtotal itself (D-57), so this module only checks the
   * format and the `percent` upper bound. */
  value: string;
  /** Sent as `SaleCreate.discountReason` only when both this and `value`
   * are non-empty (a reason with no discount is meaningless and must
   * never reach the server, mirroring `admin/src/routes/app/QuickSalePage.tsx`). */
  reason: string;
}

export interface CartLine {
  variantId: string;
  label: string;
  /** Whole units only — the same simplifying assumption
   * `admin/src/routes/app/QuickSalePage.tsx`'s `formatSaleQty` makes (a
   * family clothing shop's till always sells whole units); revisit if a
   * fractional-unit product (`Unit.decimalPlaces > 0`) ever needs a till
   * sale. Always >= 1 — a line whose qty would drop to 0 is removed
   * instead (`cartReducer`'s `decrementQty`/`setQty`). */
  qty: number;
}

export interface CartState {
  lines: CartLine[];
  discount: CartDiscount | null;
  /**
   * `Idempotency-Key` sent with `POST /sales` (docs/05-API.md §
   * Conventions). Created once, the moment the cart goes from empty to
   * non-empty, and kept stable across every further edit — adding or
   * removing a line, changing qty, changing the discount — so a genuine
   * double-tap of "Pay" always replays the exact same request instead of
   * starting a second one. Reset only on `clear` and `completed`.
   *
   * Safe to keep stable across an edit made *after* a failed submit too
   * (e.g. fixing a qty once the server answers `409 STOCK_INSUFFICIENT`):
   * the server's `Idempotent` helper
   * (`api/internal/httpx/idempotency.go`) only stores a key once its
   * request has *succeeded* — a failed attempt rolls back its whole
   * transaction and stores nothing — so the same key retried with a
   * different, corrected body is simply treated as unused, not rejected
   * as `IDEMPOTENCY_KEY_REUSED`.
   */
  idempotencyKey: string;
}

export type CartAction =
  | { type: "addItem"; variantId: string; label: string; qty?: number }
  | { type: "incrementQty"; variantId: string }
  | { type: "decrementQty"; variantId: string }
  | { type: "setQty"; variantId: string; qty: number }
  | { type: "removeItem"; variantId: string }
  | { type: "setDiscount"; discount: CartDiscount | null }
  | { type: "clear" }
  | { type: "completed" };

/**
 * Generates an idempotency key with no dependency on a runtime global this
 * plain-TS module cannot assume exists everywhere it might run (Hermes'
 * `crypto.randomUUID` support was not asserted against any pinned source,
 * so this deliberately avoids relying on it — good enough here since an
 * `Idempotency-Key` only needs to be practically unique per device per
 * cart, not cryptographically random).
 */
function generateIdempotencyKey(): string {
  const randomSegment = () => Math.random().toString(36).slice(2, 10);
  return `${Date.now().toString(36)}-${randomSegment()}-${randomSegment()}`;
}

export function initialCartState(): CartState {
  return { lines: [], discount: null, idempotencyKey: generateIdempotencyKey() };
}

/** Decimal-string compare scale for the `percent` upper bound check below —
 * generous enough for any percent a person would type (ADR-007: never
 * `Number`/float on a value that represents money or a money-like
 * percentage). */
const COMPARE_SCALE = 6;

function toScaledInt(value: string, scale: number): bigint {
  const [intPartRaw, fracPartRaw = ""] = value.trim().split(".");
  const intPart = intPartRaw || "0";
  const fracPart = (fracPartRaw + "0".repeat(scale)).slice(0, scale);
  return BigInt(intPart + fracPart);
}

/** `true` when non-negative decimal string `value` is greater than `limit`. */
function exceeds(value: string, limit: string): boolean {
  return toScaledInt(value, COMPARE_SCALE) > toScaledInt(limit, COMPARE_SCALE);
}

const DECIMAL_STRING_RE = /^\d+(\.\d+)?$/;
const MAX_DISCOUNT_PERCENT = "100";

/** A discount `value` is valid when it is a non-negative decimal string
 * and, for `percent`, does not exceed 100 — the server still validates
 * and caps everything authoritatively (D-57, hard rule 8); this is only
 * for a screen to reject an obviously-bad value before it ever tries a
 * request. */
export function isValidDiscountValue(kind: DiscountKind, value: string): boolean {
  const trimmed = value.trim();
  if (!DECIMAL_STRING_RE.test(trimmed)) {
    return false;
  }
  return kind === "fixed" || !exceeds(trimmed, MAX_DISCOUNT_PERCENT);
}

/** `true` for "0", "0.0", "00.000", … — a screen uses this to treat a
 * discount whose value is exactly zero the same as no discount at all
 * (mirrors `admin/src/routes/app/QuickSalePage.tsx`'s `discountValue > 0`
 * gate), without ever parsing the decimal string as a `Number`. */
export function isZeroDecimalString(value: string): boolean {
  return /^0+(\.0+)?$/.test(value.trim());
}

function updateLines(state: CartState, lines: CartLine[]): CartState {
  return { ...state, lines };
}

export function cartReducer(state: CartState, action: CartAction): CartState {
  switch (action.type) {
    case "addItem": {
      const addedQty = action.qty ?? 1;
      if (addedQty <= 0) {
        return state;
      }
      const existing = state.lines.find((line) => line.variantId === action.variantId);
      const lines = existing
        ? state.lines.map((line) =>
            line.variantId === action.variantId ? { ...line, qty: line.qty + addedQty } : line,
          )
        : [...state.lines, { variantId: action.variantId, label: action.label, qty: addedQty }];
      const wasEmpty = state.lines.length === 0;
      return {
        ...updateLines(state, lines),
        // A key minted while the cart was empty was never used against a
        // request with a body attached (there is nothing to submit yet),
        // so only a fill (empty -> non-empty) needs a fresh one; every
        // later edit keeps it (see `idempotencyKey`'s own doc comment).
        idempotencyKey: wasEmpty ? generateIdempotencyKey() : state.idempotencyKey,
      };
    }
    case "incrementQty":
      return updateLines(
        state,
        state.lines.map((line) =>
          line.variantId === action.variantId ? { ...line, qty: line.qty + 1 } : line,
        ),
      );
    case "decrementQty":
      return updateLines(
        state,
        state.lines
          .map((line) =>
            line.variantId === action.variantId ? { ...line, qty: line.qty - 1 } : line,
          )
          .filter((line) => line.qty > 0),
      );
    case "setQty":
      if (action.qty <= 0) {
        return updateLines(
          state,
          state.lines.filter((line) => line.variantId !== action.variantId),
        );
      }
      return updateLines(
        state,
        state.lines.map((line) =>
          line.variantId === action.variantId ? { ...line, qty: action.qty } : line,
        ),
      );
    case "removeItem":
      return updateLines(
        state,
        state.lines.filter((line) => line.variantId !== action.variantId),
      );
    case "setDiscount":
      // Stored as given, even mid-typing/invalid (e.g. a trailing "12.") —
      // a screen binds its discount value field directly to
      // `state.discount.value` so every keystroke is visible, and calls
      // `isValidDiscountValue` itself to show an inline error and to
      // decide whether to include `discount` in `SaleCreate` at all; the
      // server is the actual authority regardless (D-57, hard rule 8).
      return { ...state, discount: action.discount };
    case "clear":
    case "completed":
      return initialCartState();
    default:
      return state;
  }
}
