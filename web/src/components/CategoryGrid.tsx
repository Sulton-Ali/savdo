import type { components } from "@savdo/api-client";
import { useTranslation } from "react-i18next";

import type { Locale } from "../lib/locale";
import { CategoryCard } from "./CategoryCard";

type PublicCategory = components["schemas"]["PublicCategory"];

/**
 * The home page's category section (heading + grid together, so an empty
 * result hides both). Filters out empty categories (`productCount === 0`)
 * — the public API has no parent/child grouping yet (`PublicCategory`
 * carries no `parentSlug`), so an empty parent category would otherwise
 * show as a dead tile next to its own children. Takes the full,
 * unfiltered list and does the filtering itself so every caller gets the
 * same rule; grouping by parent later (once the API adds `parentSlug`) is
 * a small change inside this one component, not at each call site.
 */
export function CategoryGrid({
  categories,
  locale,
}: {
  categories: readonly PublicCategory[];
  locale: Locale;
}) {
  const { t } = useTranslation();
  const visible = categories.filter((category) => category.productCount > 0);
  if (visible.length === 0) {
    return null;
  }
  return (
    <section>
      <h2 className="mb-4 font-semibold text-text text-xl">{t("web.home.categoriesTitle")}</h2>
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4">
        {visible.map((category) => (
          <CategoryCard key={category.id} category={category} locale={locale} />
        ))}
      </div>
    </section>
  );
}
