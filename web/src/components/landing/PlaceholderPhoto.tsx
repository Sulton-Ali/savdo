import type { ReactNode } from "react";

/**
 * A soft-coloured decorative box standing in for a photo the shop has not
 * uploaded yet (D-121, Variant B: "placeholder photos until the shop
 * supplies real ones" — never a stock image). Rotates through a small
 * pastel palette lifted from the design canvas (decoration only, not part
 * of the T1 landing tokens — those cover the accent/secondary/surface
 * brand colours, not this fallback-art palette).
 *
 * The whole block is `aria-hidden` — same pattern as `CoverImage`'s own
 * image-less box: the icon and label are for a sighted visitor looking at
 * an empty photo slot, not for a screen reader. Every caller already gives
 * the surrounding heading/link its own accessible name (product name,
 * category name, the hero `<h1>`), so this never needs one of its own.
 */
const TONE_CLASS: Record<"peach" | "teal" | "sand" | "lilac", string> = {
  peach: "bg-[#fbe2d9]",
  teal: "bg-[#d5ecea]",
  sand: "bg-[#f1e8dc]",
  lilac: "bg-[#e6e3f2]",
};

export const PLACEHOLDER_TONES = ["peach", "teal", "sand", "lilac"] as const;
export type PlaceholderTone = (typeof PLACEHOLDER_TONES)[number];

/** Picks a tone deterministically from `seed` (an index, or `hashSeed` of a
 * slug) so the same category/product always renders the same tone across
 * renders, without any caller needing to track "which tone is next". */
export function placeholderTone(seed: number): PlaceholderTone {
  const tones = PLACEHOLDER_TONES;
  return tones[((seed % tones.length) + tones.length) % tones.length] as PlaceholderTone;
}

/** A tiny deterministic string hash (not cryptographic — just enough to
 * spread slugs across `placeholderTone`'s palette) for callers that pick a
 * tone from an id/slug rather than a list position. */
export function hashSeed(input: string): number {
  let hash = 0;
  for (let index = 0; index < input.length; index++) {
    hash = (hash * 31 + input.charCodeAt(index)) | 0;
  }
  return hash;
}

const ASPECT_CLASS: Record<"square" | "portrait", string> = {
  square: "aspect-square",
  portrait: "aspect-[3/4]",
};

export function PlaceholderPhoto({
  icon,
  label,
  tone,
  aspect = "square",
  className = "",
}: {
  /** Already sized by the caller (e.g. `<FamilyIcon className="h-16 w-16" />`) — this component only positions it. */
  icon: ReactNode;
  label: string;
  tone: PlaceholderTone;
  aspect?: "square" | "portrait";
  className?: string;
}) {
  return (
    <div
      aria-hidden="true"
      className={`flex ${ASPECT_CLASS[aspect]} w-full flex-col items-center justify-center gap-3 rounded-xl text-[#6d4d43] ${TONE_CLASS[tone]} ${className}`}
    >
      {icon}
      <span className="px-4 text-center font-medium text-xs opacity-80">{label}</span>
    </div>
  );
}
