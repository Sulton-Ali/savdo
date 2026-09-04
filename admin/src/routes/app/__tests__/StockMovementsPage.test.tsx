import type { components } from "@savdo/api-client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App as AntApp, ConfigProvider } from "antd";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../lib/api", () => ({
  api: { GET: vi.fn(), POST: vi.fn(), PATCH: vi.fn() },
}));

import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { StockMovementsPage } from "../StockMovementsPage";

type StockMovement = components["schemas"]["StockMovement"];
type Location = components["schemas"]["Location"];

const mockedApi = vi.mocked(api, { deep: true });

const locationA: Location = {
  id: "l1",
  name: "Main Store",
  kind: "store",
  isDefault: true,
  isActive: true,
};

const movement: StockMovement = {
  id: "m1",
  variantId: "v1",
  locationId: "l1",
  kind: "purchase_in",
  qty: "5.000",
  unitCost: "1000.00",
  refType: "purchase",
  refId: "11111111-2222-3333-4444-555555555555",
  reason: null,
  note: null,
  createdBy: "u1",
  createdAt: "2026-01-05T10:00:00Z",
};

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <StockMovementsPage />
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

function mockEndpoints() {
  mockedApi.GET.mockImplementation(((path: string) => {
    if (path === "/stock/movements") {
      return Promise.resolve({
        data: { items: [movement], nextCursor: null },
        error: undefined,
        response: new Response(null, { status: 200 }),
      });
    }
    if (path === "/locations") {
      return Promise.resolve({
        data: { items: [locationA], nextCursor: null },
        error: undefined,
        response: new Response(null, { status: 200 }),
      });
    }
    if (path === "/products") {
      return Promise.resolve({
        data: { items: [], nextCursor: null },
        error: undefined,
        response: new Response(null, { status: 200 }),
      });
    }
    throw new Error(`unexpected GET ${path}`);
  }) as never);
}

describe("StockMovementsPage", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    mockedApi.GET.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("renders the kind tag, signed qty, unit cost, reference and reason columns", async () => {
    mockEndpoints();
    renderPage();

    expect(await screen.findByText("Purchase")).toBeTruthy();
    expect(screen.getByText("+5.000")).toBeTruthy();
    expect(screen.getByText("1000.00")).toBeTruthy();
    expect(screen.getByText("purchase #11111111")).toBeTruthy();
  });

  it("requests /stock/movements with locationId and kind once selected", async () => {
    mockEndpoints();
    renderPage();
    await screen.findByText("Purchase");

    fireEvent.mouseDown(screen.getByLabelText("All locations"));
    fireEvent.click(await screen.findByText("Main Store"));

    await waitFor(() => {
      const call = mockedApi.GET.mock.calls.find(
        (entry) =>
          entry[0] === "/stock/movements" &&
          (entry[1] as never as { params: { query: { locationId?: string } } })?.params?.query
            ?.locationId === "l1",
      );
      expect(call).toBeTruthy();
    });

    fireEvent.mouseDown(screen.getByLabelText("All kinds"));
    fireEvent.click(await screen.findByText("Adjustment"));

    await waitFor(() => {
      const call = mockedApi.GET.mock.calls.find(
        (entry) =>
          entry[0] === "/stock/movements" &&
          (entry[1] as never as { params: { query: { kind?: string; locationId?: string } } })
            ?.params?.query?.kind === "adjustment",
      );
      expect(call).toBeTruthy();
    });
  });
});
