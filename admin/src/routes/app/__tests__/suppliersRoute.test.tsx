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
import { suppliersRoute, validateSuppliersSearch } from "../suppliersRoute";

function buildRouter(initialEntry: string) {
  const queryClient = new QueryClient();
  const routeTree = rootRoute.addChildren([
    authenticatedRoute.addChildren([dashboardRoute, suppliersRoute]),
  ]);
  const router = createRouter({
    routeTree,
    context: { queryClient },
    history: createMemoryHistory({ initialEntries: [initialEntry] }),
  });
  return { router, queryClient };
}

describe("suppliersRoute beforeLoad", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  afterEach(() => {
    cleanup();
  });

  it("redirects a cashier (no suppliers.manage) away from /suppliers", async () => {
    fetchMeMock.mockResolvedValueOnce(buildMe([]));
    const { router, queryClient } = buildRouter("/suppliers");
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/");
    });
  });

  it("loads /suppliers for a manager with suppliers.manage", async () => {
    fetchMeMock.mockResolvedValueOnce(buildMe(["suppliers.manage"]));
    const { router, queryClient } = buildRouter("/suppliers");
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/suppliers");
    });
  });

  it("replaces the history entry (not pushes) on Reset, so Back leaves the page instead of undoing one filter", async () => {
    fetchMeMock.mockResolvedValueOnce(buildMe(["suppliers.manage"]));
    const { router, queryClient } = buildRouter("/suppliers?q=acme");
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/suppliers");
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

describe("suppliersRoute validateSearch", () => {
  const validateSearch = validateSuppliersSearch;

  it("keeps a valid, non-empty trimmed q", () => {
    expect(validateSearch({ q: "  acme  " })).toEqual({ q: "acme" });
  });

  it("defaults q to undefined when the search is empty", () => {
    expect(validateSearch({})).toEqual({ q: undefined });
  });

  it("drops an empty or whitespace-only q, and a non-string q", () => {
    expect(validateSearch({ q: "" })).toEqual({ q: undefined });
    expect(validateSearch({ q: "   " })).toEqual({ q: undefined });
    expect(validateSearch({ q: 42 })).toEqual({ q: undefined });
  });

  it("truncates an over-long q to 200 characters rather than dropping it (a long paste is still a usable search prefix)", () => {
    const long = "a".repeat(250);
    const result = validateSearch({ q: long });
    expect(result.q).toHaveLength(200);
    expect(result.q).toBe("a".repeat(200));
  });
});
