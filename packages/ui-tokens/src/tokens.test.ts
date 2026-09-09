import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { tokens } from "./tokens.js";

/**
 * Parity check between `tokens.ts` (the typed object apps import) and
 * `tokens.css` (the CSS custom properties mirrored by hand, since this
 * package has no build step). Catches the two drifting after a hand edit.
 */
const cssPath = fileURLToPath(new URL("./tokens.css", import.meta.url));
const css = readFileSync(cssPath, "utf8");

function cssVar(name: string): string {
  const match = css.match(new RegExp(`--savdo-${name}:\\s*([^;]+);`));
  if (!match?.[1]) {
    throw new Error(`--savdo-${name} not found in tokens.css`);
  }
  return match[1].trim();
}

describe("tokens.css matches tokens.ts", () => {
  it("colours", () => {
    expect(cssVar("color-primary")).toBe(tokens.color.primary);
    expect(cssVar("color-primary-hover")).toBe(tokens.color.primaryHover);
    expect(cssVar("color-bg")).toBe(tokens.color.bg);
    expect(cssVar("color-surface")).toBe(tokens.color.surface);
    expect(cssVar("color-text")).toBe(tokens.color.text);
    expect(cssVar("color-muted")).toBe(tokens.color.muted);
    expect(cssVar("color-success")).toBe(tokens.color.success);
    expect(cssVar("color-warning")).toBe(tokens.color.warning);
    expect(cssVar("color-danger")).toBe(tokens.color.danger);
  });

  it("radius", () => {
    expect(cssVar("radius-sm")).toBe(`${tokens.radius.sm}px`);
    expect(cssVar("radius-md")).toBe(`${tokens.radius.md}px`);
    expect(cssVar("radius-lg")).toBe(`${tokens.radius.lg}px`);
  });

  it("font family", () => {
    expect(cssVar("font-family")).toBe(tokens.font.family);
  });

  it("landing colours (D-121)", () => {
    expect(cssVar("landing-accent")).toBe(tokens.landing.accent);
    expect(cssVar("landing-accent-hover")).toBe(tokens.landing.accentHover);
    expect(cssVar("landing-on-accent")).toBe(tokens.landing.onAccent);
    expect(cssVar("landing-secondary")).toBe(tokens.landing.secondary);
    expect(cssVar("landing-secondary-hover")).toBe(tokens.landing.secondaryHover);
    expect(cssVar("landing-on-secondary")).toBe(tokens.landing.onSecondary);
    expect(cssVar("landing-surface")).toBe(tokens.landing.surface);
  });

  it("landing radius and shadow (D-121)", () => {
    expect(cssVar("landing-radius-card")).toBe(`${tokens.landing.radius.card}px`);
    expect(cssVar("landing-shadow-card")).toBe(tokens.landing.shadow.card);
  });
});

describe("brand tokens (D-36)", () => {
  it("primary is terracotta", () => {
    expect(tokens.color.primary).toBe("#c2410c");
  });

  it("font family leads with self-hosted Inter Variable", () => {
    expect(tokens.font.family).toBe('"Inter Variable", Inter, system-ui, sans-serif');
  });
});

describe("landing tokens (D-121) don't touch the shared brand", () => {
  it("primary stays terracotta, not coral/teal", () => {
    expect(tokens.color.primary).toBe("#c2410c");
  });

  it("landing accent/secondary are additive, scoped keys", () => {
    expect(tokens.landing.accent).toBe("#e05a47");
    expect(tokens.landing.secondary).toBe("#1f8a80");
  });
});

/**
 * WCAG 2.x contrast ratio between two sRGB hex colours, via relative
 * luminance (https://www.w3.org/TR/WCAG21/#dfn-relative-luminance).
 */
function relativeLuminance(hex: string): number {
  const linear = (channel: number) => {
    const c = channel / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  };
  const r = linear(Number.parseInt(hex.slice(1, 3), 16));
  const g = linear(Number.parseInt(hex.slice(3, 5), 16));
  const b = linear(Number.parseInt(hex.slice(5, 7), 16));
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function contrastRatio(hexA: string, hexB: string): number {
  const lA = relativeLuminance(hexA);
  const lB = relativeLuminance(hexB);
  const lighter = Math.max(lA, lB);
  const darker = Math.min(lA, lB);
  return (lighter + 0.05) / (darker + 0.05);
}

describe("landing WCAG AA contrast (D-121)", () => {
  const AA_NORMAL_TEXT = 4.5;
  const AA_LARGE_TEXT = 3;

  it("on-accent white passes AA for large text/UI, not normal text, on the base accent", () => {
    const ratio = contrastRatio(tokens.landing.onAccent, tokens.landing.accent);
    expect(ratio).toBeGreaterThanOrEqual(AA_LARGE_TEXT);
    expect(ratio).toBeLessThan(AA_NORMAL_TEXT);
  });

  it("on-accent white passes AA for normal text on the darker accentHover step", () => {
    const ratio = contrastRatio(tokens.landing.onAccent, tokens.landing.accentHover);
    expect(ratio).toBeGreaterThanOrEqual(AA_NORMAL_TEXT);
  });

  it("on-secondary white passes AA for large text/UI, not normal text, on the base secondary", () => {
    const ratio = contrastRatio(tokens.landing.onSecondary, tokens.landing.secondary);
    expect(ratio).toBeGreaterThanOrEqual(AA_LARGE_TEXT);
    expect(ratio).toBeLessThan(AA_NORMAL_TEXT);
  });

  it("on-secondary white passes AA for normal text on the darker secondaryHover step", () => {
    const ratio = contrastRatio(tokens.landing.onSecondary, tokens.landing.secondaryHover);
    expect(ratio).toBeGreaterThanOrEqual(AA_NORMAL_TEXT);
  });
});
