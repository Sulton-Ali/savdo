/** Formats an `InputNumber` value into the decimal-string wire format
 * (`"125000.00"`, ADR-007) — money is a string on the wire, never a float. */
export function formatMoney(value: number | null | undefined): string | undefined {
  if (value == null || Number.isNaN(value)) {
    return undefined;
  }
  return value.toFixed(2);
}

/** Parses a `Decimal` wire string (`"125000.00"`) back into the number an
 * `InputNumber` field expects. */
export function parseMoney(value: string | null | undefined): number | undefined {
  if (value == null) {
    return undefined;
  }
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : undefined;
}

/** Formats a `Decimal` wire string for display with thousands separators
 * (`"125000.00"` -> `"125,000"`), dropping a trailing `.00`/`.000` — UZS
 * has no everyday subunit — but keeping a non-zero fraction (`"125000.50"`
 * -> `"125,000.50"`). Works on the string directly (no `Number` parsing,
 * hence no precision loss) — display-only, never used for arithmetic;
 * compute with `parseMoney`/a decimal-safe helper instead. */
export function formatMoneyDisplay(value: string): string {
  const trimmed = value.trim();
  const negative = trimmed.startsWith("-");
  const unsigned = negative ? trimmed.slice(1) : trimmed;
  const [intPartRaw, fracPart = ""] = unsigned.split(".");
  const intPart = (intPartRaw || "0").replace(/^0+(?=\d)/, "");
  const withSeparators = intPart.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  const hasFraction = /[1-9]/.test(fracPart);
  const suffix = hasFraction ? `.${fracPart}` : "";
  const sign = negative && (withSeparators !== "0" || hasFraction) ? "-" : "";
  return `${sign}${withSeparators}${suffix}`;
}
