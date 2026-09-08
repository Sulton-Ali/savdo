import type { components } from "@savdo/api-client";

import { CoverImage } from "./CoverImage";

type PublicHero = components["schemas"]["PublicHero"];

/**
 * The hero title/tagline render on the brand colour when there is no photo
 * (D-99); with a photo, the image is the eager-loaded LCP candidate (only
 * one per page, per deliverable 4). `ctaHref`/`ctaLabel` add a primary
 * button (deliverable 4 — the hero needed more visual weight than a flat
 * title/tagline pair) pointing at the newest-products section on the same
 * page (`ctaHref="#products"`) — a plain `<a>`, same reasoning as
 * `LanguageSwitcher`: not a typed router destination.
 */
export function HeroSection({
  hero,
  fallbackTitle,
  ctaHref,
  ctaLabel,
}: {
  hero?: PublicHero;
  fallbackTitle: string;
  ctaHref: string;
  ctaLabel: string;
}) {
  const title = hero?.title ?? fallbackTitle;
  return (
    <section className="overflow-hidden rounded-lg bg-primary text-white">
      <div className="grid gap-6 p-8 sm:grid-cols-2 sm:items-center sm:p-12">
        <div className="flex flex-col items-start gap-4">
          <h1 className="font-bold text-4xl leading-tight sm:text-5xl">{title}</h1>
          {hero?.tagline != null && hero.tagline !== "" && (
            <p className="text-lg text-white/90">{hero.tagline}</p>
          )}
          <a
            href={ctaHref}
            className="rounded-md bg-white px-5 py-2.5 font-semibold text-primary transition hover:bg-white/90"
          >
            {ctaLabel}
          </a>
        </div>
        {hero?.image != null && (
          <CoverImage image={hero.image} size="full" alt={title} loading="eager" />
        )}
      </div>
    </section>
  );
}
