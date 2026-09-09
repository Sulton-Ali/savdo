import type { components } from "@savdo/api-client";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import type { Locale } from "../lib/locale";
import { ArrowRightIcon, HangerIcon } from "./icons";
import { hashSeed, placeholderTone } from "./landing/PlaceholderPhoto";

type PublicCategory = components["schemas"]["PublicCategory"];

const TONE_ICON_CLASS: Record<string, string> = {
  peach: "bg-[#fbe2d9]",
  teal: "bg-[#d5ecea]",
  sand: "bg-[#f1e8dc]",
  lilac: "bg-[#e6e3f2]",
};

/**
 * A category row card (D-121, Variant B): icon tile, name + product count,
 * teal "go" arrow. The icon tile's pastel background is picked
 * deterministically from the category's own slug (`hashSeed` +
 * `placeholderTone`) — categories carry no colour/icon of their own
 * (`PublicCategory` has no such field), so this is decoration only, not
 * data, and stays stable across renders without a caller-supplied index.
 */
export function CategoryCard({ category, locale }: { category: PublicCategory; locale: Locale }) {
  const { t } = useTranslation();
  const tone = placeholderTone(hashSeed(category.slug));
  return (
    <Link
      to="/$locale/c/$slug"
      params={{ locale, slug: category.slug }}
      className="flex items-center gap-4 rounded-landing-card bg-landing-surface p-3.5 shadow-landing-card transition hover:brightness-[0.98]"
    >
      <span
        aria-hidden="true"
        className={`flex h-16 w-16 shrink-0 items-center justify-center rounded-xl text-[#6d4d43] sm:h-20 sm:w-20 ${TONE_ICON_CLASS[tone]}`}
      >
        <HangerIcon className="h-8 w-8 sm:h-10 sm:w-10" />
      </span>
      <span className="flex flex-1 flex-col gap-0.5">
        <span className="font-bold text-lg text-text">{category.name}</span>
        <span className="text-muted text-sm">
          {t("web.home.categoryProductCount", { count: category.productCount })}
        </span>
      </span>
      <span className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-[#d5ecea] text-landing-secondary">
        <ArrowRightIcon className="h-4 w-4" />
      </span>
    </Link>
  );
}
