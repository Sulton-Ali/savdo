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
