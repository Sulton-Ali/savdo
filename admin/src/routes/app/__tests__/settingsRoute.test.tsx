import type { components } from "@savdo/api-client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App as AntApp } from "antd";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

type Me = components["schemas"]["Me"];

function buildMe(shopName: string): Me {
  return {
    user: {
      id: "u1",
      username: "owner",
      fullName: "Test Owner",
      phone: null,
      role: "owner",
      locale: "en",
      isActive: true,
      lastLoginAt: null,
      createdAt: "2026-01-01T00:00:00Z",
    },
    shop: {
      id: "s1",
      slug: "test-shop",
      name: shopName,
      currency: "UZS",
      timezone: "Asia/Tashkent",
      defaultLocale: "en",
      allowNegativeStock: false,
      updateCostOnPurchase: true,
      lowStockThreshold: 2,
    },
    permissions: ["shop.settings"],
  };
}

const fetchMeMock = vi.fn<() => Promise<Me>>();

vi.mock("../../../auth/api", () => ({
  fetchMe: () => fetchMeMock(),
}));

vi.mock("../../../lib/api", () => ({
  api: { GET: vi.fn(), POST: vi.fn(), PATCH: vi.fn() },
}));

import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { rootRoute } from "../../root";
import { authenticatedRoute } from "../authenticatedRoute";
import { settingsRoute } from "../settingsRoute";

const mockedApi = vi.mocked(api, { deep: true });

function buildRouter() {
  const queryClient = new QueryClient();
  const routeTree = rootRoute.addChildren([authenticatedRoute.addChildren([settingsRoute])]);
  const router = createRouter({
    routeTree,
    context: { queryClient },
    history: createMemoryHistory({ initialEntries: ["/settings"] }),
  });
  return { router, queryClient };
}

describe("settings save reflects in the sider without a navigation", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    mockedApi.GET.mockReset();
    mockedApi.PATCH.mockReset();
    fetchMeMock.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("updates the sider's shop name after PATCH /shop succeeds", async () => {
    const oldMe = buildMe("Old Shop Name");
    const newMe = buildMe("New Shop Name");

    // The route guard's `beforeLoad` fetches `me` once on load; every call
    // after the settings save (triggered by the invalidate + `useMe()`
    // subscription in AppLayout, and by `router.invalidate()`) should see
    // the new name.
    fetchMeMock.mockResolvedValueOnce(oldMe);
    fetchMeMock.mockResolvedValue(newMe);

    mockedApi.GET.mockResolvedValueOnce({
      data: oldMe.shop,
      error: undefined,
      response: new Response(null, { status: 200 }),
    } as never);
    mockedApi.PATCH.mockResolvedValueOnce({
      data: newMe.shop,
      error: undefined,
      response: new Response(null, { status: 200 }),
    } as never);

    const { router, queryClient } = buildRouter();
    render(
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <RouterProvider router={router} />
        </AntApp>
      </QueryClientProvider>,
    );

    // Sider shows the shop name from the initial `me` before any save.
    expect(await screen.findByText("Old Shop Name")).toBeTruthy();

    const nameInput = await screen.findByDisplayValue("Old Shop Name");
    fireEvent.change(nameInput, { target: { value: "New Shop Name" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mockedApi.PATCH).toHaveBeenCalled();
    });

    await waitFor(() => {
      expect(screen.getByText("New Shop Name")).toBeTruthy();
    });
    expect(screen.queryByText("Old Shop Name")).toBeNull();
  });
});
