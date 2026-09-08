import type { components } from "@savdo/api-client";
import { describe, expect, it } from "vitest";

import { categoryDisplayName, groupCategories } from "./categories";

type PublicCategory = components["schemas"]["PublicCategory"];

function category(
  overrides: Partial<PublicCategory> & { id: string; slug: string },
): PublicCategory {
  return {
    name: overrides.slug,
    parentSlug: null,
    parentName: null,
    productCount: 1,
    ...overrides,
  };
}

describe("groupCategories", () => {
  it("keeps roots in API order", () => {
    const men = category({ id: "1", slug: "men", name: "Erkaklar" });
    const women = category({ id: "2", slug: "women", name: "Ayollar" });
    const kids = category({ id: "3", slug: "kids", name: "Bolalar" });

    const groups = groupCategories([women, kids, men]);

    expect(groups.map((g) => g.root.slug)).toEqual(["women", "kids", "men"]);
  });

  it("puts children under their root, preserving order", () => {
    const men = category({ id: "1", slug: "men", name: "Erkaklar", productCount: 5 });
    const shirts = category({
      id: "2",
      slug: "men-shirts",
      name: "Ko'ylaklar",
      parentSlug: "men",
      parentName: "Erkaklar",
      productCount: 3,
    });
    const pants = category({
      id: "3",
      slug: "men-pants",
      name: "Shimlar",
      parentSlug: "men",
      parentName: "Erkaklar",
      productCount: 2,
    });

    const groups = groupCategories([men, shirts, pants]);

    expect(groups).toHaveLength(1);
    expect(groups[0]?.root.slug).toBe("men");
    expect(groups[0]?.children.map((c) => c.slug)).toEqual(["men-shirts", "men-pants"]);
  });

  it("treats an orphan (parent not in the list) as its own root", () => {
    const shirts = category({
      id: "1",
      slug: "shirts",
      name: "Ko'ylaklar",
      parentSlug: "missing-parent",
      parentName: "Ghost",
      productCount: 4,
    });

    const groups = groupCategories([shirts]);

    expect(groups).toHaveLength(1);
    expect(groups[0]?.root.slug).toBe("shirts");
    expect(groups[0]?.children).toEqual([]);
  });

  it("hides zero-count categories but keeps a root that still has visible children", () => {
    const men = category({ id: "1", slug: "men", name: "Erkaklar", productCount: 0 });
    const shirts = category({
      id: "2",
      slug: "men-shirts",
      name: "Ko'ylaklar",
      parentSlug: "men",
      parentName: "Erkaklar",
      productCount: 3,
    });
    const empty = category({
      id: "3",
      slug: "men-hats",
      name: "Bosh kiyimlar",
      parentSlug: "men",
      parentName: "Erkaklar",
      productCount: 0,
    });
    const emptyRoot = category({ id: "4", slug: "sale", name: "Chegirma", productCount: 0 });

    const groups = groupCategories([men, shirts, empty, emptyRoot]);

    expect(groups).toHaveLength(1);
    expect(groups[0]?.root.slug).toBe("men");
    expect(groups[0]?.children.map((c) => c.slug)).toEqual(["men-shirts"]);
  });
});

describe("categoryDisplayName", () => {
  it("returns the plain name for a root category", () => {
    expect(categoryDisplayName(category({ id: "1", slug: "men", name: "Erkaklar" }))).toBe(
      "Erkaklar",
    );
  });

  it("disambiguates a child category with its parent's name", () => {
    const shirts = category({
      id: "2",
      slug: "men-shirts",
      name: "Ko'ylaklar",
      parentSlug: "men",
      parentName: "Erkaklar",
    });
    expect(categoryDisplayName(shirts)).toBe("Ko'ylaklar — Erkaklar");
  });
});
