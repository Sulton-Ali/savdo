import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App as AntApp, ConfigProvider } from "antd";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>();
  return { ...actual, useNavigate: () => vi.fn() };
});

vi.mock("../../../lib/api", () => ({
  api: { GET: vi.fn(), POST: vi.fn(), PATCH: vi.fn() },
}));

import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { PurchasesListPage } from "../PurchasesListPage";

const mockedApi = vi.mocked(api, { deep: true });

function apiResult(data: unknown, status = 200) {
  return { data, error: undefined, response: new Response(null, { status }) } as never;
}

function purchase() {
  return {
    id: "pur1",
    number: "P-000012",
    supplierId: "sup1",
    locationId: "loc1",
    status: "draft" as const,
    supplierInvoiceNo: null,
    receivedAt: null,
    note: null,
    totalCost: "150000.00",
    items: [],
    createdAt: "2026-01-01T00:00:00Z",
  };
}

function mockEndpoints() {
  mockedApi.GET.mockImplementation(((path: string) => {
    if (path === "/suppliers") {
      return Promise.resolve(
        apiResult({
          items: [
            {
              id: "sup1",
              name: "Acme Textiles",
              contactName: null,
              phone: null,
              telegramUsername: null,
              note: null,
            },
          ],
          nextCursor: null,
        }),
      );
    }
    if (path === "/locations") {
      return Promise.resolve(
        apiResult({
          items: [
            { id: "loc1", name: "Main store", kind: "store", isDefault: true, isActive: true },
          ],
          nextCursor: null,
        }),
      );
    }
    return Promise.resolve(apiResult({ items: [purchase()], nextCursor: null }));
  }) as never);
}

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <PurchasesListPage />
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

describe("PurchasesListPage", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    mockedApi.GET.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("renders the purchase's number, supplier, location and status", async () => {
    mockEndpoints();

    renderPage();

    expect(await screen.findByText("P-000012")).toBeTruthy();
    expect(await screen.findByText("Acme Textiles")).toBeTruthy();
    expect(await screen.findByText("Main store")).toBeTruthy();
    expect(screen.getByText("Draft")).toBeTruthy();
  });

  it("requests /purchases with the selected status filter", async () => {
    mockEndpoints();

    renderPage();
    await screen.findByText("P-000012");

    fireEvent.mouseDown(screen.getByText("All statuses"));
    const receivedOption = await waitFor(() => {
      const match = document.querySelector<HTMLElement>('.ant-select-item[title="Received"]');
      if (!match) {
        throw new Error("Received option not rendered yet");
      }
      return match;
    });
    fireEvent.click(receivedOption);

    await waitFor(() => {
      const matched = mockedApi.GET.mock.calls.some(
        (call) =>
          call[0] === "/purchases" &&
          (call[1] as never as { params: { query: { status?: string } } })?.params?.query
            ?.status === "received",
      );
      expect(matched).toBe(true);
    });
  });
});
