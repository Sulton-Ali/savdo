import type { components } from "@savdo/api-client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";

type Me = components["schemas"]["Me"];

function buildMe(role: Me["user"]["role"]): Me {
  return {
    user: {
      id: "u1",
      username: "owner",
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
    },
    permissions: [],
  };
}

const fetchMeMock = vi.fn<() => Promise<Me>>();

vi.mock("../../../auth/api", () => ({
  fetchMe: () => fetchMeMock(),
}));

import { i18next } from "../../../i18n";
import { rootRoute } from "../../root";
import { authenticatedRoute } from "../authenticatedRoute";
import { dashboardRoute } from "../dashboardRoute";

function buildRouterAndClient() {
  const queryClient = new QueryClient();
  const routeTree = rootRoute.addChildren([authenticatedRoute.addChildren([dashboardRoute])]);
  const router = createRouter({
    routeTree,
    context: { queryClient },
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  return { router, queryClient };
}

describe("AppLayout navigation", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  // `vite.config.ts` does not set `test.globals`, so testing-library's
  // automatic per-test cleanup never registers — do it explicitly, since
  // this file renders more than once.
  afterEach(() => {
    cleanup();
  });

  it("shows the Staff nav item for an owner", async () => {
    fetchMeMock.mockResolvedValueOnce(buildMe("owner"));
    const { router, queryClient } = buildRouterAndClient();
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    await waitFor(() => expect(screen.getByText("Test Shop")).toBeTruthy());
    expect(screen.getByText("Staff")).toBeTruthy();
  });

  it("hides the Staff nav item for a cashier", async () => {
    fetchMeMock.mockResolvedValueOnce(buildMe("cashier"));
    const { router, queryClient } = buildRouterAndClient();
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    await waitFor(() => expect(screen.getByText("Test Shop")).toBeTruthy());
    expect(screen.queryByText("Staff")).toBeNull();
  });
});
