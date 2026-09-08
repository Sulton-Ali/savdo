/**
 * True only for an `https:` URL — the guard every admin-supplied external
 * link (social/`mapUrl`, O-19 "https only") must pass before it is ever
 * rendered as an `href`. Rejects `javascript:`, `data:`, relative paths,
 * malformed input, and plain `http:` alike; a value that fails this check
 * is omitted, not "fixed" or defaulted.
 */
export function isSafeHttpsUrl(value: string | null | undefined): boolean {
  if (value == null || value === "") {
    return false;
  }
  try {
    return new URL(value).protocol === "https:";
  } catch {
    return false;
  }
}
