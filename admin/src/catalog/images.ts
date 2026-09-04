import type { ProductImage } from "./api";

/** The product's image cap (D-34) — the upload control disables past this. */
export const MAX_PRODUCT_IMAGES = 8;

/**
 * Moves one image up or down one slot in display order, returning a new
 * array (or the same array, unchanged, at either end). Callers post the
 * result's full id list to `PATCH /products/{id}/images/order` — the
 * endpoint always takes every id, never a single move (`docs/05-API.md` §
 * Catalogue and media).
 */
export function moveImage(
  images: ProductImage[],
  imageId: string,
  direction: "up" | "down",
): ProductImage[] {
  const index = images.findIndex((image) => image.id === imageId);
  if (index < 0) {
    return images;
  }
  const swapWith = direction === "up" ? index - 1 : index + 1;
  if (swapWith < 0 || swapWith >= images.length) {
    return images;
  }
  const next = [...images];
  const temp = next[index];
  next[index] = next[swapWith] as ProductImage;
  next[swapWith] = temp as ProductImage;
  return next;
}
