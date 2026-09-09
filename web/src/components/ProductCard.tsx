import type { components } from "@savdo/api-client";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import type { Locale } from "../lib/locale";
import { AvailabilityBadge } from "./AvailabilityBadge";
import { CoverImage } from "./CoverImage";
import { HangerIcon } from "./icons";
import { hashSeed, PlaceholderPhoto, placeholderTone } from "./landing/PlaceholderPhoto";
import { PriceTag } from "./PriceTag";

type PublicProductListItem = components["schemas"]["PublicProductListItem"];

/**
 * A product card (D-121, Variant B: white rounded card, soft shadow). Falls
 * back to the shared `PlaceholderPhoto` (not `CoverImage`'s own plain box)
 * when the product has no cover image, so an imageless product still gets
 * Variant B's coloured-shape-plus-label placeholder instead of a flat tint.
 */
export function ProductCard({
  product,
  locale,
  currency,
}: {
  product: PublicProductListItem;
  locale: Locale;
  currency: string;
}) {
  const { t } = useTranslation();
  return (
    <Link
      to="/$locale/p/$slug"
      params={{ locale, slug: product.slug }}
      className="flex flex-col gap-2.5 rounded-landing-card bg-landing-surface p-3 shadow-landing-card transition hover:brightness-[0.98]"
    >
      {product.coverImage != null ? (
        <CoverImage
          image={product.coverImage.urls}
          size="card"
          aspect="portrait"
          alt={product.name}
        />
      ) : (
        <PlaceholderPhoto
          aspect="portrait"
          tone={placeholderTone(hashSeed(product.slug))}
          label={t("web.home.productPhotoPlaceholder")}
          icon={<HangerIcon className="h-14 w-14" />}
        />
      )}
      <span className="line-clamp-2 font-medium text-text">{product.name}</span>
      <PriceTag price={product.price} currency={currency} />
      <AvailabilityBadge value={product.availability} />
    </Link>
  );
}
