import type { components } from "@savdo/api-client";
import { CoverImage } from "../CoverImage";
import { FamilyIcon } from "../icons";
import { PlaceholderPhoto } from "./PlaceholderPhoto";
import { TelegramButton } from "./TelegramButton";

type PublicHero = components["schemas"]["PublicHero"];

/**
 * The homepage hero card (D-121, Variant B): one white rounded card with
 * the photo (or a placeholder) on one side and title/tagline/CTAs on the
 * other, photo-first on mobile and text-first on desktop — matching the
 * design canvas's `order` swap between `BMobile`/`BDesktop`. `eyebrow` is a
 * fixed marketing label (no per-shop "district" field exists to source a
 * more specific line from — see the task report). The Telegram button is
 * eager, the fallback to `<CoverImage>`'s own `loading="eager"` matches the
 * old `HeroSection`'s LCP-candidate reasoning for a real photo.
 */
export function HeroCard({
  hero,
  fallbackTitle,
  eyebrow,
  telegramHref,
  telegramLabel,
  catalogLabel,
  catalogHref,
  photoLabel,
}: {
  hero?: PublicHero;
  fallbackTitle: string;
  eyebrow: string;
  telegramHref: string | null;
  telegramLabel: string;
  catalogLabel: string;
  catalogHref: string;
  photoLabel: string;
}) {
  const title = hero?.title ?? fallbackTitle;
  return (
    <div className="grid items-center gap-6 rounded-landing-card bg-landing-surface p-5 shadow-landing-card sm:grid-cols-2 sm:gap-8 sm:p-10">
      <div className="order-1 sm:order-2">
        {hero?.image != null ? (
          <CoverImage image={hero.image} size="full" alt={title} loading="eager" aspect="square" />
        ) : (
          <PlaceholderPhoto
            tone="peach"
            label={photoLabel}
            icon={<FamilyIcon className="h-24 w-24 sm:h-32 sm:w-32" />}
          />
        )}
      </div>
      <div className="order-2 flex flex-col items-start gap-4 sm:order-1">
        <span className="font-bold text-landing-secondary-hover text-xs uppercase tracking-wide">
          {eyebrow}
        </span>
        <h1 className="font-extrabold text-3xl text-text leading-tight tracking-tight sm:text-5xl">
          {title}
        </h1>
        {hero?.tagline != null && hero.tagline !== "" && (
          <p className="text-base text-muted sm:text-lg">{hero.tagline}</p>
        )}
        <div className="flex w-full flex-wrap gap-3">
          {telegramHref != null && (
            <TelegramButton
              href={telegramHref}
              label={telegramLabel}
              className="w-full sm:w-auto"
            />
          )}
          <a
            href={catalogHref}
            className="inline-flex w-full items-center justify-center rounded-xl border border-bg bg-landing-surface px-6 py-3 font-semibold text-base text-text transition hover:border-muted sm:w-auto"
          >
            {catalogLabel}
          </a>
        </div>
      </div>
    </div>
  );
}
