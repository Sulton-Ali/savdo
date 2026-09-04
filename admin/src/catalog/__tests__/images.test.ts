import { describe, expect, it } from "vitest";

import type { ProductImage } from "../api";
import { MAX_PRODUCT_IMAGES, moveImage } from "../images";

function image(id: string, sortOrder: number): ProductImage {
  return {
    id,
    mediaId: `media-${id}`,
    variantId: null,
    isCover: sortOrder === 0,
    sortOrder,
    urls: { thumb: `${id}-thumb`, card: `${id}-card`, full: `${id}-full` },
  };
}

describe("moveImage", () => {
  const images = [image("a", 0), image("b", 1), image("c", 2)];

  it("swaps with the previous image when moving up", () => {
    expect(moveImage(images, "b", "up").map((i) => i.id)).toEqual(["b", "a", "c"]);
  });

  it("swaps with the next image when moving down", () => {
    expect(moveImage(images, "b", "down").map((i) => i.id)).toEqual(["a", "c", "b"]);
  });

  it("is a no-op moving the first image up", () => {
    expect(moveImage(images, "a", "up").map((i) => i.id)).toEqual(["a", "b", "c"]);
  });

  it("is a no-op moving the last image down", () => {
    expect(moveImage(images, "c", "down").map((i) => i.id)).toEqual(["a", "b", "c"]);
  });

  it("is a no-op for an unknown id", () => {
    expect(moveImage(images, "z", "up")).toBe(images);
  });
});

describe("MAX_PRODUCT_IMAGES", () => {
  it("is the D-34 cap", () => {
    expect(MAX_PRODUCT_IMAGES).toBe(8);
  });
});
