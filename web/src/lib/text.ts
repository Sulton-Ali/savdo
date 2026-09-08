/**
 * Splits plain text into paragraphs on blank-line boundaries (O-19's
 * `about.body`/product `description` shape: "paragraphs separated by blank
 * lines, no HTML"). Callers render each entry as its own `<p>` via normal
 * JSX text interpolation — React escapes it automatically, so this never
 * needs (and must never use) `dangerouslySetInnerHTML`.
 */
export function splitParagraphs(text: string): string[] {
  return text
    .split(/\n{2,}/)
    .map((paragraph) => paragraph.trim())
    .filter((paragraph) => paragraph.length > 0);
}
