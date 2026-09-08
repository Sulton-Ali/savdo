import type { components } from "@savdo/api-client";
import { Link } from "@tanstack/react-router";

import type { Locale } from "../lib/locale";
import { AvailabilityBadge } from "./AvailabilityBadge";
import { CoverImage } from "./CoverImage";
import { PriceTag } from "./PriceTag";

type PublicProductListItem = components["schemas"]["PublicProductListItem"];

export function ProductCard({
  product,
  locale,
  currency,
}: {
  product: PublicProductListItem;
  locale: Locale;
  currency: string;
}) {
  return (
    <Link
      to="/$locale/p/$slug"
      params={{ locale, slug: product.slug }}
      className="flex flex-col gap-2 rounded-md border border-bg p-2 transition hover:border-primary"
    >
      <CoverImage image={product.coverImage?.urls} size="card" alt={product.name} />
      <span className="font-medium text-text">{product.name}</span>
      <PriceTag price={product.price} currency={currency} />
      <AvailabilityBadge value={product.availability} />
    </Link>
  );
}
