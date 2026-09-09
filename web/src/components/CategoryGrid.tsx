import type { components } from "@savdo/api-client";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { type CategoryGroup, groupCategories } from "../lib/categories";
import type { Locale } from "../lib/locale";
import { CategoryCard } from "./CategoryCard";

type PublicCategory = components["schemas"]["PublicCategory"];

const GRID_CLASS = "grid grid-cols-1 gap-4 lg:grid-cols-3";

type Block =
  | { type: "singles"; key: string; items: PublicCategory[] }
  | { type: "group"; key: string; group: CategoryGroup };

/**
 * Groups (`groupCategories`, T7) run into render blocks: consecutive roots
 * without children share one grid so they don't each sit alone on their
 * own row; a root with children gets its own labelled group (D-99).
 */
function toBlocks(groups: readonly CategoryGroup[]): Block[] {
  const blocks: Block[] = [];
  for (const group of groups) {
    if (group.children.length === 0) {
      const last = blocks.at(-1);
      if (last?.type === "singles") {
        last.items.push(group.root);
      } else {
        blocks.push({ type: "singles", key: `singles-${group.root.slug}`, items: [group.root] });
      }
    } else {
      blocks.push({ type: "group", key: group.root.slug, group });
    }
  }
  return blocks;
}

/**
 * The home page's category section (heading + grid together, so an empty
 * result hides both). Categories with `parentSlug` set are grouped under
 * their root ("Erkaklar" → Ko'ylaklar, Shimlar, Kurtkalar); a root with no
 * children renders as a plain card, same as before T7.
 */
export function CategoryGrid({
  categories,
  locale,
}: {
  categories: readonly PublicCategory[];
  locale: Locale;
}) {
  const { t } = useTranslation();
  const groups = groupCategories(categories);
  if (groups.length === 0) {
    return null;
  }
  const blocks = toBlocks(groups);

  return (
    <section>
      <h2 className="mb-5 font-bold text-2xl text-text tracking-tight sm:text-[28px]">
        {t("web.home.categoriesTitle")}
      </h2>
      <div className="flex flex-col gap-8">
        {blocks.map((block) =>
          block.type === "singles" ? (
            <div key={block.key} className={GRID_CLASS}>
              {block.items.map((category) => (
                <CategoryCard key={category.id} category={category} locale={locale} />
              ))}
            </div>
          ) : (
            <div key={block.key} className="flex flex-col gap-3">
              <Link
                to="/$locale/c/$slug"
                params={{ locale, slug: block.group.root.slug }}
                className="flex items-baseline gap-2 font-bold text-lg text-text hover:text-landing-secondary-hover"
              >
                {block.group.root.name}
                <span className="font-normal text-muted text-sm">
                  {t("web.home.categoryProductCount", { count: block.group.root.productCount })}
                </span>
              </Link>
              <div className={GRID_CLASS}>
                {block.group.children.map((category) => (
                  <CategoryCard key={category.id} category={category} locale={locale} />
                ))}
              </div>
            </div>
          ),
        )}
      </div>
    </section>
  );
}
