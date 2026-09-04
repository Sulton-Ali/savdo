import type { components } from "@savdo/api-client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App as AntApp, ConfigProvider } from "antd";
import dayjs from "dayjs";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../lib/api", () => ({
  api: { GET: vi.fn(), POST: vi.fn(), PATCH: vi.fn() },
}));

import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { StockMovementsPage } from "../StockMovementsPage";

type StockMovement = components["schemas"]["StockMovement"];
type Location = components["schemas"]["Location"];
type Product = components["schemas"]["Product"];
type Variant = components["schemas"]["Variant"];

const mockedApi = vi.mocked(api, { deep: true });

const locationA: Location = {
  id: "l1",
  name: "Main Store",
  kind: "store",
  isDefault: true,
  isActive: true,
};

const product: Product = {
  id: "p1",
  categoryId: null,
  slug: "t-shirt",
  sku: null,
  unitId: "u1",
  basePrice: "10000.00",
  promoPrice: null,
  promoFrom: null,
  promoTo: null,
  isActive: true,
  isFeatured: false,
  name: "T-Shirt",
  description: null,
  locale: "en",
  translationFallback: false,
  lowStockThreshold: null,
};

const variant: Variant = {
  id: "v1",
  sku: "SKU1",
  barcode: null,
  attributes: { size: "M" },
  priceOverride: null,
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
        data: { items: [product], nextCursor: null },
        error: undefined,
        response: new Response(null, { status: 200 }),
      });
    }
    if (path === "/products/{id}/variants") {
      return Promise.resolve({
        data: { items: [variant] },
        error: undefined,
        response: new Response(null, { status: 200 }),
      });
    }
    throw new Error(`unexpected GET ${path}`);
  }) as never);
}

/** rc-select keeps a closed dropdown's option nodes in the DOM (just
 * hidden), so once more than one `Select` has ever been opened, a plain
 * `findByText` on an option label can match a stale, already-closed
 * dropdown too. Always pick the *last* matching `.ant-select-item-option`
 * node — the most recently opened dropdown's (mirrors
 * `StockActionsDrawer.test.tsx`). */
async function selectOption(text: string) {
  await waitFor(() => {
    const matches = Array.from(document.querySelectorAll(".ant-select-item-option")).filter(
      (el) => el.textContent === text,
    );
    expect(matches.length).toBeGreaterThan(0);
  });
  const matches = Array.from(document.querySelectorAll(".ant-select-item-option")).filter(
    (el) => el.textContent === text,
  );
  fireEvent.click(matches[matches.length - 1] as HTMLElement);
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

  it("requests /stock/movements with variantId once a variant is picked", async () => {
    mockEndpoints();
    renderPage();
    await screen.findByText("Purchase");

    fireEvent.mouseDown(screen.getByLabelText("Search product"));
    await selectOption("T-Shirt");
    fireEvent.mouseDown(await screen.findByLabelText("Select variant"));
    await selectOption("size: M — SKU SKU1");

    await waitFor(() => {
      const call = mockedApi.GET.mock.calls.find(
        (entry) =>
          entry[0] === "/stock/movements" &&
          (entry[1] as never as { params: { query: { variantId?: string } } })?.params?.query
            ?.variantId === "v1",
      );
      expect(call).toBeTruthy();
    });
  });

  it("requests /stock/movements with from/to as start/end-of-day ISO strings once a date range is picked", async () => {
    mockEndpoints();
    renderPage();
    await screen.findByText("Purchase");

    const [startInput, endInput] = screen.getAllByLabelText("Date range");
    fireEvent.mouseDown(startInput as HTMLElement);
    fireEvent.change(startInput as HTMLElement, { target: { value: "2026-01-05" } });
    fireEvent.keyDown(startInput as HTMLElement, { key: "Enter", code: "Enter" });
    fireEvent.change(endInput as HTMLElement, { target: { value: "2026-01-10" } });
    fireEvent.keyDown(endInput as HTMLElement, { key: "Enter", code: "Enter" });

    const expectedFrom = dayjs("2026-01-05").startOf("day").toISOString();
    const expectedTo = dayjs("2026-01-10").endOf("day").toISOString();

    await waitFor(() => {
      const call = mockedApi.GET.mock.calls.find(
        (entry) =>
          entry[0] === "/stock/movements" &&
          (entry[1] as never as { params: { query: { from?: string; to?: string } } })?.params
            ?.query?.from === expectedFrom,
      );
      expect(call).toBeTruthy();
      if (!call) {
        return;
      }
      const query = (call[1] as never as { params: { query: { from?: string; to?: string } } })
        .params.query;
      expect(query.to).toBe(expectedTo);
    });
  });
});
