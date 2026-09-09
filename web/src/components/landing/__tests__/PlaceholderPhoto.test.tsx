import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import {
  hashSeed,
  PLACEHOLDER_TONES,
  PlaceholderPhoto,
  placeholderTone,
} from "../PlaceholderPhoto";

describe("PlaceholderPhoto", () => {
  it("renders the label and hides the whole block from the accessibility tree", () => {
    const { container } = render(
      <PlaceholderPhoto icon={<span>icon</span>} label="Mahsulot fotosi" tone="peach" />,
    );
    expect(screen.getByText("Mahsulot fotosi")).toBeTruthy();
    expect(container.firstElementChild?.getAttribute("aria-hidden")).toBe("true");
  });

  it("renders the label at full opacity (review: opacity-80 dropped peach below WCAG AA)", () => {
    render(<PlaceholderPhoto icon={<span>icon</span>} label="Mahsulot fotosi" tone="peach" />);
    expect(screen.getByText("Mahsulot fotosi").className).not.toMatch(/opacity/);
  });
});

/**
 * WCAG 2.x contrast ratio between two sRGB hex colours — same formula as
 * `packages/ui-tokens/src/tokens.test.ts`'s helper (D-121 T1), reproduced
 * here rather than imported so this suite has no cross-package dependency.
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

describe("PlaceholderPhoto tone/label contrast (D-121 review)", () => {
  // Mirrors `TONE_CLASS`'s literal hex values in PlaceholderPhoto.tsx and
  // the icon/label colour (`text-[#6d4d43]`) applied at full opacity.
  const TONE_HEX: Record<(typeof PLACEHOLDER_TONES)[number], string> = {
    peach: "#fbe2d9",
    teal: "#d5ecea",
    sand: "#f1e8dc",
    lilac: "#e6e3f2",
  };
  const LABEL_COLOR = "#6d4d43";
  const AA_NORMAL_TEXT = 4.5;

  for (const tone of PLACEHOLDER_TONES) {
    it(`${tone} background passes AA for the label at full opacity`, () => {
      expect(contrastRatio(LABEL_COLOR, TONE_HEX[tone])).toBeGreaterThanOrEqual(AA_NORMAL_TEXT);
    });
  }
});

describe("placeholderTone", () => {
  it("stays within the fixed palette for any seed, including negative ones", () => {
    for (const seed of [0, 1, 4, -1, -7, 999]) {
      expect(PLACEHOLDER_TONES).toContain(placeholderTone(seed));
    }
  });

  it("is deterministic for the same seed", () => {
    expect(placeholderTone(7)).toBe(placeholderTone(7));
  });
});

describe("hashSeed", () => {
  it("is deterministic for the same input", () => {
    expect(hashSeed("kids-tshirt")).toBe(hashSeed("kids-tshirt"));
  });

  it("differs across distinct inputs (not a constant)", () => {
    const seeds = new Set(["kids", "women", "men", "kids-tshirt", "women-dress"].map(hashSeed));
    expect(seeds.size).toBeGreaterThan(1);
  });
});
