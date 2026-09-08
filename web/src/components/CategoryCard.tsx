import type { components } from "@savdo/api-client";
import { Link } from "@tanstack/react-router";

import type { Locale } from "../lib/locale";

type PublicCategory = components["schemas"]["PublicCategory"];

export function CategoryCard({ category, locale }: { category: PublicCategory; locale: Locale }) {
  return (
    <Link
      to="/$locale/c/$slug"
      params={{ locale, slug: category.slug }}
      className="flex flex-col gap-1 rounded-md border border-bg p-4 text-center transition hover:border-primary"
    >
      <span className="font-medium text-text">{category.name}</span>
      <span className="text-muted text-sm">{category.productCount}</span>
    </Link>
  );
}
