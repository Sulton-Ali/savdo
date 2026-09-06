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
 *
 * T14 (D-87..D-90) additive: `CartState` also carries `note` and `draftId`
 * so the same cart can either build a `POST /sales` body (`Pay`, as
 * before) or a `SaleDraftCreate`/`SaleDraftPatch` body (`Save draft`) —
 * `draftId` is `null` while composing a brand-new sale/draft and set once
 * the cart was loaded from an existing draft (`loadDraft`, dispatched by
 * `sale/index.tsx` after `GET /sales/drafts/{id}` resolves), at which
 * point `sale/index.tsx`'s Save action switches from `createSaleDraft` to
 * `updateSaleDraft` and its Pay action `updateSaleDraft`s before
 * completing (this task's own brief). Only `variantId`/`qty` per line and
 * `note`/`discount` ever leave this module in a request body — never a
 * price (D-56/D-87, hard rule 8), matching the reasoning above.
 */

// Type-only import (ADR-002: no hand-declared request/response shapes) —
// erased at build time, so it adds nothing to this module's zero-RN-import
// contract (D-85) and doesn't affect Vitest.
import type { components } from "@savdo/api-client";

type SaleItemCreate = components["schemas"]["SaleItemCreate"];
type SaleDiscount = components["schemas"]["SaleDiscount"];
type SaleDraftItem = components["schemas"]["SaleDraftItem"];
type SaleDraftCreate = components["schemas"]["SaleDraftCreate"];
type SaleDraftPatch = components["schemas"]["SaleDraftPatch"];
type ErrorCode = components["schemas"]["ErrorCode"];

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
  /** `true` for a line added from `VariantPicker` (always a currently
   * active, resolvable variant) or a draft line whose `SaleDraftItem.
   * available` was `true` at load time; `false` only for a line loaded
   * from a draft whose variant/product had already gone inactive or
   * soft-deleted (`cartLinesFromDraftItems`, T14 fix round MAJOR 3) — the
   * screen tags such a line "unavailable" and blocks Save/Pay until it is
   * removed, since `PATCH`/`POST .../complete` would otherwise fail hard
   * on it server-side (`resolveSaleItems`'s own `404 NOT_FOUND
   * {entity:"variant"}`/`errDraftLineUnavailable`'s `422
   * VALIDATION_FAILED`). Never re-checked as the cart is edited, same
   * staleness caveat `availableQty` above already carries. */
  available: boolean;
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
   * The actual key generation happens outside this reducer: `Math.random`/
   * `Date.now` are non-deterministic, and a reducer that calls them itself
   * can no longer be trusted to return the same output for the same input
   * (React 19's Strict Mode intentionally double-invokes a reducer to catch
   * exactly this kind of impurity). `addItem` and `rekey` actions instead
   * carry an `idempotencyKey` the dispatching screen minted via this
   * module's own exported `generateIdempotencyKey`; the reducer only
   * *decides whether* to adopt it (`addItem`: only when the cart was empty;
   * `rekey`: always), never generates one itself.
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
  /** A small optional note (T14, mirrors `SaleCreate.note`/
   * `SaleDraftCreate.note`) — bound to a single `TextInput` on
   * `sale/index.tsx` and sent with either `Pay` or `Save draft`, trimmed
   * and omitted entirely when blank (`buildDraftCreateBody`/
   * `buildDraftPatchBody` below; `sale/index.tsx`'s own `handlePay` does
   * the same for `SaleCreate.note`). Reset on `clear`/`completed`, carried
   * over as-is by `loadDraft` from `SaleDraft.note`. */
  note: string;
  /** `null` for a sale/draft being composed from scratch; the loaded
   * draft's id once `loadDraft` has run (this module's own doc comment
   * above has the full reasoning). Reset to `null` on `clear`/`completed`
   * — paying or discarding a loaded draft always returns the screen to a
   * fresh, unlinked cart. */
  draftId: string | null;
  /** `false` immediately after `loadDraft` (the cart matches what the
   * server already has stored) or after a successful Save/Pay on this
   * draft; `true` from the moment any line/qty/discount/note changes, or
   * the screen dispatches `markDirty` for a change it owns itself
   * (attaching/detaching a customer, which lives outside this reducer —
   * `sale/index.tsx`'s own doc comment). Pay-on-a-loaded-draft
   * (`planDraftPay` below) only PATCHes when this is `true`: a clean
   * (`false`) draft is already correct server-side, so completing it
   * directly is both correct and strictly safer than a needless PATCH
   * (T14 fix round, Opus review CRITICAL). */
  dirty: boolean;
  /** `false` until the moment a `POST .../complete` request for this
   * loaded draft is actually sent (set by the screen dispatching
   * `completionAttempted`, *before* that request's response arrives) —
   * once `true`, it stays `true` for the rest of this draft's session
   * (until `loadDraft`, `clear` or `completed`) even if the cart is
   * edited further, so `planDraftPay` never again chooses to PATCH: a
   * complete attempt whose outcome is unknown (the response was lost)
   * might already have committed, and PATCHing over a since-completed
   * (and therefore already-deleted) draft — or racing a duplicate
   * complete under a fresh body — is exactly the risk this flag exists
   * to rule out. Every further Pay tap instead replays `complete` under
   * the *same* `idempotencyKey`, which the server answers with its
   * already-stored 201 if the first attempt did commit
   * (`nextAfterDraftPayError` below covers the case where it did not). */
  completionAttempted: boolean;
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
      /** Minted by the dispatching component via `generateIdempotencyKey`,
       * not by this reducer (see `CartState.idempotencyKey`'s doc comment).
       * Adopted only when this action takes the cart from empty to
       * non-empty; otherwise ignored, same as before. */
      idempotencyKey: string;
    }
  | { type: "incrementQty"; variantId: string }
  | { type: "decrementQty"; variantId: string }
  | { type: "setQty"; variantId: string; qty: number }
  | { type: "removeItem"; variantId: string }
  | { type: "setDiscount"; discount: CartDiscount | null }
  | { type: "setNote"; note: string }
  | { type: "clear" }
  | { type: "completed" }
  /** Replaces the whole cart with a draft's own state (T14) — dispatched
   * once by `sale/index.tsx` after `GET /sales/drafts/{id}` resolves (and
   * the shop's locations have loaded, so the screen can resolve
   * `draft.locationId` to a `Location` itself; that resolution lives on
   * the screen, not here, since this module knows nothing about
   * `Location`). `lines`/`discount` are built by this module's own
   * `cartLinesFromDraftItems`/`cartDiscountFromDraft` from the fetched
   * `SaleDraft`; `idempotencyKey` is minted by the dispatching screen
   * (same reasoning as `addItem`/`rekey` above) since a fresh edit of an
   * existing draft is its own attempt series, unrelated to whatever key
   * (if any) this cart held before. */
  | {
      type: "loadDraft";
      draftId: string;
      lines: CartLine[];
      discount: CartDiscount | null;
      note: string;
      idempotencyKey: string;
    }
  /** Adopts a fresh `idempotencyKey` (minted by the dispatching component,
   * same as `addItem`) without touching `lines`/`discount` — unlike
   * `clear`/`completed`. Dispatched *only* when `idempotencyOutcome` below
   * says `"rekey"`, i.e. only for `409 IDEMPOTENCY_KEY_REUSED`: the one
   * case where the current key is *provably* already spent on an earlier,
   * successful attempt (this reducer's own request never reused a key
   * against a different body on purpose), so this attempt's edited body
   * needs a key of its own. `sale/index.tsx` pairs this with a hint
   * pointing at today's sales list, so the cashier can check whether that
   * earlier attempt is the one they meant before paying again. Every other
   * failure — including one with no response at all — keeps the key
   * instead (`idempotencyKey`'s own doc comment has the full reasoning). */
  | { type: "rekey"; idempotencyKey: string }
  /** Marks the cart `dirty` for a change this reducer doesn't otherwise
   * see itself — today, only attaching/detaching a customer, which
   * `sale/index.tsx` keeps as its own local state rather than on
   * `CartState` (T14 fix round). Every action below that touches
   * `lines`/`discount`/`note` already sets `dirty: true` on its own. */
  | { type: "markDirty" }
  /** Set by the screen the moment it sends a `POST .../complete` for the
   * loaded draft, before that request's response arrives
   * (`CartState.completionAttempted`'s own doc comment has the full
   * reasoning). Idempotent — dispatching it again once already `true` is
   * a no-op. */
  | { type: "completionAttempted" }
  /** Detaches the cart from a draft that is now confirmed gone (`404
   * NOT_FOUND` on either leg of Pay, not naming an unavailable variant —
   * `nextAfterDraftPayError`'s `"draftGone"`) without discarding the
   * cashier's own lines/discount/note, so they may deliberately re-sell
   * the same cart as a brand-new sale/draft instead of retyping it
   * (T14 fix round MAJOR 2). Mints a fresh `idempotencyKey` (same
   * reasoning as `loadDraft`/`rekey` above) since this is the start of a
   * new attempt series against a different endpoint (`POST /sales` or
   * `POST /sales/drafts`, never the now-gone draft's own routes again). */
  | { type: "unlinkDraft"; idempotencyKey: string };

