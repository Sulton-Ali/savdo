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
});

describe("brand tokens (D-36)", () => {
  it("primary is terracotta", () => {
    expect(tokens.color.primary).toBe("#c2410c");
  });

  it("font family leads with self-hosted Inter Variable", () => {
    expect(tokens.font.family).toBe('"Inter Variable", Inter, system-ui, sans-serif');
  });
});
