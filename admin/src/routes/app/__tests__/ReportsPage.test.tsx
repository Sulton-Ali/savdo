import type { components } from "@savdo/api-client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App as AntApp, ConfigProvider } from "antd";
import dayjs from "dayjs";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

const navigateMock = vi.fn();

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>();
  return { ...actual, useNavigate: () => navigateMock };
});

vi.mock("../../../lib/api", () => ({
  api: { GET: vi.fn(), POST: vi.fn(), PATCH: vi.fn(), DELETE: vi.fn() },
}));

import { AuthProvider } from "../../../auth/AuthContext";
import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { ReportsPage } from "../ReportsPage";

type Me = components["schemas"]["Me"];
type Location = components["schemas"]["Location"];
type SalesSummaryReport = components["schemas"]["SalesSummaryReport"];
type SalesByProductRow = components["schemas"]["SalesByProductRow"];
type StockLowItem = components["schemas"]["StockLowItem"];

const mockedApi = vi.mocked(api, { deep: true });

function ok<T>(data: T, status = 200) {
  return { data, error: undefined, response: new Response(null, { status }) } as never;
}

function buildMe(permissions: string[]): Me {
  return {
    user: {
      id: "u1",
      username: "test",
      fullName: "Test User",
      phone: null,
      role: permissions.includes("reports.read") ? "manager" : "cashier",
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

const location: Location = {
  id: "l1",
  name: "Main Store",
  kind: "store",
  isDefault: true,
  isActive: true,
};

function summary(overrides: Partial<SalesSummaryReport> = {}): SalesSummaryReport {
  return {
    from: "2026-09-05",
    to: "2026-09-05",
    locationId: null,
    cashierId: null,
    salesCount: 10,
    returnsCount: 1,
    revenue: "500000.00",
    discounts: "10000.00",
    refunds: "5000.00",
    netRevenue: "485000.00",
    ...overrides,
  };
}

function productRow(overrides: Partial<SalesByProductRow> = {}): SalesByProductRow {
  return {
    productId: "p1",
    productName: "T-Shirt",
    qtySold: "5.000",
    qtyReturned: "0.000",
    revenue: "250000.00",
    ...overrides,
  };
}

const lowItem: StockLowItem = {
  variantId: "v1",
  productId: "p1",
  qty: "1.000",
  threshold: 2,
};

function renderPage(permissions: string[]) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <AuthProvider me={buildMe(permissions)}>
            <ReportsPage />
          </AuthProvider>
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

/** Routes every GET this page can issue to a canned response, mirroring
 * `StockMovementsPage.test.tsx`'s `mockEndpoints` pattern. */
function mockEndpoints(
  options: {
    summaryData?: SalesSummaryReport;
    byProductPages?: Record<string, { items: SalesByProductRow[]; nextCursor: string | null }>;
  } = {},
) {
  const byProductPages = options.byProductPages ?? {
    "": { items: [productRow()], nextCursor: null },
  };

  mockedApi.GET.mockImplementation(((
    path: string,
    init?: { params?: { query?: Record<string, unknown> } },
  ) => {
    if (path === "/reports/sales/summary") {
      return Promise.resolve(ok(options.summaryData ?? summary()));
    }
    if (path === "/reports/sales/by-product") {
      const cursor = (init?.params?.query?.cursor as string | undefined) ?? "";
      const page = byProductPages[cursor] ?? { items: [], nextCursor: null };
      return Promise.resolve(ok(page));
    }
    if (path === "/locations") {
      return Promise.resolve(ok({ items: [location], nextCursor: null }));
    }
    if (path === "/stock/low") {
      return Promise.resolve(ok({ items: [lowItem], nextCursor: null }));
    }
    throw new Error(`unexpected GET ${path}`);
  }) as never);
}

describe("ReportsPage", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    navigateMock.mockReset();
    mockedApi.GET.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("manager view: sends from/to/locationId and renders cost/margin when present", async () => {
    mockEndpoints({
      summaryData: summary({ cost: "300000.00", margin: "185000.00" }),
      byProductPages: {
        "": { items: [productRow({ cost: "150000.00", margin: "100000.00" })], nextCursor: null },
      },
    });

    renderPage(["reports.read"]);

    await screen.findByText("300000.00");
    expect(screen.getByText("185000.00")).toBeTruthy();

    // Pick a location — that must refetch both the summary and by-product
    // reports with `locationId`.
    fireEvent.mouseDown(screen.getByLabelText("All locations"));
    fireEvent.click(await screen.findByText("Main Store"));

    await waitFor(() => {
      const call = mockedApi.GET.mock.calls.find(
        (entry) =>
          entry[0] === "/reports/sales/summary" &&
          (entry[1] as never as { params: { query: { locationId?: string } } })?.params?.query
            ?.locationId === "l1",
      );
      expect(call).toBeTruthy();
    });

    const today = dayjs().format("YYYY-MM-DD");
    await waitFor(() => {
      const summaryCall = mockedApi.GET.mock.calls.find(
        (entry) => entry[0] === "/reports/sales/summary",
      );
      expect(summaryCall).toBeTruthy();
      if (!summaryCall) {
        return;
      }
      const query = (
        summaryCall[1] as never as { params: { query: { from?: string; to?: string } } }
      ).params.query;
      expect(query.from).toBe(today);
      expect(query.to).toBe(today);
    });

    await waitFor(() => {
      const byProductCall = mockedApi.GET.mock.calls.find(
        (entry) =>
          entry[0] === "/reports/sales/by-product" &&
          (entry[1] as never as { params: { query: { locationId?: string } } })?.params?.query
            ?.locationId === "l1",
      );
      expect(byProductCall).toBeTruthy();
    });

    // By-product table renders cost/margin columns too.
    expect(await screen.findByText("150000.00")).toBeTruthy();
    expect(screen.getByText("100000.00")).toBeTruthy();
  });

  it("manager view: changing the date preset refetches with the new range", async () => {
    mockEndpoints();
    renderPage(["reports.read"]);

    await screen.findByText("500000.00");
    mockedApi.GET.mockClear();

    fireEvent.click(screen.getByText("Last 7 days"));

    const expectedFrom = dayjs().subtract(6, "day").format("YYYY-MM-DD");
    await waitFor(() => {
      const call = mockedApi.GET.mock.calls.find(
        (entry) =>
          entry[0] === "/reports/sales/summary" &&
          (entry[1] as never as { params: { query: { from?: string } } })?.params?.query?.from ===
            expectedFrom,
      );
      expect(call).toBeTruthy();
    });
  });

  it("manager view: by-product 'Load more' requests the next page with the returned cursor", async () => {
    mockEndpoints({
      byProductPages: {
        "": {
          items: [productRow({ productId: "p1", productName: "First" })],
          nextCursor: "cursor-2",
        },
        "cursor-2": {
          items: [productRow({ productId: "p2", productName: "Second" })],
          nextCursor: null,
        },
      },
    });

    renderPage(["reports.read"]);

    await screen.findByText("First");
    fireEvent.click(screen.getByRole("button", { name: "Load more" }));

    await screen.findByText("Second");
    const call = mockedApi.GET.mock.calls.find(
      (entry) =>
        entry[0] === "/reports/sales/by-product" &&
        (entry[1] as never as { params: { query: { cursor?: string } } })?.params?.query?.cursor ===
          "cursor-2",
    );
    expect(call).toBeTruthy();
  });

  it("manager view: the low-stock card links to /stock/low", async () => {
    mockEndpoints();
    renderPage(["reports.read"]);

    await screen.findByText("Sales summary");
    fireEvent.click(screen.getByRole("button", { name: "View low stock" }));

    expect(navigateMock).toHaveBeenCalledWith({ to: "/stock/low" });
  });

  it("cashier view: no date/location controls, no by-product section, no cost/margin, renders echoed dates", async () => {
    mockEndpoints({ summaryData: summary({ from: "2026-09-01", to: "2026-09-01" }) });

    renderPage(["reports.own_day", "sales.create"]);

    const rangeMatches = await screen.findAllByText(
      (_, element) => element?.textContent === "2026-09-01 — 2026-09-01",
    );
    expect(rangeMatches.length).toBeGreaterThan(0);

    expect(screen.queryByText("Today")).toBeNull();
    expect(screen.queryByText("Last 7 days")).toBeNull();
    expect(screen.queryByLabelText("All locations")).toBeNull();
    expect(screen.queryByText("Sales by product")).toBeNull();
    expect(screen.queryByText("Cost")).toBeNull();
    expect(screen.queryByText("Margin")).toBeNull();
    expect(screen.queryByText("Low stock")).toBeNull();

    const summaryCall = mockedApi.GET.mock.calls.find(
      (entry) => entry[0] === "/reports/sales/summary",
    );
    expect(summaryCall).toBeTruthy();
    if (!summaryCall) {
      return;
    }
    const query = (summaryCall[1] as never as { params: { query: Record<string, unknown> } }).params
      .query;
    expect(query.from).toBeUndefined();
    expect(query.to).toBeUndefined();
    expect(query.locationId).toBeUndefined();
  });
});