/**
 * Generates an idempotency key with no dependency on a runtime global this
 * plain-TS module cannot assume exists everywhere it might run (Hermes'
 * `crypto.randomUUID` support was not asserted against any pinned source,
 * so this deliberately avoids relying on it — good enough here since an
 * `Idempotency-Key` only needs to be practically unique per device per
 * cart, not cryptographically random). Exported so the dispatching
 * component (`sale/index.tsx`) can mint the key it hands to `addItem`/
 * `rekey` actions itself — see `CartState.idempotencyKey`'s doc comment for
 * why minting doesn't happen inside `cartReducer`.
 */
export function generateIdempotencyKey(): string {
  const randomSegment = () => Math.random().toString(36).slice(2, 10);
  return `${Date.now().toString(36)}-${randomSegment()}-${randomSegment()}`;
}

export function initialCartState(): CartState {
  return {
    lines: [],
    discount: null,
    note: "",
    draftId: null,
    dirty: false,
    completionAttempted: false,
    idempotencyKey: generateIdempotencyKey(),
  };
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
  return { ...state, lines, dirty: true };
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
              available: true,
            },
          ];
      const wasEmpty = state.lines.length === 0;
      return {
        ...updateLines(state, lines),
        // A key minted while the cart was empty was never used against a
        // request with a body attached (there is nothing to submit yet),
        // so only a fill (empty -> non-empty) adopts the action's freshly
        // minted key; every later edit keeps the existing one (see
        // `idempotencyKey`'s own doc comment).
        idempotencyKey: wasEmpty ? action.idempotencyKey : state.idempotencyKey,
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
      return { ...state, discount: action.discount, dirty: true };
    case "setNote":
      return { ...state, note: action.note, dirty: true };
    case "clear":
    case "completed":
      return initialCartState();
    case "rekey":
      return { ...state, idempotencyKey: action.idempotencyKey };
    case "loadDraft":
      return {
        lines: action.lines,
        discount: action.discount,
        note: action.note,
        draftId: action.draftId,
        dirty: false,
        completionAttempted: false,
        idempotencyKey: action.idempotencyKey,
      };
    case "markDirty":
      return { ...state, dirty: true };
    case "completionAttempted":
      return state.completionAttempted ? state : { ...state, completionAttempted: true };
    case "unlinkDraft":
      return {
        ...state,
        draftId: null,
        dirty: false,
        completionAttempted: false,
        idempotencyKey: action.idempotencyKey,
      };
    default:
      return state;
  }
}

