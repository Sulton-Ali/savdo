import type { components } from "@savdo/api-client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App as AntApp, ConfigProvider } from "antd";
import { useState } from "react";
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
import type { SalesListSearch } from "../salesListRoute";

type Me = components["schemas"]["Me"];
type SaleSummary = components["schemas"]["SaleSummary"];
type Customer = components["schemas"]["Customer"];

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

function customer(overrides: Partial<Customer> = {}): Customer {
  return {
    id: "c1",
    fullName: "Jane Doe",
    phone: "+998901112233",
    telegramUsername: "janedoe",
    note: null,
    tags: [],
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
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
    if (path === "/customers/{id}") {
      return Promise.resolve(apiResult(customer()));
    }
    return Promise.resolve(apiResult({ items: [sale()], nextCursor: null }));
  }) as never);
}

/** Mirrors how `salesListRoute`'s wrapper drives `SalesListPage`
 * (`search`/`onSearchChange`), except the search state lives in this test
 * harness instead of the router — `onSearchChangeSpy` observes every call
 * the page makes while `useState` keeps the page controlled, same as a real
 * `navigate({ search })` round-trip would (mirrors
 * `StockMovementsPage.test.tsx`'s harness). */
function renderPage(
  role: "owner" | "manager" | "cashier" = "manager",
  initialSearch: SalesListSearch = {},
) {
  const onSearchChangeSpy = vi.fn<(next: SalesListSearch) => void>();

  function Harness() {
    const [search, setSearch] = useState<SalesListSearch>(initialSearch);
    return (
      <SalesListPage
        search={search}
        onSearchChange={(next) => {
          onSearchChangeSpy(next);
          setSearch(next);
        }}
      />
    );
  }

  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const utils = render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <AuthProvider me={buildMe(role)}>
            <Harness />
          </AuthProvider>
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
  return { ...utils, onSearchChangeSpy };
}

function lastSalesQuery() {
  const call = [...mockedApi.GET.mock.calls].reverse().find((entry) => entry[0] === "/sales");
  return (call?.[1] as never as { params: { query: Record<string, unknown> } } | undefined)?.params
    .query;
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

  it("passes the selected status filter through to GET /sales and pushes it into the search (replace)", async () => {
    mockEndpoints();

    const { onSearchChangeSpy } = renderPage();
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

    expect(onSearchChangeSpy).toHaveBeenCalledWith(expect.objectContaining({ status: "voided" }));
    await waitFor(() => {
      expect(lastSalesQuery()?.status).toBe("voided");
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

  it("shows the result count line", async () => {
    mockEndpoints();

    renderPage();

    expect(await screen.findByText("1 result")).toBeTruthy();
  });

  it("pre-fills the filters from the initial search (URL) and requests /sales with the mapped params", async () => {
    mockEndpoints();

    renderPage("owner", {
      from: "2026-01-05",
      to: "2026-01-10",
      locationId: "l1",
      kind: "sale",
      status: "completed",
      cashierId: "u1",
    });

    await screen.findByText("42");
    expect(screen.getAllByText("Main Store").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Cashier One").length).toBeGreaterThan(0);

    await waitFor(() => {
      const query = lastSalesQuery();
      expect(query?.from).toBe("2026-01-05");
      expect(query?.to).toBe("2026-01-10");
      expect(query?.locationId).toBe("l1");
      expect(query?.kind).toBe("sale");
      expect(query?.status).toBe("completed");
      expect(query?.cashierId).toBe("u1");
    });
  });

  it("Reset clears every filter", async () => {
    mockEndpoints();

    const { onSearchChangeSpy } = renderPage("owner", {
      locationId: "l1",
      kind: "sale",
      status: "completed",
      cashierId: "u1",
    });
    await screen.findByText("42");

    fireEvent.click(screen.getByRole("button", { name: "Reset" }));

    expect(onSearchChangeSpy).toHaveBeenCalledWith({});
    await waitFor(() => {
      const query = lastSalesQuery();
      expect(query?.locationId).toBeUndefined();
      expect(query?.kind).toBeUndefined();
      expect(query?.status).toBeUndefined();
      expect(query?.cashierId).toBeUndefined();
    });
  });

  it("does not forward cashierId to GET /sales for a non-owner even when the URL has one", async () => {
    mockEndpoints();

    renderPage("manager", { cashierId: "u1" });
    await screen.findByText("42");

    expect(screen.queryByText("All cashiers")).toBeNull();
    await waitFor(() => {
      expect(lastSalesQuery()?.cashierId).toBeUndefined();
    });
  });

  it("seeds the customer filter's label from customerId via GET /customers/{id}", async () => {
    mockEndpoints();

    renderPage("owner", { customerId: "c1" });
    await screen.findByText("42");

    expect(await screen.findByText("Jane Doe")).toBeTruthy();
    await waitFor(() => {
      expect(lastSalesQuery()?.customerId).toBe("c1");
    });
  });

  it("drops customerId from the search when GET /customers/{id} fails", async () => {
    mockedApi.GET.mockImplementation(((path: string) => {
      if (path === "/customers/{id}") {
        return Promise.resolve({
          data: undefined,
          error: { error: { code: "NOT_FOUND", details: {} } },
          response: new Response(null, { status: 404 }),
        });
      }
      if (path === "/customers") {
        return Promise.resolve(apiResult({ items: [], nextCursor: null }));
      }
      if (path === "/locations") {
        return Promise.resolve(apiResult({ items: [], nextCursor: null }));
      }
      return Promise.resolve(apiResult({ items: [sale()], nextCursor: null }));
    }) as never);

    const { onSearchChangeSpy } = renderPage("manager", { customerId: "gone" });
    await screen.findByText("42");

    await waitFor(() => {
      expect(onSearchChangeSpy).toHaveBeenCalledWith(
        expect.objectContaining({ customerId: undefined }),
      );
    });
  });
});
