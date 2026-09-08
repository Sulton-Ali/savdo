import type { components } from "@savdo/api-client";

type PublicCategory = components["schemas"]["PublicCategory"];

export interface CategoryGroup {
  root: PublicCategory;
  children: PublicCategory[];
}

/**
 * Groups a flat `GET /public/categories` list (T7: parents sorted
 * immediately before their children, `productCount` already aggregated
 * over descendants — see D-99, T7) into parent/children groups for the
 * home page's category section.
 *
 * Rules:
 * - Roots keep the API's order.
 * - An item whose `parentSlug` does not match any slug in the list (its
 *   parent was filtered out or never existed) is treated as its own root
 *   — never dropped and never grouped under a stranger.
 * - A category with `productCount === 0` is hidden, whether it is a root
 *   or a child — except a root that has at least one visible (non-zero)
 *   child still shows as a group, even if the root's own aggregate is 0
 *   (defensive: T7's aggregation should make that impossible in practice,
 *   but the helper does not rely on it).
 */
export function groupCategories(items: readonly PublicCategory[]): CategoryGroup[] {
  const bySlug = new Map(items.map((item) => [item.slug, item]));

  const rootOrder: string[] = [];
  const groups = new Map<string, CategoryGroup>();

  for (const item of items) {
    const hasKnownParent = item.parentSlug != null && bySlug.has(item.parentSlug);
    if (!hasKnownParent) {
      if (!groups.has(item.slug)) {
        rootOrder.push(item.slug);
      }
      groups.set(item.slug, { root: item, children: groups.get(item.slug)?.children ?? [] });
    }
  }

  for (const item of items) {
    const parentSlug = item.parentSlug;
    const hasKnownParent = parentSlug != null && bySlug.has(parentSlug);
    if (hasKnownParent && parentSlug != null) {
      const group = groups.get(parentSlug);
      if (group) {
        group.children.push(item);
      }
    }
  }

  return rootOrder
    .map((slug) => groups.get(slug))
    .filter((group): group is CategoryGroup => group != null)
    .map((group) => ({
      ...group,
      children: group.children.filter((child) => child.productCount > 0),
    }))
    .filter((group) => group.root.productCount > 0 || group.children.length > 0);
}

/**
 * The disambiguated name for a category that is a child ("Ko'ylaklar —
 * Erkaklar"); a root category's own name unchanged.
 */
export function categoryDisplayName(category: PublicCategory): string {
  return category.parentName != null ? `${category.name} — ${category.parentName}` : category.name;
}
