import { describe, expect, it } from "vitest";

import { selectedNavKeys } from "../NavMenu";

// The same key set `NavMenu` builds internally (dashboard + every group's
// item keys), reproduced here so this test stays a pure, router-free check
// of the longest-prefix matching rule (D-122).
const keys = [
  "/",
  "/quick-sale",
  "/sales",
  "/sales/drafts",
  "/products",
  "/categories",
  "/settings/attributes",
  "/stock",
  "/stock/movements",
  "/stock/low",
  "/customers",
  "/suppliers",
  "/purchases",
  "/staff",
  "/locations",
  "/reports",
  "/bot/conversations",
  "/settings",
  "/settings/landing",
  "/settings/telegram",
];

describe("selectedNavKeys", () => {
  it("matches the dashboard only at the exact root path", () => {
    expect(selectedNavKeys("/", keys)).toEqual(["/"]);
    // Every path starts with "/" — the root key must not match everything.
    expect(selectedNavKeys("/products", keys)).toEqual(["/products"]);
  });

  it("highlights the parent for a nested product route", () => {
    expect(selectedNavKeys("/products/new", keys)).toEqual(["/products"]);
    expect(selectedNavKeys("/products/abc-123/edit", keys)).toEqual(["/products"]);
  });

  it("highlights the parent for a nested sale route, not the longer sibling key", () => {
    expect(selectedNavKeys("/sales/123", keys)).toEqual(["/sales"]);
  });

  it("keeps a route that is itself a key selected, over its shorter prefix", () => {
    expect(selectedNavKeys("/sales/drafts", keys)).toEqual(["/sales/drafts"]);
    expect(selectedNavKeys("/sales/drafts/1", keys)).toEqual(["/sales/drafts"]);
  });

  it("returns no match for a path outside the nav", () => {
    expect(selectedNavKeys("/login", keys)).toEqual([]);
  });
});
