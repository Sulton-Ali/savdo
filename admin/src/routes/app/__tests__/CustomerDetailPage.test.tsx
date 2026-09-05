import type { components } from "@savdo/api-client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { App as AntApp, ConfigProvider } from "antd";
import dayjs from "dayjs";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

const navigateMock = vi.fn();

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>();
  return { ...actual, useNavigate: () => navigateMock };
});

vi.mock("../../../lib/api", () => ({
  api: { GET: vi.fn() },
}));

import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { CustomerDetailPage } from "../CustomerDetailPage";

type Customer = components["schemas"]["Customer"];
type SaleSummary = components["schemas"]["SaleSummary"];

const mockedApi = vi.mocked(api, { deep: true });

const customer: Customer = {
  id: "c1",
  fullName: "Jane Doe",
  phone: "+998901112233",
  telegramUsername: "janedoe",
  note: "Prefers cash",
  tags: ["vip"],
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

const sale: SaleSummary = {
  id: "s1",
  number: 42,
  kind: "sale",
  status: "completed",
  locationId: "l1",
  locationName: "Main Store",
  customerId: "c1",
  customerName: "Jane Doe",
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
};

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <CustomerDetailPage customerId="c1" />
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

function mockEndpoints() {
  mockedApi.GET.mockImplementation(((path: string) => {
    if (path === "/customers/{id}") {
      return Promise.resolve({
        data: customer,
        error: undefined,
        response: new Response(null, { status: 200 }),
      });
    }
    if (path === "/sales") {
      return Promise.resolve({
        data: { items: [sale], nextCursor: null },
        error: undefined,
        response: new Response(null, { status: 200 }),
      });
    }
    throw new Error(`unexpected GET ${path}`);
  }) as never);
}

describe("CustomerDetailPage", () => {
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

  it("renders the customer's details and requests their sales filtered by customerId", async () => {
    mockEndpoints();

    renderPage();

    await screen.findByText("Jane Doe");
    expect(screen.getByText("+998901112233")).toBeTruthy();
    expect(screen.getByText("janedoe")).toBeTruthy();
    expect(screen.getByText("Prefers cash")).toBeTruthy();
    expect(screen.getByText("vip")).toBeTruthy();

    await waitFor(() => {
      const salesCall = mockedApi.GET.mock.calls.find((call) => call[0] === "/sales");
      expect(salesCall).toBeTruthy();
      const query = (salesCall?.[1] as never as { params?: { query?: { customerId?: string } } })
        ?.params?.query;
      expect(query?.customerId).toBe("c1");
    });
  });

  it("renders one purchase-history row per sale with number, total, kind, status and payment method", async () => {
    mockEndpoints();

    renderPage();

    expect(await screen.findByText("42")).toBeTruthy();
    expect(screen.getByText("125000.00")).toBeTruthy();
    expect(screen.getByText("Sale")).toBeTruthy();
    expect(screen.getByText("Completed")).toBeTruthy();
    expect(screen.getByText("Cash")).toBeTruthy();
    // Formatted in the local timezone, same as `StaffPage`/`StockMovementsPage`
    // (not asserting a fixed clock time — the test machine's timezone varies).
    expect(screen.getByText(dayjs(sale.completedAt).format("YYYY-MM-DD HH:mm"))).toBeTruthy();
  });

  it("shows a not-found message when GET /customers/{id} 404s", async () => {
    mockedApi.GET.mockImplementation(((path: string) => {
      if (path === "/customers/{id}") {
        return Promise.resolve({
          data: undefined,
          error: { error: { code: "NOT_FOUND", details: {} } },
          response: new Response(null, { status: 404 }),
        });
      }
      return Promise.resolve({
        data: { items: [], nextCursor: null },
        error: undefined,
        response: new Response(null, { status: 200 }),
      });
    }) as never);

    renderPage();

    expect(await screen.findByText("Customer not found.")).toBeTruthy();
  });
});
