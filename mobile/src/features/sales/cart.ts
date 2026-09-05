/**
 * Pure cart state for the one-handed quick-sale screen
 * (`app/(app)/sale/index.tsx`) — no React, no React Native import, so it is
 * testable under plain Vitest with no Expo/RN mocking (D-85 amends D-73 to
 * allow this for pure, RN-free modules; `mobile/vitest.config.mts`'s
 * `include` covers this file under `src/features` alongside D-85's own
 * `src/lib` examples).
 *
 * A cart line's `productName`/`unitPrice` are a *preview* only, resolved
 * once when the line is added from the `Variant` and its parent `Product`
 * the shared `features/catalog/VariantPicker.tsx` now reports back
 * (T4 review: its `onPick` gained an additive third `product` argument for
 * exactly this). `POST /sales` still only ever carries `variantId`/`qty` —
 * never a price (D-56, hard rule 8) — so none of this module's money math
 * is sent anywhere; `estimateCartTotals` below is for display only,
 * labelled as an estimate in the UI (`mobile.sale.estimate`), and the
 * server's own response/`GET /sales/{id}` is what a confirmation screen
 * shows. `availableQty` is kept per line only to warn when "+" would take
 * a line above the stock last seen at pick time (`qtyExceedsAvailable`) —
 * the server still re-checks authoritatively and can still answer `409
 * STOCK_INSUFFICIENT` regardless (stock can move between pick and pay).
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
  /** Resolved once, when the line is added, from the picked `Variant`'s
   * parent `Product` — a preview only (see this module's own doc comment). */
  productName: string;
  /** `features/catalog/pricing.ts`'s `resolveEffectivePrice` at pick time —
   * a preview only; the server resolves it again, authoritatively, at
   * payment time (D-67/D-68, hard rule 8). */
  unitPrice: string;
  /** The stock quantity the `VariantPicker` showed for this variant at
   * pick time — a snapshot, not re-checked as the cart is edited; used only
   * by `qtyExceedsAvailable` to warn in the UI, never to block a qty change
   * (the server is still the only authority, hard rule 8). */
  availableQty: string;
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
   * starting a second one. Reset on `clear` and `completed`; also minted
   * fresh on demand by `rekey`, but *only* for `409 IDEMPOTENCY_KEY_REUSED`
   * (see `idempotencyOutcome` below) — kept as a distinct action from
   * `clear` since it leaves `lines`/`discount` untouched.
   *
   * Safe to keep stable across an edit made *after* a failed submit too
   * (e.g. fixing a qty once the server answers `409 STOCK_INSUFFICIENT`):
   * the server's `Idempotent` helper
   * (`api/internal/httpx/idempotency.go`) only stores a key once its
   * request has *succeeded* — a failed attempt rolls back its whole
   * transaction and stores nothing — so the same key retried with a
   * different, corrected body is simply treated as unused, not rejected
   * as `IDEMPOTENCY_KEY_REUSED`.
   *
   * Critically, this also means a request whose *response was never seen*
   * at all (a network drop, a timeout) must keep the same key rather than
   * mint a new one (T4 review CRITICAL, corrects an earlier version of
   * this file that rekeyed on any undecoded error): if that request had
   * actually reached the server and committed, a cashier's natural retry
   * of the same cart under a *new* key would create a second, real sale
   * with its own stock movements — one that can never be undone from the
   * device (ADR-014, sales are immutable). Keeping the key means a retry
   * of the unchanged body either replays the first attempt's own stored
   * success (`Idempotent` returns the same response, no new movements) or
   * is genuinely the first attempt to reach the server at all — never a
   * duplicate.
   */
  idempotencyKey: string;
}

export type CartAction =
  | {
      type: "addItem";
      variantId: string;
      label: string;
      productName: string;
      unitPrice: string;
      availableQty: string;
      qty?: number;
    }
  | { type: "incrementQty"; variantId: string }
  | { type: "decrementQty"; variantId: string }
  | { type: "setQty"; variantId: string; qty: number }
  | { type: "removeItem"; variantId: string }
  | { type: "setDiscount"; discount: CartDiscount | null }
  | { type: "clear" }
  | { type: "completed" }
  /** Mints a fresh `idempotencyKey` without touching `lines`/`discount` —
   * unlike `clear`/`completed`. Dispatched *only* when `idempotencyOutcome`
   * below says `"rekey"`, i.e. only for `409 IDEMPOTENCY_KEY_REUSED`: the
   * one case where the current key is *provably* already spent on an
   * earlier, successful attempt (this reducer's own request never reused a
   * key against a different body on purpose), so this attempt's edited
   * body needs a key of its own. `sale/index.tsx` pairs this with a hint
   * pointing at today's sales list, so the cashier can check whether that
   * earlier attempt is the one they meant before paying again. Every other
   * failure — including one with no response at all — keeps the key
   * instead (`idempotencyKey`'s own doc comment has the full reasoning). */
  | { type: "rekey" };

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

