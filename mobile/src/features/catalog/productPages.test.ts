import { describe, expect, it } from "vitest";

import { flattenProductPages } from "./productPages";

describe("flattenProductPages", () => {
  // Regression for the bug the owner reported (T20, D-92 follow-up): the
  // variant picker (stock levels, adjustment, quick sale, new purchase) ran
  // a plain `useQuery` under the same `catalogKeys.products` key the
  // Products tab's `useProductsSearch` (`useInfiniteQuery`) uses, expecting
  // a `{ items }` response instead of `{ pages, pageParams }`. TanStack
  // Query keeps one cache entry per key, so whichever query populated the
  // cache last decided the shape the other consumer read; the loser saw
  // `data.items === undefined` and rendered an empty list. The fix routes
  // every consumer through `useProductsSearch` and this flattening
  // function, so there is exactly one shape at that key.
  it("concatenates items across pages in page order", () => {
    const pages = [{ items: [{ id: "1" }, { id: "2" }] }, { items: [{ id: "3" }] }];
    expect(flattenProductPages(pages)).toEqual([{ id: "1" }, { id: "2" }, { id: "3" }]);
  });

  it("returns an empty array while the query is still pending (pages undefined)", () => {
    expect(flattenProductPages(undefined)).toEqual([]);
  });

  it("returns an empty array for a single page with no items, not undefined", () => {
    expect(flattenProductPages([{ items: [] }])).toEqual([]);
  });

  it("works for any page item shape, not just products", () => {
    const pages = [{ items: [1, 2] }, { items: [3] }];
    expect(flattenProductPages(pages)).toEqual([1, 2, 3]);
  });
});