/**
 * `SaleCreate.items`/`SaleDraftCreate.items`/`SaleDraftPatch.items` are all
 * the exact same shape (`SaleItemCreate[]`: `variantId`/`qty` only — never
 * a price, D-56/D-87) — this is the one place that maps a cart's lines to
 * it, used by `sale/index.tsx`'s `handlePay` and by
 * `buildDraftCreateBody`/`buildDraftPatchBody` below so the three request
 * bodies can never drift out of sync with each other.
 */
export function cartLinesToSaleItems(lines: CartLine[]): SaleItemCreate[] {
  return lines.map((line) => ({ variantId: line.variantId, qty: String(line.qty) }));
}

/**
 * The active, submittable discount for a request body, or `undefined`
 * when there is none to send — the exact same "is this discount really
 * on" test `estimateCartTotals` above already applies for the preview
 * (invalid format, blank, or exactly zero all count as "no discount"),
 * factored out so `sale/index.tsx`'s `handlePay` and
 * `buildDraftCreateBody`/`buildDraftPatchBody` below all agree with the
 * preview about what counts as "has a discount" (previously duplicated
 * inline in `handlePay` alone; T14 review would otherwise have three
 * copies of this same test to keep in sync).
 */
function activeDiscount(discount: CartDiscount | null): CartDiscount | null {
  if (
    discount == null ||
    discount.value.trim() === "" ||
    !isValidDiscountValue(discount.kind, discount.value) ||
    isZeroDecimalString(discount.value)
  ) {
    return null;
  }
  return discount;
}