export type IdempotencyOutcome = "rekey" | "keepKey";

/**
 * What `sale/index.tsx` should do with the cart's `idempotencyKey` after a
 * failed `POST /sales`, given the error's machine-readable `code` — or
 * `undefined` when the error was never a decoded server response at all
 * (a network drop, a timeout: `SalesApiError` is only ever constructed
 * from a response the client actually received and parsed).
 *
 * `"rekey"` only for `IDEMPOTENCY_KEY_REUSED`: the server's own
 * `Idempotent` helper (`api/internal/httpx/idempotency.go`) answers that
 * code exactly when this key was already used for a *different* body —
 * which, since a failed attempt never stores a key at all, can only mean
 * an *earlier* attempt with this key already succeeded. Every other
 * outcome is `"keepKey"`, including `undefined`: a decoded failure
 * (`STOCK_INSUFFICIENT`, `VALIDATION_FAILED`, …) never stored a key
 * either, so the same key is safe to retry once the cart is fixed; and an
 * undecoded failure's outcome is *unknown* — the request may have reached
 * the server and committed anyway — so rekeying there would risk a
 * cashier's natural retry becoming a second, real sale under a fresh key
 * instead of safely replaying the first attempt's own result (T4 review
 * CRITICAL; `idempotencyKey`'s own doc comment on `CartState` has the
 * full reasoning). A pure function so this is pinned in Vitest without
 * needing to construct a real `SalesApiError`.
 */
export function idempotencyOutcome(errorCode: string | undefined): IdempotencyOutcome {
  return errorCode === "IDEMPOTENCY_KEY_REUSED" ? "rekey" : "keepKey";
}

/** Decimal-string fixed-point scale for money (ADR-007: `NUMERIC(14,2)`)
 * and for a discount `percent` value — never `Number`/float on either
 * (hard rule 4). Shared by the discount-percent bound check below and by
 * `estimateCartTotals`'s money arithmetic. */
const MONEY_SCALE = 2;
const PERCENT_SCALE = 4;
const QTY_SCALE = 3;

function toScaledInt(value: string, scale: number): bigint {
  const [intPartRaw, fracPartRaw = ""] = value.trim().split(".");
  const intPart = intPartRaw || "0";
  const fracPart = (fracPartRaw + "0".repeat(scale)).slice(0, scale);
  return BigInt(intPart + fracPart);
}

/** Inverse of `toScaledInt` — formats a scaled `BigInt` back to a decimal
 * string, e.g. `fromScaledInt(12345n, 2)` -> `"123.45"`. */
function fromScaledInt(value: bigint, scale: number): string {
  const negative = value < 0n;
  const magnitude = negative ? -value : value;
  const digits = magnitude.toString().padStart(scale + 1, "0");
  const intPart = digits.slice(0, digits.length - scale);
  const fracPart = digits.slice(digits.length - scale);
  const sign = negative && magnitude !== 0n ? "-" : "";
  return scale > 0 ? `${sign}${intPart}.${fracPart}` : `${sign}${intPart}`;
}

/** Rounds `numerator / divisor` half-up, away from zero, both `BigInt` —
 * only `percentOfMoney` below needs this (an integer money x integer qty
 * multiplication, `multiplyMoneyByQty`, is always exact). */
function roundDiv(numerator: bigint, divisor: bigint): bigint {
  if (numerator >= 0n) {
    return (numerator + divisor / 2n) / divisor;
  }
  return -((-numerator + divisor / 2n) / divisor);
}

/** `true` when non-negative decimal string `value` is greater than `limit`
 * — compared at `PERCENT_SCALE`, generous enough for any percent a person
 * would type. */
function exceeds(value: string, limit: string): boolean {
  return toScaledInt(value, PERCENT_SCALE) > toScaledInt(limit, PERCENT_SCALE);
}

// At most 2 fractional digits — the server's own discount `value`
// validation is `^\d+(\.\d{1,2})?$` (T4 review), stricter than the
// contract's general `Decimal` pattern; matching it here means a value
// with a 3rd decimal digit shows this screen's own inline field error
// instead of a round trip that bounces back as a generic
// `VALIDATION_FAILED` banner.
const DECIMAL_STRING_RE = /^\d+(\.\d{1,2})?$/;
const MAX_DISCOUNT_PERCENT = "100";

