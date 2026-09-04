import type { Area } from "react-easy-crop";

/**
 * Renders the cropped region of `imageSrc` (an object URL) onto an
 * off-screen canvas at its native pixel size and exports it as a JPEG Blob
 * (quality 0.9) — the file the crop modal hands to `uploadMedia`. jsdom has
 * no canvas implementation, so every test mocks this function rather than
 * exercising it (`admin/vite.config.ts` test environment).
 */
export async function cropImageToBlob(imageSrc: string, area: Area): Promise<Blob> {
  const image = await loadImage(imageSrc);
  const canvas = document.createElement("canvas");
  canvas.width = Math.round(area.width);
  canvas.height = Math.round(area.height);
  const ctx = canvas.getContext("2d");
  if (!ctx) {
    throw new Error("2d canvas context unavailable");
  }
  ctx.drawImage(image, area.x, area.y, area.width, area.height, 0, 0, canvas.width, canvas.height);
  return new Promise<Blob>((resolve, reject) => {
    canvas.toBlob(
      (blob) => {
        if (blob) {
          resolve(blob);
        } else {
          reject(new Error("canvas export failed"));
        }
      },
      "image/jpeg",
      0.9,
    );
  });
}

function loadImage(src: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image();
    image.onload = () => resolve(image);
    image.onerror = () => reject(new Error("failed to load image for cropping"));
    image.src = src;
  });
}
