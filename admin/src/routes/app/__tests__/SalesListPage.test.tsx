import type { components } from "@savdo/api-client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App as AntApp, ConfigProvider } from "antd";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

const navigateMock = vi.fn();

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>();
  return { ...actual, useNavigate: () => navigateMock };
});

vi.mock("../../../lib/api", () => ({
  api: { GET: vi.fn() },
}));

import { AuthProvider } from "../../../auth/AuthContext";
import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { SalesListPage } from "../SalesListPage";

type Me = components["schemas"]["Me"];
type SaleSummary = components["schemas"]["SaleSummary"];

const mockedApi = vi.mocked(api, { deep: true });

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
    permissions: role === "owner" ? ["sales.void"] : role === "manager" ? ["sales.void"] : [],
  };
}

function sale(overrides: Partial<SaleSummary> = {}): SaleSummary {
  return {
    id: "s1",
    number: 42,
    kind: "sale",
    status: "completed",
    locationId: "l1",
    locationName: "Main Store",
    customerId: null,
    customerName: null,
    cashierId: "u1",
    cashierName: "Cashier One",
    originalSaleId: null,
    subtotal: "125000.00",
    discountAmount: "0.00",
    discountReason: null,
    total: "125000.00",
    note: null,
    completedAt: "2026-02-01T10:00:00Z",
    voidedAt: null,
    voidedBy: null,
    voidReason: null,
    paymentMethod: "cash",
    hasReturns: false,
    ...overrides,
  };
}

function apiResult(data: unknown, status = 200) {
  return { data, error: undefined, response: new Response(null, { status }) } as never;
}

function mockEndpoints() {
  mockedApi.GET.mockImplementation(((path: string) => {
    if (path === "/locations") {
      return Promise.resolve(
        apiResult({
          items: [{ id: "l1", name: "Main Store", kind: "store", isDefault: true, isActive: true }],
          nextCursor: null,
        }),
      );
    }
    if (path === "/staff") {
      return Promise.resolve(
        apiResult({
          items: [
            {
              id: "u1",
              username: "cashier1",
              fullName: "Cashier One",
              phone: null,
              role: "cashier",
              locale: "en",
              isActive: true,
              lastLoginAt: null,
              createdAt: "2026-01-01T00:00:00Z",
            },
          ],
          nextCursor: null,
        }),
      );
    }
    if (path === "/customers") {
      return Promise.resolve(apiResult({ items: [], nextCursor: null }));
    }
    return Promise.resolve(apiResult({ items: [sale()], nextCursor: null }));
  }) as never);
}

function renderPage(role: "owner" | "manager" | "cashier" = "manager") {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <AuthProvider me={buildMe(role)}>
            <SalesListPage />
          </AuthProvider>
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

describe("SalesListPage", () => {
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

  it("renders sales returned by GET /sales", async () => {
    mockEndpoints();

    renderPage();

    expect(await screen.findByText("42")).toBeTruthy();
    expect(screen.getByText("Main Store")).toBeTruthy();
    expect(screen.getByText("Cashier One")).toBeTruthy();
    expect(screen.getByText("125000.00")).toBeTruthy();
  });

  it("navigates to the sale detail page on row click", async () => {
    mockEndpoints();

    renderPage();
    fireEvent.click(await screen.findByText("42"));

    expect(navigateMock).toHaveBeenCalledWith({ to: "/sales/$id", params: { id: "s1" } });
  });

  it("passes the selected status filter through to GET /sales", async () => {
    mockEndpoints();

    renderPage();
    await screen.findByText("42");

    fireEvent.mouseDown(screen.getByText("All statuses"));
    const option = await waitFor(() => {
      const match = document.querySelector<HTMLElement>('.ant-select-item[title="Voided"]');
      if (!match) {
        throw new Error("Voided option not rendered yet");
      }
      return match;
    });
    fireEvent.click(option);

    await waitFor(() => {
      const matched = mockedApi.GET.mock.calls.some(
        (call) =>
          call[0] === "/sales" &&
          (call[1] as never as { params: { query: { status?: string } } })?.params?.query
            ?.status === "voided",
      );
      expect(matched).toBe(true);
    });
  });

  it("shows the cashier filter for the owner but not for a manager", async () => {
    mockEndpoints();
    renderPage("owner");
    await screen.findByText("42");
    expect(screen.getByText("All cashiers")).toBeTruthy();
    cleanup();

    mockEndpoints();
    renderPage("manager");
    await screen.findByText("42");
    expect(screen.queryByText("All cashiers")).toBeNull();
  });
});
