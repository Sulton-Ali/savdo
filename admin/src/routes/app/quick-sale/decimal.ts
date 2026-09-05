/**
 * Decimal-safe arithmetic for the quick-sale cart preview. Money and
 * quantity are wire `Decimal` strings (ADR-007) — this module never runs
 * `Number` multiplication or division on them (a known trap: `0.1 * 3`
 * already isn't exact in floating point, and shop totals compound the
 * error). Instead every value is converted to a fixed-point `BigInt`
 * (tiyin — 2 places — for money; milli-units — 3 places — for quantity,
 * matching `docs/05-API.md`'s stock `"available": "2.000"` convention),
 * the arithmetic runs on integers, and the result is formatted back to a
 * 2-place decimal string.
 *
 * This is a client-side *preview* only: `POST /sales` never sends a total
 * or a line price, only `variantId`/`qty` (D-56) — the server recomputes
 * every total authoritatively (hard rule 8).
 */

const MONEY_SCALE = 2;
const QTY_SCALE = 3;
/** Generous fixed-point scale for a discount `percent` value (e.g. "12.5"). */
const PERCENT_SCALE = 4;

function toFixedPoint(value: string, scale: number): bigint {
  const trimmed = value.trim();
  const negative = trimmed.startsWith("-");
  const unsigned = negative ? trimmed.slice(1) : trimmed;
  const [intPart = "0", fracPart = ""] = unsigned.split(".");
  const paddedFrac = (fracPart + "0".repeat(scale)).slice(0, scale);
  const magnitude = BigInt(`${intPart || "0"}${paddedFrac}` || "0");
  return negative ? -magnitude : magnitude;
}

function fromFixedPoint(value: bigint, scale: number): string {
  const negative = value < 0n;
  const magnitude = negative ? -value : value;
  const digits = magnitude.toString().padStart(scale + 1, "0");
  const intPart = digits.slice(0, digits.length - scale);
  const fracPart = digits.slice(digits.length - scale);
  const sign = negative && magnitude !== 0n ? "-" : "";
  return scale > 0 ? `${sign}${intPart}.${fracPart}` : `${sign}${intPart}`;
}

/** Rounds `numerator / divisor` half-up, away from zero, both as `BigInt`. */
function roundDiv(numerator: bigint, divisor: bigint): bigint {
  if (numerator >= 0n) {
    return (numerator + divisor / 2n) / divisor;
  }
  return -((-numerator + divisor / 2n) / divisor);
}

/** `unitPrice` (money, 2-place decimal string) x `qty` (quantity, up to
 * 3-place decimal string) -> a money decimal string, rounded to 2 places. */
export function multiplyMoneyByQty(unitPrice: string, qty: string): string {
  const priceUnits = toFixedPoint(unitPrice, MONEY_SCALE);
  const qtyUnits = toFixedPoint(qty, QTY_SCALE);
  const divisor = 10n ** BigInt(QTY_SCALE);
  return fromFixedPoint(roundDiv(priceUnits * qtyUnits, divisor), MONEY_SCALE);
}

export function addMoney(a: string, b: string): string {
  return fromFixedPoint(toFixedPoint(a, MONEY_SCALE) + toFixedPoint(b, MONEY_SCALE), MONEY_SCALE);
}

export function subtractMoney(a: string, b: string): string {
  return fromFixedPoint(toFixedPoint(a, MONEY_SCALE) - toFixedPoint(b, MONEY_SCALE), MONEY_SCALE);
}

/** Sums a list of money decimal strings; `[]` sums to `"0.00"`. */
export function sumMoney(values: string[]): string {
  return values.reduce((acc, value) => addMoney(acc, value), "0.00");
}

/** `true` when `a` is strictly greater than `b` (both money decimal strings). */
export function isMoneyGreaterThan(a: string, b: string): boolean {
  return toFixedPoint(a, MONEY_SCALE) > toFixedPoint(b, MONEY_SCALE);
}

/** Clamps a money decimal string at zero (never negative) — used for the
 * total preview, since the server rejects a discount over the subtotal
 * rather than silently clamping it (D-57); this only keeps the *preview*
 * from showing a negative total while the user is mid-edit. */
export function clampMoneyAtZero(value: string): string {
  return isMoneyGreaterThan("0.00", value) ? "0.00" : value;
}

/** `percent` (0..100, decimal string, e.g. "12.5") of a money `subtotal`,
 * decimal-safe, rounded to 2 places. */
export function percentOfMoney(subtotal: string, percent: string): string {
  const subtotalUnits = toFixedPoint(subtotal, MONEY_SCALE);
  const percentUnits = toFixedPoint(percent, PERCENT_SCALE);
  const divisor = 10n ** BigInt(PERCENT_SCALE) * 100n;
  return fromFixedPoint(roundDiv(subtotalUnits * percentUnits, divisor), MONEY_SCALE);
}
