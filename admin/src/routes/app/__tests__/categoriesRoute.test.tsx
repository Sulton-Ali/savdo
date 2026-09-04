import type { components } from "@savdo/api-client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { cleanup, render, waitFor } from "@testing-library/react";
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
import { categoriesRoute } from "../categoriesRoute";
import { dashboardRoute } from "../dashboardRoute";

function buildRouter(initialEntry: string) {
  const queryClient = new QueryClient();
  const routeTree = rootRoute.addChildren([
    authenticatedRoute.addChildren([dashboardRoute, categoriesRoute]),
  ]);
  const router = createRouter({
    routeTree,
    context: { queryClient },
    history: createMemoryHistory({ initialEntries: [initialEntry] }),
  });
  return { router, queryClient };
}

describe("categoriesRoute beforeLoad", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  afterEach(() => {
    cleanup();
  });

  it("redirects a cashier (no catalog.write) away from /categories", async () => {
    fetchMeMock.mockResolvedValueOnce(buildMe([]));
    const { router, queryClient } = buildRouter("/categories");
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/");
    });
  });

  it("loads /categories for a manager with catalog.write", async () => {
    fetchMeMock.mockResolvedValueOnce(buildMe(["catalog.write"]));
    const { router, queryClient } = buildRouter("/categories");
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/categories");
    });
  });
});
