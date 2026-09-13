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
      username: "manager",
      fullName: "Test Manager",
      phone: null,
      role: permissions.length > 0 ? "manager" : "cashier",
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
import { purchasesRoute, validatePurchasesSearch } from "../purchasesRoute";

function buildRouter(initialEntry: string) {
  const queryClient = new QueryClient();
  const routeTree = rootRoute.addChildren([
    authenticatedRoute.addChildren([dashboardRoute, purchasesRoute]),
  ]);
  const router = createRouter({
    routeTree,
    context: { queryClient },
    history: createMemoryHistory({ initialEntries: [initialEntry] }),
  });
  return { router, queryClient };
}

describe("purchasesRoute beforeLoad", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  afterEach(() => {
    cleanup();
  });

  it("redirects to / when the user lacks stock.write", async () => {
    fetchMeMock.mockResolvedValueOnce(buildMe([]));
    const { router, queryClient } = buildRouter("/purchases");
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/");
    });
  });

  it("loads /purchases when the user has stock.write", async () => {
    fetchMeMock.mockResolvedValueOnce(buildMe(["stock.write"]));
    const { router, queryClient } = buildRouter("/purchases");
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/purchases");
    });
  });

  it("replaces the history entry (not pushes) on Reset, so Back leaves the page instead of undoing one filter", async () => {
    fetchMeMock.mockResolvedValueOnce(buildMe(["stock.write"]));
    const { router, queryClient } = buildRouter("/purchases?supplierId=sup1");
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/purchases");
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

describe("purchasesRoute validateSearch", () => {
  const validateSearch = validatePurchasesSearch;

  it("keeps every field when all are valid", () => {
    expect(validateSearch({ status: "received", supplierId: "sup1" })).toEqual({
      status: "received",
      supplierId: "sup1",
    });
  });

  it("defaults every field to undefined when the search is empty", () => {
    expect(validateSearch({})).toEqual({ status: undefined, supplierId: undefined });
  });

  it("drops an unknown status and a non-string supplierId", () => {
    expect(validateSearch({ status: "not_a_real_status", supplierId: 7 })).toEqual({
      status: undefined,
      supplierId: undefined,
    });
  });
});
