import type { components } from "@savdo/api-client";

import { CoverImage } from "./CoverImage";

type PublicHero = components["schemas"]["PublicHero"];

/**
 * The hero title/tagline render on the brand colour when there is no photo
 * (D-99); with a photo, the image is the eager-loaded LCP candidate (only
 * one per page, per deliverable 4).
 */
export function HeroSection({ hero, fallbackTitle }: { hero?: PublicHero; fallbackTitle: string }) {
  const title = hero?.title ?? fallbackTitle;
  return (
    <section className="overflow-hidden rounded-lg bg-primary text-white">
      <div className="grid gap-6 p-8 sm:grid-cols-2 sm:items-center">
        <div>
          <h1 className="font-bold text-3xl">{title}</h1>
          {hero?.tagline != null && hero.tagline !== "" && (
            <p className="mt-2 text-white/90">{hero.tagline}</p>
          )}
        </div>
        {hero?.image != null && (
          <CoverImage image={hero.image} size="full" alt={title} loading="eager" />
        )}
      </div>
    </section>
  );
}