export interface SubmitDiscount {
  type: DiscountKind;
  value: string;
}

/** `SaleCreate.discount`/`SaleDraftCreate.discount` shape, or `undefined`
 * when `discount` isn't currently active (`activeDiscount` above). */
export function resolveSubmitDiscount(discount: CartDiscount | null): SubmitDiscount | undefined {
  const active = activeDiscount(discount);
  return active ? { type: active.kind, value: active.value.trim() } : undefined;
}

/** `SaleCreate.discountReason`/`SaleDraftCreate.discountReason` — only ever
 * sent alongside an active discount, and only when non-blank (a reason
 * with no discount, or a blank reason, is never sent, matching
 * `admin/src/routes/app/QuickSalePage.tsx`). */
export function resolveSubmitDiscountReason(discount: CartDiscount | null): string | undefined {
  const active = activeDiscount(discount);
  const reason = active?.reason.trim();
  return reason ? reason : undefined;
}

/**
 * Builds the exact `POST /sales/drafts` body (T14/D-87) from the cart plus
 * the two pieces of screen-owned state a cart doesn't carry itself
 * (`locationId`, resolved from the picked `Location`; `customerId`, from
 * the attached `SelectedCustomer`, if any). Mirrors
 * `features/purchases/draft.ts`'s `buildCreateBody` shape/reasoning.
 */
export function buildDraftCreateBody(
  cart: CartState,
  locationId: string,
  customerId: string | null,
): SaleDraftCreate {
  const discount = resolveSubmitDiscount(cart.discount);
  const discountReason = resolveSubmitDiscountReason(cart.discount);
  const note = cart.note.trim();
  return {
    locationId,
    items: cartLinesToSaleItems(cart.lines),
    ...(customerId ? { customerId } : {}),
    ...(discount ? { discount } : {}),
    ...(discountReason ? { discountReason } : {}),
    ...(note ? { note } : {}),
  };
}

/**
 * Builds the exact `PATCH /sales/drafts/{id}` body for the "Save" action
 * while editing an already-loaded draft (`cart.draftId` set) — a full
 * replace of `items`/`customerId`/`discountType`/`discountValue`/
 * `discountReason`/`note` to match the form's current state exactly
 * (this task's own brief: "replacing `items`, customer, discount, note"),
 * not a partial diff against whatever the draft held before. `customerId`/
 * `discountReason`/`note` are explicit `null` (not omitted) when absent,
 * and `discountType`/`discountValue` are cleared together as the pair the
 * contract documents (`SaleDraftPatch`'s own doc comment, D-35) — omitting
 * a field here would leave the server's stored value untouched instead of
 * clearing it, which would silently disagree with what the form shows.
 */
export function buildDraftPatchBody(
  cart: CartState,
  locationId: string,
  customerId: string | null,
): SaleDraftPatch {
  const discount = resolveSubmitDiscount(cart.discount);
  const discountReason = resolveSubmitDiscountReason(cart.discount);
  const note = cart.note.trim();
  return {
    locationId,
    items: cartLinesToSaleItems(cart.lines),
    customerId: customerId ?? null,
    discountType: discount ? discount.type : null,
    discountValue: discount ? discount.value : null,
    discountReason: discountReason ?? null,
    note: note ? note : null,
  };
}

/** A generous stand-in `availableQty` for a cart line loaded from a draft
 * (`cartLinesFromDraftItems` below): unlike `VariantPicker`'s pick-time
 * snapshot, `SaleDraftItem` reports only `available` (a boolean — the
 * variant/product still exists and is active), never a stock quantity, so
 * there is no real number to put here. A value this large means
 * `qtyExceedsAvailable` never flags a loaded line's qty in the UI; the
 * server remains the only actual authority regardless (hard rule 8, this
 * module's own top-of-file doc comment). */
