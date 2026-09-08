import type { components } from "@savdo/api-client";
import { Link } from "@tanstack/react-router";

import type { Locale } from "../lib/locale";

type PublicCategory = components["schemas"]["PublicCategory"];

export function CategoryCard({ category, locale }: { category: PublicCategory; locale: Locale }) {
  return (
    <Link
      to="/$locale/c/$slug"
      params={{ locale, slug: category.slug }}
      className="flex flex-col items-center gap-1 rounded-md border border-bg bg-surface px-4 py-6 text-center shadow-sm transition hover:border-primary hover:shadow-md"
    >
      <span className="font-medium text-text">{category.name}</span>
      <span className="text-muted text-xs">{category.productCount}</span>
    </Link>
  );
}
