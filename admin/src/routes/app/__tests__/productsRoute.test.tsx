import type { components } from "@savdo/api-client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";

type Me = components["schemas"]["Me"];

function buildMe(permissions: string[]): Me {
  return {
    user: {
      id: "u1",
      username: "cashier",
      fullName: "Test Cashier",
      phone: null,
      role: "cashier",
      locale: "en",
      isActive: true,
      lastLoginAt: null,
      createdAt: "2026-01-01T00:00:00Z",
    },
    shop: {
      id: "s1",
      slug: "test-shop",
      name: "Test Shop",
      currency: "UZS",
      timezone: "Asia/Tashkent",
      defaultLocale: "en",
      allowNegativeStock: false,
      updateCostOnPurchase: true,
      lowStockThreshold: 2,
    },
    permissions,
  };
}

const fetchMeMock = vi.fn<() => Promise<Me>>();

vi.mock("../../../auth/api", () => ({
  fetchMe: () => fetchMeMock(),
}));

vi.mock("../../../lib/api", () => ({
  api: {
    GET: vi.fn(async () => ({
      data: { items: [], nextCursor: null },
      error: undefined,
      response: new Response(null, { status: 200 }),
    })),
    POST: vi.fn(),
    PATCH: vi.fn(),
    DELETE: vi.fn(),
  },
}));

import { i18next } from "../../../i18n";
import { rootRoute } from "../../root";
import { authenticatedRoute } from "../authenticatedRoute";
import { dashboardRoute } from "../dashboardRoute";
import { productsRoute, validateProductsSearch } from "../productsRoute";

function buildRouter(initialEntry: string) {
  const queryClient = new QueryClient();
  const routeTree = rootRoute.addChildren([
    authenticatedRoute.addChildren([dashboardRoute, productsRoute]),
  ]);
  const router = createRouter({
    routeTree,
    context: { queryClient },
    history: createMemoryHistory({ initialEntries: [initialEntry] }),
  });
  return { router, queryClient };
}

describe("productsRoute beforeLoad", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  afterEach(() => {
    cleanup();
  });

  it("loads /products for a cashier — no permission guard", async () => {
    fetchMeMock.mockResolvedValueOnce(buildMe([]));
    const { router, queryClient } = buildRouter("/products");
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/products");
    });
  });

  it("replaces the history entry (not pushes) on Reset, so Back leaves the page instead of undoing one filter", async () => {
    fetchMeMock.mockResolvedValueOnce(buildMe(["catalog.write"]));
    const { router, queryClient } = buildRouter("/products?q=shirt");
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/products");
    });

    const replaceSpy = vi.spyOn(router.history, "replace");
    const pushSpy = vi.spyOn(router.history, "push");

    fireEvent.click(screen.getByRole("button", { name: "Reset" }));

    await waitFor(() => {
      expect(replaceSpy).toHaveBeenCalled();
    });
    expect(pushSpy).not.toHaveBeenCalled();
  });
});

describe("productsRoute validateSearch", () => {
  const validateSearch = validateProductsSearch;

  it("keeps every field when all are valid", () => {
    expect(validateSearch({ q: "  shirt  ", categoryId: "c1", includeInactive: true })).toEqual({
      q: "shirt",
      categoryId: "c1",
      includeInactive: true,
    });
  });

  it("accepts the string form 'true'/'false' for includeInactive", () => {
    expect(validateSearch({ includeInactive: "true" }).includeInactive).toBe(true);
    expect(validateSearch({ includeInactive: "false" }).includeInactive).toBe(false);
  });

  it("defaults every field to undefined when the search is empty", () => {
    expect(validateSearch({})).toEqual({
      q: undefined,
      categoryId: undefined,
      includeInactive: undefined,
    });
  });

  it("drops an empty/whitespace-only q, a non-string categoryId and an invalid includeInactive", () => {
    expect(validateSearch({ q: "   ", categoryId: 7, includeInactive: "maybe" })).toEqual({
      q: undefined,
      categoryId: undefined,
      includeInactive: undefined,
    });
  });

  it("truncates an over-long q to 200 characters rather than dropping it", () => {
    const long = "a".repeat(250);
    const result = validateSearch({ q: long });
    expect(result.q).toHaveLength(200);
    expect(result.q).toBe("a".repeat(200));
  });
});
