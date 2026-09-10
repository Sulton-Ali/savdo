import type { components } from "@savdo/api-client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

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

/**
 * `NavMenu` resolves the desktop/mobile shell via Ant Design's
 * `Grid.useBreakpoint`, which reads each breakpoint from
 * `window.matchMedia(...)` (min-width queries for everything but `xs`). The
 * global stub in `test/setup.ts` always reports `matches: false`, which
 * reads as "below every breakpoint" — fine for components that don't care,
 * but it would put every test below `lg` (mobile shell, sider replaced by a
 * closed, unmounted drawer) unless overridden. Mock it per test to
 * simulate desktop (`lg` and up match) or mobile (nothing matches, same as
 * the global stub).
 */
function mockMatchMedia(isDesktop: boolean) {
  vi.spyOn(window, "matchMedia").mockImplementation(
    (query: string) =>
      ({
        matches: isDesktop && query.includes("min-width"),
        media: query,
        onchange: null,
        addListener: () => {},
        removeListener: () => {},
        addEventListener: () => {},
        removeEventListener: () => {},
        dispatchEvent: () => false,
      }) as MediaQueryList,
  );
}

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

  beforeEach(() => {
    // Desktop by default — most of these tests are about permission
    // filtering, not responsive behaviour; the mobile-specific tests below
    // override this.
    mockMatchMedia(true);
  });

  // `vite.config.ts` does not set `test.globals`, so testing-library's
  // automatic per-test cleanup never registers — do it explicitly, since
  // this file renders more than once.
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
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
    expect(screen.getByRole("menuitem", { name: "Staff" })).toBeTruthy();
    expect(screen.getByRole("menuitem", { name: "Locations" })).toBeTruthy();
    expect(screen.getByRole("menuitem", { name: "Settings" })).toBeTruthy();
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
    expect(screen.queryByRole("menuitem", { name: "Staff" })).toBeNull();
    expect(screen.queryByRole("menuitem", { name: "Locations" })).toBeNull();
    expect(screen.queryByRole("menuitem", { name: "Settings" })).toBeNull();
    // The Settings *section* still shows: `/settings/telegram` has no
    // permission requirement (every role manages their own Telegram link,
    // Phase 7 T7), so the group is never fully empty for an authenticated
    // user — only the Settings and Landing content items are permission-gated.
    expect(screen.getByRole("menuitem", { name: "Telegram" })).toBeTruthy();
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
  // `docs/04-DATA-MODEL.md` § 7). "Bot" is both the group label and its
  // only item's label — `getByRole("menuitem", ...)` targets the item, not
  // the (non-interactive, `role="presentation"`) group header.
  it("shows Bot only with bot.read", async () => {
    fetchMeMock.mockResolvedValueOnce(buildMe(["bot.read"]));
    const { router, queryClient } = buildRouterAndClient();
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    await waitFor(() => expect(screen.getByText("Test Shop")).toBeTruthy());
    expect(screen.getByRole("menuitem", { name: "Bot" })).toBeTruthy();
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
    expect(screen.queryByRole("menuitem", { name: "Bot" })).toBeNull();
  });

  // D-122: grouped nav.
  describe("grouping", () => {
    it("renders a section header for a group with at least one visible item", async () => {
      fetchMeMock.mockResolvedValueOnce(buildMe(["catalog.write", "stock.write"]));
      const { router, queryClient } = buildRouterAndClient();
      render(
        <QueryClientProvider client={queryClient}>
          <RouterProvider router={router} />
        </QueryClientProvider>,
      );

      await waitFor(() => expect(screen.getByText("Test Shop")).toBeTruthy());
      // Neither group label collides with one of its own item labels here,
      // so a plain text query is unambiguous.
      expect(screen.getByText("Catalog")).toBeTruthy();
      expect(screen.getByText("Stock")).toBeTruthy();
      expect(screen.getByText("People")).toBeTruthy();
    });

    it("drops the Sales section header along with its items for nobody, but keeps it for every role (no permission on any sales item)", async () => {
      fetchMeMock.mockResolvedValueOnce(buildMe([]));
      const { router, queryClient } = buildRouterAndClient();
      render(
        <QueryClientProvider client={queryClient}>
          <RouterProvider router={router} />
        </QueryClientProvider>,
      );

      await waitFor(() => expect(screen.getByText("Test Shop")).toBeTruthy());
      // Every Sales item is permission: null, so the section always shows.
      expect(screen.getByRole("menuitem", { name: "Quick sale" })).toBeTruthy();
      expect(screen.getByRole("menuitem", { name: "Sale drafts" })).toBeTruthy();
    });

    it("highlights the parent item for a nested child route", async () => {
      // A route tree built just for this test: `/products/new` is a real
      // admin route, but wiring its full page and data dependencies here
      // would test unrelated code. A trivial stand-in component at the
      // same path is enough to prove the menu selection follows the
      // longest-prefix rule (D-122) from the router's actual pathname.
      const productNewStub = createRoute({
        getParentRoute: () => authenticatedRoute,
        path: "/products/new",
        component: () => <div>product form</div>,
      });
      const queryClient = new QueryClient();
      const routeTree = rootRoute.addChildren([
        authenticatedRoute.addChildren([dashboardRoute, productNewStub]),
      ]);
      const router = createRouter({
        routeTree,
        context: { queryClient },
        history: createMemoryHistory({ initialEntries: ["/products/new"] }),
      });
      fetchMeMock.mockResolvedValueOnce(buildMe([]));
      render(
        <QueryClientProvider client={queryClient}>
          <RouterProvider router={router} />
        </QueryClientProvider>,
      );

      await waitFor(() => expect(screen.getByText("Test Shop")).toBeTruthy());
      const productsItem = screen.getByRole("menuitem", { name: "Products" });
      expect(productsItem.className).toContain("ant-menu-item-selected");
    });
  });

  // D-122: fixed shell.
  describe("fixed shell", () => {
    it("scrolls the content area, not the document body", async () => {
      fetchMeMock.mockResolvedValueOnce(buildMe([]));
      const { router, queryClient } = buildRouterAndClient();
      const { container } = render(
        <QueryClientProvider client={queryClient}>
          <RouterProvider router={router} />
        </QueryClientProvider>,
      );

      await waitFor(() => expect(screen.getByText("Test Shop")).toBeTruthy());
      const content = container.querySelector(".ant-layout-content");
      expect(content).not.toBeNull();
      expect((content as HTMLElement).style.overflow).toBe("auto");
      expect(document.body.style.overflow).toBe("");
    });
  });

  // D-122: mobile drawer.
  describe("below the lg breakpoint", () => {
    beforeEach(() => {
      mockMatchMedia(false);
    });

    it("does not render the sider, and opens the drawer with the nav from the header menu button", async () => {
      fetchMeMock.mockResolvedValueOnce(buildMe(["staff.manage"]));
      const { router, queryClient } = buildRouterAndClient();
      const { container } = render(
        <QueryClientProvider client={queryClient}>
          <RouterProvider router={router} />
        </QueryClientProvider>,
      );

      const menuButton = await screen.findByRole("button", { name: "Menu" });
      expect(container.querySelector(".ant-layout-sider")).toBeNull();
      expect(screen.queryByRole("menuitem", { name: "Staff" })).toBeNull();

      fireEvent.click(menuButton);

      const staffItem = await screen.findByRole("menuitem", { name: "Staff" });
      expect(staffItem).toBeTruthy();
    });

    it("closes the drawer and returns focus to the menu button after a nav click", async () => {
      fetchMeMock.mockResolvedValueOnce(buildMe([]));
      const { router, queryClient } = buildRouterAndClient();
      render(
        <QueryClientProvider client={queryClient}>
          <RouterProvider router={router} />
        </QueryClientProvider>,
      );

      const menuButton = await screen.findByRole("button", { name: "Menu" });
      // A real click on a native `<button>` focuses it; `fireEvent.click`
      // does not simulate that, so focus it explicitly to match what the
      // Drawer's built-in `focusTriggerAfterClose` (Ant Design default)
      // actually restores focus to.
      menuButton.focus();
      fireEvent.click(menuButton);
      const productsItem = await screen.findByRole("menuitem", { name: "Products" });
      fireEvent.click(productsItem);

      await waitFor(() => expect(screen.queryByRole("menuitem", { name: "Products" })).toBeNull());
      await waitFor(() => expect(document.activeElement).toBe(menuButton));
    });
  });
});
