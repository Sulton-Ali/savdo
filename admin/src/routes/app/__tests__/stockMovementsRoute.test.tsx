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
import { stockMovementsRoute, validateStockMovementsSearch } from "../stockMovementsRoute";

function buildRouter(initialEntry: string) {
  const queryClient = new QueryClient();
  const routeTree = rootRoute.addChildren([
    authenticatedRoute.addChildren([dashboardRoute, stockMovementsRoute]),
  ]);
  const router = createRouter({
    routeTree,
    context: { queryClient },
    history: createMemoryHistory({ initialEntries: [initialEntry] }),
  });
  return { router, queryClient };
}

describe("stockMovementsRoute beforeLoad", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  afterEach(() => {
    cleanup();
  });

  it("redirects a cashier (no stock.write) away from /stock/movements", async () => {
    fetchMeMock.mockResolvedValueOnce(buildMe([]));
    const { router, queryClient } = buildRouter("/stock/movements");
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/");
    });
  });

  it("loads /stock/movements for a manager with stock.write", async () => {
    fetchMeMock.mockResolvedValueOnce(buildMe(["stock.write"]));
    const { router, queryClient } = buildRouter("/stock/movements");
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/stock/movements");
    });
  });

  it("replaces the history entry (not pushes) on a filter change, so Back leaves the page instead of undoing one filter", async () => {
    fetchMeMock.mockResolvedValueOnce(buildMe(["stock.write"]));
    const { router, queryClient } = buildRouter("/stock/movements?locationId=l1");
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/stock/movements");
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

describe("stockMovementsRoute validateSearch", () => {
  const validateSearch = validateStockMovementsSearch;

  it("keeps every field when all are valid", () => {
    expect(
      validateSearch({
        productId: "p1",
        variantId: "v1",
        locationId: "l1",
        kind: "adjustment",
        from: "2026-01-05",
        to: "2026-01-10",
      }),
    ).toEqual({
      productId: "p1",
      variantId: "v1",
      locationId: "l1",
      kind: "adjustment",
      from: "2026-01-05",
      to: "2026-01-10",
    });
  });

  it("defaults every field to undefined when the search is empty", () => {
    expect(validateSearch({})).toEqual({
      productId: undefined,
      variantId: undefined,
      locationId: undefined,
      kind: undefined,
      from: undefined,
      to: undefined,
    });
  });

  it("drops an unknown kind, non-string ids (including productId) and malformed dates", () => {
    expect(
      validateSearch({
        productId: 7,
        variantId: 42,
        locationId: "",
        kind: "not_a_real_kind",
        from: "2026/01/05",
        to: "2026-01-32",
      }),
    ).toEqual({
      productId: undefined,
      variantId: undefined,
      locationId: undefined,
      kind: undefined,
      from: undefined,
      to: undefined,
    });
  });
});
