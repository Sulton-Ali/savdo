import type { components } from "@savdo/api-client";

type MediaUrls = components["schemas"]["MediaUrls"];

/**
 * A product/hero image, or a neutral placeholder when there is none
 * (uncategorized/imageless products render fine, O-22). Rendered in a
 * fixed-ratio box (`square` 1:1, `portrait` 3:4 for product cards) with
 * `object-cover` so `width`/`height` reliably describe the reserved layout
 * box regardless of the source photo's true aspect ratio (ADR-008's
 * derivatives are resized to fit within 200/600/1600px, not guaranteed
 * square) — this avoids claiming false intrinsic dimensions while still
 * preventing layout shift. The box always carries a neutral background and
 * the `<img>` itself is `text-transparent`, so a broken/missing image
 * source never surfaces raw alt text — just an empty tinted box.
 */
export function CoverImage({
  image,
  size,
  alt,
  loading = "lazy",
  aspect = "square",
  className = "",
}: {
  image?: MediaUrls;
  size: "thumb" | "card" | "full";
  alt: string;
  loading?: "lazy" | "eager";
  aspect?: "square" | "portrait";
  className?: string;
}) {
  const width = size === "thumb" ? 200 : size === "card" ? 600 : 1600;
  const height = aspect === "portrait" ? Math.round((width * 4) / 3) : width;
  const aspectClass = aspect === "portrait" ? "aspect-[3/4]" : "aspect-square";
  if (!image) {
    return (
      <div aria-hidden="true" className={`${aspectClass} w-full rounded-md bg-bg ${className}`} />
    );
  }
  return (
    <img
      src={image[size]}
      alt={alt}
      width={width}
      height={height}
      loading={loading}
      decoding="async"
      fetchPriority={loading === "eager" ? "high" : undefined}
      className={`${aspectClass} w-full rounded-md bg-bg object-cover text-transparent ${className}`}
    />
  );
}
