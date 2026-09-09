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
