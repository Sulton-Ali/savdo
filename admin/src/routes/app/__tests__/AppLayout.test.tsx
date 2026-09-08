import type { components } from "@savdo/api-client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";

type Me = components["schemas"]["Me"];

function buildMe(permissions: string[]): Me {
  return {
    user: {
      id: "u1",
      username: "owner",
      fullName: "Test User",
      phone: null,
      role: permissions.length > 0 ? "owner" : "cashier",
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

  it("shows the Staff nav item when the user has staff.manage", async () => {
    fetchMeMock.mockResolvedValueOnce(
      buildMe(["staff.manage", "locations.manage", "shop.settings"]),
    );
    const { router, queryClient } = buildRouterAndClient();
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    await waitFor(() => expect(screen.getByText("Test Shop")).toBeTruthy());
    expect(screen.getByText("Staff")).toBeTruthy();
    expect(screen.getByText("Locations")).toBeTruthy();
    expect(screen.getByText("Settings")).toBeTruthy();
  });

  it("hides the Staff, Locations and Settings nav items without the matching permissions", async () => {
    fetchMeMock.mockResolvedValueOnce(buildMe([]));
    const { router, queryClient } = buildRouterAndClient();
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    await waitFor(() => expect(screen.getByText("Test Shop")).toBeTruthy());
    expect(screen.queryByText("Staff")).toBeNull();
    expect(screen.queryByText("Locations")).toBeNull();
    expect(screen.queryByText("Settings")).toBeNull();
  });

  it("shows Products to every role but Categories/Attributes only with catalog.write", async () => {
    fetchMeMock.mockResolvedValueOnce(buildMe([]));
    const { router, queryClient } = buildRouterAndClient();
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    await waitFor(() => expect(screen.getByText("Test Shop")).toBeTruthy());
    expect(screen.getByText("Products")).toBeTruthy();
    expect(screen.queryByText("Categories")).toBeNull();
    expect(screen.queryByText("Attributes")).toBeNull();
  });

  it("shows Categories and Attributes with catalog.write", async () => {
    fetchMeMock.mockResolvedValueOnce(buildMe(["catalog.write"]));
    const { router, queryClient } = buildRouterAndClient();
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    await waitFor(() => expect(screen.getByText("Test Shop")).toBeTruthy());
    expect(screen.getByText("Products")).toBeTruthy();
    expect(screen.getByText("Categories")).toBeTruthy();
    expect(screen.getByText("Attributes")).toBeTruthy();
  });

  // Phase 6 T4: landing content editor nav entry (manager+, D-99).
  it("shows Landing content only with content.manage", async () => {
    fetchMeMock.mockResolvedValueOnce(buildMe(["content.manage"]));
    const { router, queryClient } = buildRouterAndClient();
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    await waitFor(() => expect(screen.getByText("Test Shop")).toBeTruthy());
    expect(screen.getByText("Landing content")).toBeTruthy();
  });

  it("hides Landing content without content.manage", async () => {
    fetchMeMock.mockResolvedValueOnce(buildMe([]));
    const { router, queryClient } = buildRouterAndClient();
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    await waitFor(() => expect(screen.getByText("Test Shop")).toBeTruthy());
    expect(screen.queryByText("Landing content")).toBeNull();
  });

  // Phase 7 T6: bot conversations nav entry (owner/manager, `bot.read`,
  // `docs/04-DATA-MODEL.md` § 7).
  it("shows Bot only with bot.read", async () => {
    fetchMeMock.mockResolvedValueOnce(buildMe(["bot.read"]));
    const { router, queryClient } = buildRouterAndClient();
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    await waitFor(() => expect(screen.getByText("Test Shop")).toBeTruthy());
    expect(screen.getByText("Bot")).toBeTruthy();
  });

  it("hides Bot without bot.read (e.g. a cashier)", async () => {
    fetchMeMock.mockResolvedValueOnce(buildMe([]));
    const { router, queryClient } = buildRouterAndClient();
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    await waitFor(() => expect(screen.getByText("Test Shop")).toBeTruthy());
    expect(screen.queryByText("Bot")).toBeNull();
  });
});
