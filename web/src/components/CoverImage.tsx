import type { components } from "@savdo/api-client";

type MediaUrls = components["schemas"]["MediaUrls"];

/**
 * A product/hero image, or a neutral placeholder when there is none
 * (uncategorized/imageless products render fine, O-22). Always rendered in
 * a fixed 1:1 box with `object-cover` so `width`/`height` reliably describe
 * the reserved layout box regardless of the source photo's true aspect
 * ratio (ADR-008's derivatives are resized to fit within 200/600/1600px,
 * not guaranteed square) — this avoids claiming false intrinsic dimensions
 * while still preventing layout shift.
 */
export function CoverImage({
  image,
  size,
  alt,
  loading = "lazy",
  className = "",
}: {
  image?: MediaUrls;
  size: "thumb" | "card" | "full";
  alt: string;
  loading?: "lazy" | "eager";
  className?: string;
}) {
  const px = size === "thumb" ? 200 : size === "card" ? 600 : 1600;
  if (!image) {
    return (
      <div aria-hidden="true" className={`aspect-square w-full rounded-md bg-bg ${className}`} />
    );
  }
  return (
    <img
      src={image[size]}
      alt={alt}
      width={px}
      height={px}
      loading={loading}
      className={`aspect-square w-full rounded-md object-cover ${className}`}
    />
  );
}