/** A discount `value` is valid when it is a non-negative decimal string
 * with at most 2 fractional digits, and, for `percent`, does not exceed
 * 100 — the server still validates and caps everything authoritatively
 * (D-57, hard rule 8); this is only for a screen to reject an
 * obviously-bad value before it ever tries a request. */
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

/** `unitPrice` (money, 2-place decimal string) x `qty` (a whole-unit
 * integer count) -> a money decimal string. Exact — no rounding needed,
 * unlike `percentOfMoney` below. */
export function multiplyMoneyByQty(unitPrice: string, qty: number): string {
  return fromScaledInt(toScaledInt(unitPrice, MONEY_SCALE) * BigInt(qty), MONEY_SCALE);
}

/** Sums a list of money decimal strings; `[]` sums to `"0.00"`. */
export function sumMoney(values: string[]): string {
  const total = values.reduce((acc, value) => acc + toScaledInt(value, MONEY_SCALE), 0n);
  return fromScaledInt(total, MONEY_SCALE);
}

export function subtractMoney(a: string, b: string): string {
  return fromScaledInt(toScaledInt(a, MONEY_SCALE) - toScaledInt(b, MONEY_SCALE), MONEY_SCALE);
}

/** Clamps a money decimal string at zero (never negative) — the estimate's
 * `total` uses this the same way `admin/src/routes/app/QuickSalePage.tsx`'s
 * preview does: the server rejects a discount over the subtotal rather
 * than silently clamping it (D-57), this only keeps the *preview* from
 * showing a negative total while the user is mid-edit. */
export function clampMoneyAtZero(value: string): string {
  return toScaledInt(value, MONEY_SCALE) < 0n ? "0.00" : value;
}

/** `percent` (0..100, decimal string, e.g. "12.5") of a money `subtotal`,
 * decimal-safe, rounded half-up to 2 places. */
export function percentOfMoney(subtotal: string, percent: string): string {
  const subtotalUnits = toScaledInt(subtotal, MONEY_SCALE);
  const percentUnits = toScaledInt(percent, PERCENT_SCALE);
  const divisor = 10n ** BigInt(PERCENT_SCALE) * 100n;
  return fromScaledInt(roundDiv(subtotalUnits * percentUnits, divisor), MONEY_SCALE);
}

/** `true` when a whole-unit `qty` is more than the decimal-string
 * `availableQty` snapshot a cart line carries — decimal-safe (`available`
 * can have up to 3 places, matching `docs/05-API.md`'s stock `"2.000"`
 * convention), used only for a UI warning (this module's own doc comment). */
export function qtyExceedsAvailable(qty: number, availableQty: string): boolean {
  const qtyUnits = BigInt(qty) * 10n ** BigInt(QTY_SCALE);
  return qtyUnits > toScaledInt(availableQty, QTY_SCALE);
}

export interface CartEstimate {
  subtotal: string;
  /** `"0.00"` when there is no active discount. */
  discountAmount: string;
  total: string;
}

/** A decimal-safe *preview* of what the server will charge, from the
 * catalogue prices resolved onto each line at pick time — never sent to
 * the server and never authoritative (hard rule 8; this module's own doc
 * comment). Mirrors `admin/src/routes/app/quick-sale/decimal.ts`'s
 * `multiplyMoneyByQty`/`sumMoney`/`percentOfMoney`/`subtractMoney` shape. */
export function estimateCartTotals(lines: CartLine[], discount: CartDiscount | null): CartEstimate {
  const subtotal = sumMoney(lines.map((line) => multiplyMoneyByQty(line.unitPrice, line.qty)));
  const hasDiscount =
    discount != null &&
    isValidDiscountValue(discount.kind, discount.value) &&
    !isZeroDecimalString(discount.value);
  const discountAmount = hasDiscount
    ? discount.kind === "percent"
      ? percentOfMoney(subtotal, discount.value.trim())
      : // Re-scaled to a 2-place money string (a "fixed" value like "5000"
        // is otherwise returned as-is, inconsistent with every other money
        // string this module produces).
        fromScaledInt(toScaledInt(discount.value.trim(), MONEY_SCALE), MONEY_SCALE)
    : "0.00";
  return {
    subtotal,
    discountAmount,
    total: clampMoneyAtZero(subtractMoney(subtotal, discountAmount)),
  };
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
        : [
            ...state.lines,
            {
              variantId: action.variantId,
              label: action.label,
              productName: action.productName,
              unitPrice: action.unitPrice,
              availableQty: action.availableQty,
              qty: addedQty,
            },
          ];
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
    case "rekey":
      return { ...state, idempotencyKey: generateIdempotencyKey() };
    default:
      return state;
  }
}