const UNKNOWN_AVAILABLE_QTY = "999999.000";

/** Truncates a possibly-fractional stored quantity to a whole unit for
 * `cartLinesFromDraftItems` below — `Math.trunc`, never `Math.round`
 * (T14 fix round MINOR 7): rounding a value like `"1.6"` up to `2` would
 * silently show/sell more than the draft actually recorded, the exact
 * kind of client-side quantity inflation hard rule 8 exists to rule out
 * (the server still recomputes and validates everything regardless, so
 * this is only ever a display/edit-starting-point concern). Never below
 * `1` — this app's cart has no zero/negative-qty line (`CartLine.qty`'s
 * own doc comment) — and never `NaN` for a malformed value. */
function truncateToWholeUnit(raw: string): number {
  const truncated = Math.trunc(Number(raw));
  return Number.isFinite(truncated) && truncated >= 1 ? truncated : 1;
}

/**
 * Maps a fetched `SaleDraft`'s `items` to `CartLine[]` for `loadDraft`
 * (T14) — `unitPrice`/`label`/`productName` come straight from the
 * server's own read-time resolution (`SaleDraftItem`'s own doc comment:
 * the same D-67 rule `SaleItem.unitPrice` uses), not recomputed here.
 * `qty` is truncated to a whole unit (`truncateToWholeUnit`) — this app's
 * cart has only ever supported whole-unit lines (`CartLine.qty`'s own
 * doc comment); a draft created or edited elsewhere (the admin web) with
 * a fractional qty is truncated rather than rejected, since `POST
 * .../complete` recomputes and validates everything server-side
 * regardless (hard rule 8). `available` carries straight through
 * (`CartLine.available`'s own doc comment).
 */
export function cartLinesFromDraftItems(items: SaleDraftItem[]): CartLine[] {
  return items.map((item) => ({
    variantId: item.variantId,
    label: item.variantLabel,
    productName: item.productName,
    unitPrice: item.unitPrice,
    availableQty: UNKNOWN_AVAILABLE_QTY,
    qty: truncateToWholeUnit(item.qty),
    available: item.available,
  }));
}

/** Maps a fetched `SaleDraft`'s `discount`/`discountReason` pair to this
 * module's own `CartDiscount` shape for `loadDraft` — `null` when the
 * draft has no manual discount (`SaleDraft.discount`'s own doc comment). */
export function cartDiscountFromDraft(
  discount: SaleDiscount | null,
  discountReason: string | null,
): CartDiscount | null {
  if (!discount) {
    return null;
  }
  return { kind: discount.type, value: discount.value, reason: discountReason ?? "" };
}

export type DraftPayPlan = "complete" | "patchThenComplete";

/**
 * Decides whether Pay on a loaded draft (`cart.draftId` set) needs to
 * PATCH the draft to this screen's current state before completing it,
 * or can complete directly (T14 fix round, Opus review CRITICAL).
 * `dirty` false means nothing has changed since `loadDraft` (or since
 * the last successful Save/Pay on it) — the server's own stored draft
 * already matches the cart, so completing it directly is both correct
 * and strictly safer than a needless PATCH. `completionAttempted` — set
 * the moment *any* complete request for this draft is sent, before its
 * response arrives (`CartState.completionAttempted`'s own doc comment)
 * — permanently rules out `patchThenComplete` for the rest of this
 * draft's session once `true`, even if the cart is edited again
 * afterwards: a PATCH following an attempt whose outcome is unknown (a
 * lost response) could race with, or paper over, a complete that already
 * committed. Every later Pay tap instead replays `complete` under the
 * *same* `idempotencyKey`, which the server answers with its
 * already-stored 201 if the first attempt did commit
 * (`nextAfterDraftPayError` below covers what happens if it did not).
 *
 * | dirty | completionAttempted | plan              |
 * | ----- | -------------------- | ----------------- |
 * | false | false                | complete           |
 * | false | true                 | complete           |
 * | true  | false                | patchThenComplete  |
 * | true  | true                 | complete           |
 */
export function planDraftPay(input: {
  draftId: string | null;
  dirty: boolean;
  completionAttempted: boolean;
}): DraftPayPlan {
  return input.dirty && !input.completionAttempted ? "patchThenComplete" : "complete";
}

