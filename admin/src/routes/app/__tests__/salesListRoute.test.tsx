import type { components } from "@savdo/api-client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";

type Me = components["schemas"]["Me"];

function buildMe(role: "owner" | "manager" | "cashier"): Me {
  return {
    user: {
      id: "u1",
      username: "user",
      fullName: "Test User",
      phone: null,
      role,
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
    permissions: [],
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
import { salesListRoute, validateSalesListSearch } from "../salesListRoute";

function buildRouter(initialEntry: string) {
  const queryClient = new QueryClient();
  const routeTree = rootRoute.addChildren([
    authenticatedRoute.addChildren([dashboardRoute, salesListRoute]),
  ]);
  const router = createRouter({
    routeTree,
    context: { queryClient },
    history: createMemoryHistory({ initialEntries: [initialEntry] }),
  });
  return { router, queryClient };
}

describe("salesListRoute beforeLoad", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  afterEach(() => {
    cleanup();
  });

  it("loads /sales for a cashier — no permission guard, every role may list every sale (D-63)", async () => {
    fetchMeMock.mockResolvedValueOnce(buildMe("cashier"));
    const { router, queryClient } = buildRouter("/sales");
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/sales");
    });
  });

  it("replaces the history entry (not pushes) on Reset, so Back leaves the page instead of undoing one filter", async () => {
    fetchMeMock.mockResolvedValueOnce(buildMe("owner"));
    const { router, queryClient } = buildRouter("/sales?locationId=l1");
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/sales");
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

describe("salesListRoute validateSearch", () => {
  const validateSearch = validateSalesListSearch;

  it("keeps every field when all are valid", () => {
    expect(
      validateSearch({
        from: "2026-01-05",
        to: "2026-01-10",
        locationId: "l1",
        kind: "return",
        status: "voided",
        cashierId: "u1",
        customerId: "c1",
      }),
    ).toEqual({
      from: "2026-01-05",
      to: "2026-01-10",
      locationId: "l1",
      kind: "return",
      status: "voided",
      cashierId: "u1",
      customerId: "c1",
    });
  });

  it("defaults every field to undefined when the search is empty", () => {
    expect(validateSearch({})).toEqual({
      from: undefined,
      to: undefined,
      locationId: undefined,
      kind: undefined,
      status: undefined,
      cashierId: undefined,
      customerId: undefined,
    });
  });

  it("drops an unknown kind, an unknown status, non-string ids and malformed dates", () => {
    expect(
      validateSearch({
        from: "2026/01/05",
        to: "2026-01-32",
        locationId: 7,
        kind: "not_a_real_kind",
        status: "not_a_real_status",
        cashierId: 42,
        customerId: "",
      }),
    ).toEqual({
      from: undefined,
      to: undefined,
      locationId: undefined,
      kind: undefined,
      status: undefined,
      cashierId: undefined,
      customerId: undefined,
    });
  });
});
