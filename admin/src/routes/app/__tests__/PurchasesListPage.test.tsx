import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App as AntApp, ConfigProvider } from "antd";
import { useState } from "react";
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
import type { PurchasesSearch } from "../purchasesRoute";

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

/** Mirrors how `purchasesRoute`'s wrapper drives `PurchasesListPage`
 * (`search`/`onSearchChange`), except the search state lives in this test
 * harness instead of the router — `onSearchChangeSpy` observes every call
 * the page makes while `useState` keeps the page controlled, same as a real
 * `navigate({ search })` round-trip would (mirrors
 * `StockMovementsPage.test.tsx`'s harness). */
function renderPage(initialSearch: PurchasesSearch = {}) {
  const onSearchChangeSpy = vi.fn<(next: PurchasesSearch) => void>();

  function Harness() {
    const [search, setSearch] = useState<PurchasesSearch>(initialSearch);
    return (
      <PurchasesListPage
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
          <Harness />
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
  return { ...utils, onSearchChangeSpy };
}

function lastPurchasesQuery() {
  const call = [...mockedApi.GET.mock.calls].reverse().find((entry) => entry[0] === "/purchases");
  return (call?.[1] as never as { params: { query: Record<string, unknown> } } | undefined)?.params
    .query;
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
    expect(screen.getByText("Acme Textiles")).toBeTruthy();
    expect(screen.getByText("Main store")).toBeTruthy();
    expect(screen.getByText("Draft")).toBeTruthy();
  });

  it("requests /purchases with the selected status filter and pushes it into the search (replace)", async () => {
    mockEndpoints();

    const { onSearchChangeSpy } = renderPage();
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

    expect(onSearchChangeSpy).toHaveBeenCalledWith(expect.objectContaining({ status: "received" }));
    await waitFor(() => {
      expect(lastPurchasesQuery()?.status).toBe("received");
    });
  });

  it("shows the result count line", async () => {
    mockEndpoints();

    renderPage();

    expect(await screen.findByText("1 result")).toBeTruthy();
  });

  it("pre-fills the status and supplier filters from the initial search (URL) and requests /purchases with them", async () => {
    mockEndpoints();

    renderPage({ status: "received", supplierId: "sup1" });

    await screen.findByText("P-000012");
    expect(screen.getAllByText("Acme Textiles").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Received").length).toBeGreaterThan(0);

    await waitFor(() => {
      const query = lastPurchasesQuery();
      expect(query?.status).toBe("received");
      expect(query?.supplierId).toBe("sup1");
    });
  });

  it("Reset clears both filters", async () => {
    mockEndpoints();

    const { onSearchChangeSpy } = renderPage({ status: "received", supplierId: "sup1" });
    await screen.findByText("P-000012");

    fireEvent.click(screen.getByRole("button", { name: "Reset" }));

    expect(onSearchChangeSpy).toHaveBeenCalledWith({});
    await waitFor(() => {
      const query = lastPurchasesQuery();
      expect(query?.status).toBeUndefined();
      expect(query?.supplierId).toBeUndefined();
    });
  });
});