/**
 * `details.entity` for a `404 NOT_FOUND` (`apierr.NotFound`,
 * `api/internal/apierr/apierr.go`) — `"variant"` means a line in the
 * draft's own stored/submitted `items` references a variant that no
 * longer resolves (soft-deleted, deactivated, or never existed;
 * `resolveSaleItems`, `api/internal/sales/create.go`, shared by
 * `PATCH /sales/drafts/{id}` when it replaces `items`), never that the
 * draft itself is gone. Every other entity (`"draft"`, `"location"`,
 * `"customer"`) means something the request named directly is missing.
 * A caller passes `error.details` straight through; `undefined` (no
 * details at all, or a non-`NOT_FOUND` error) is not a variant miss.
 */
export function isVariantNotFoundDetails(details: Record<string, unknown> | undefined): boolean {
  return details?.entity === "variant";
}

export type DraftPayErrorAction =
  | "retryCompleteSameKey"
  | "draftGone"
  | "lineVariantGone"
  | "forbiddenToEditDraft"
  | "patchNetworkSafe"
  | "completeNetworkAmbiguous"
  | "patchGenericError"
  | "useIdempotencyOutcome";

export interface DraftPayErrorInput {
  leg: "patch" | "complete";
  /** The decoded server error code, or `undefined` for a network drop/
   * timeout — a response that was never received at all, the same
   * "undecoded" convention `idempotencyOutcome` below already uses. */
  code: ErrorCode | undefined;
  /** `isVariantNotFoundDetails` on the `patch` leg's own error `details`
   * — meaningless (and ignored) on the `complete` leg, whose only
   * `NOT_FOUND` is `apierr.NotFound("draft")`
   * (`api/internal/sales/drafts_write.go`), never a variant. */
  isVariantNotFound: boolean;
  /** `true` only when this `NOT_FOUND` is itself the response to the
   * one-shot same-key replay `"retryCompleteSameKey"` asked for —
   * distinguishes "this is the very first `NOT_FOUND` seen for this
   * attempt" (worth one retry) from "the retry also `NOT_FOUND`ed"
   * (genuinely gone, T14 fix round MAJOR 2). Ignored for every other
   * leg/code combination. */
  isRetry: boolean;
}

/**
 * The state-machine decisions Pay-on-a-loaded-draft's two legs (`PATCH`,
 * `complete`) need beyond the existing `idempotencyOutcome` (still used,
 * unchanged, for the `complete` leg's own rekey-vs-keep-key decision on
 * every code this function answers `"useIdempotencyOutcome"` for).
 * Message text stays where every other error mapping in this app
 * already lives, in the screen itself; this only decides which *state*
 * transition applies (T14 fix round, Opus review).
 *
 * patch leg:
 * | code                | isVariantNotFound | action              |
 * | ------------------- | ------------------ | ------------------- |
 * | NOT_FOUND           | true                | lineVariantGone      |
 * | NOT_FOUND           | false               | draftGone            |
 * | FORBIDDEN           | —                   | forbiddenToEditDraft |
 * | undefined (network) | —                   | patchNetworkSafe     |
 * | anything else       | —                   | patchGenericError    |
 *
 * complete leg:
 * | code                 | isRetry | action                |
 * | -------------------- | ------- | --------------------- |
 * | NOT_FOUND            | false   | retryCompleteSameKey   |
 * | NOT_FOUND            | true    | draftGone              |
 * | undefined (network)  | —       | completeNetworkAmbiguous |
 * | anything else        | —       | useIdempotencyOutcome  |
 */
export function nextAfterDraftPayError(input: DraftPayErrorInput): DraftPayErrorAction {
  if (input.leg === "patch") {
    if (input.code === "NOT_FOUND") {
      return input.isVariantNotFound ? "lineVariantGone" : "draftGone";
    }
    if (input.code === "FORBIDDEN") {
      return "forbiddenToEditDraft";
    }
    if (input.code === undefined) {
      return "patchNetworkSafe";
    }
    return "patchGenericError";
  }
  if (input.code === "NOT_FOUND") {
    return input.isRetry ? "draftGone" : "retryCompleteSameKey";
  }
  if (input.code === undefined) {
    return "completeNetworkAmbiguous";
  }
  return "useIdempotencyOutcome";
}
