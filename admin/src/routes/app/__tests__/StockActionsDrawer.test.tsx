import type { components } from "@savdo/api-client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App as AntApp, ConfigProvider } from "antd";
import type { ReactNode } from "react";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../lib/api", () => ({
  api: { GET: vi.fn(), POST: vi.fn(), PATCH: vi.fn() },
}));

import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { StockAdjustmentDrawer, StockTransferDrawer } from "../StockActionsDrawer";

type Product = components["schemas"]["Product"];
type Variant = components["schemas"]["Variant"];
type Location = components["schemas"]["Location"];

const mockedApi = vi.mocked(api, { deep: true });

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

const locationA: Location = {
  id: "l1",
  name: "Main Store",
  kind: "store",
  isDefault: true,
  isActive: true,
};
const locationB: Location = {
  id: "l2",
  name: "Warehouse",
  kind: "warehouse",
  isDefault: false,
  isActive: true,
};

function jsonResult(data: unknown, status = 200) {
  return { data, error: undefined, response: new Response(null, { status }) } as never;
}

function mockGetEndpoints(locations: Location[] = [locationA, locationB]) {
  mockedApi.GET.mockImplementation(((path: string) => {
    if (path === "/products") {
      return Promise.resolve(jsonResult({ items: [product], nextCursor: null }));
    }
    if (path === "/products/{id}/variants") {
      return Promise.resolve(jsonResult({ items: [variant] }));
    }
    if (path === "/locations") {
      return Promise.resolve(jsonResult({ items: locations, nextCursor: null }));
    }
    throw new Error(`unexpected GET ${path}`);
  }) as never);
}

function renderDrawer(node: ReactNode) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>{node}</AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

/** rc-select keeps a closed dropdown's option nodes in the DOM (just
 * hidden), so once more than one `Select` has ever been opened, a plain
 * `getByText`/`findByText` on an option label can match a stale, already-
 * closed dropdown too. Always pick the *last* matching `.ant-select-item-option`
 * node — the most recently opened dropdown's — mirroring the same trick
 * `ImageGallery.test.tsx` uses. */
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

/** Opens the product select, picks `T-Shirt`, then opens the variant select
 * and picks its one variant — shared by every adjustment/transfer test
 * below since both drawers require a variant before qty/location fields
 * become meaningful. */
async function pickVariant() {
  fireEvent.mouseDown(screen.getByLabelText("Search product"));
  await selectOption("T-Shirt");

  fireEvent.mouseDown(await screen.findByLabelText("Select variant"));
  await selectOption("size: M — SKU SKU1");
}

describe("StockAdjustmentDrawer", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    mockedApi.GET.mockReset();
    mockedApi.POST.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("posts variantId/locationId/qty as a string and the chosen reason, with an Idempotency-Key reused on retry", async () => {
    mockGetEndpoints();
    mockedApi.POST.mockResolvedValueOnce({
      data: undefined,
      error: { error: { code: "INTERNAL", details: {} } },
      response: new Response(null, { status: 500 }),
    } as never);
    mockedApi.POST.mockResolvedValueOnce(
      jsonResult(
        {
          id: "mv1",
          variantId: "v1",
          locationId: "l1",
          kind: "adjustment",
          qty: "2.000",
          unitCost: null,
          refType: null,
          refId: null,
          reason: "found",
          note: null,
          createdBy: "u1",
          createdAt: "2026-01-05T10:00:00Z",
        },
        201,
      ),
    );

    renderDrawer(<StockAdjustmentDrawer open onClose={vi.fn()} />);

    await pickVariant();

    fireEvent.mouseDown(screen.getByLabelText("Location"));
    await selectOption("Main Store");

    fireEvent.change(screen.getByLabelText("Quantity"), { target: { value: "2" } });

    fireEvent.mouseDown(screen.getByLabelText("Reason"));
    await selectOption("Found");

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(mockedApi.POST).toHaveBeenCalledTimes(1));
    const firstCall = mockedApi.POST.mock.calls[0] as unknown as [
      string,
      { params: { header: { "Idempotency-Key": string } }; body: Record<string, unknown> },
    ];
    expect(firstCall[0]).toBe("/stock/adjustments");
    expect(firstCall[1].body).toEqual({
      variantId: "v1",
      locationId: "l1",
      qty: "2.000",
      reason: "found",
    });
    const key = firstCall[1].params.header["Idempotency-Key"];
    expect(typeof key).toBe("string");
    expect(key.length).toBeGreaterThan(0);

    // Retry the same submission — the same drawer session, so the same key.
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(mockedApi.POST).toHaveBeenCalledTimes(2));
    const secondCall = mockedApi.POST.mock.calls[1] as unknown as [
      string,
      { params: { header: { "Idempotency-Key": string } } },
    ];
    expect(secondCall[1].params.header["Idempotency-Key"]).toBe(key);
  });

  it("shows the available qty from STOCK_INSUFFICIENT details on the qty field", async () => {
    mockGetEndpoints();
    mockedApi.POST.mockResolvedValueOnce({
      data: undefined,
      error: {
        error: {
          code: "STOCK_INSUFFICIENT",
          details: { variantId: "v1", locationId: "l1", available: "1.500" },
        },
      },
      response: new Response(null, { status: 409 }),
    } as never);

    renderDrawer(<StockAdjustmentDrawer open onClose={vi.fn()} />);
    await pickVariant();

    fireEvent.mouseDown(screen.getByLabelText("Location"));
    await selectOption("Main Store");
    fireEvent.change(screen.getByLabelText("Quantity"), { target: { value: "-5" } });
    fireEvent.mouseDown(screen.getByLabelText("Reason"));
    await selectOption("Damaged");

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText("Only 1.500 available")).toBeTruthy();
  });
});

describe("StockTransferDrawer", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    mockedApi.GET.mockReset();
    mockedApi.POST.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("rejects the same from/to location client-side without posting", async () => {
    mockGetEndpoints();

    renderDrawer(<StockTransferDrawer open onClose={vi.fn()} />);
    await pickVariant();

    fireEvent.mouseDown(screen.getByLabelText("From location"));
    await selectOption("Main Store");

    fireEvent.mouseDown(screen.getByLabelText("To location"));
    await selectOption("Main Store");

    fireEvent.change(screen.getByLabelText("Quantity"), { target: { value: "1" } });

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText("Choose a different location")).toBeTruthy();
    expect(mockedApi.POST).not.toHaveBeenCalled();
  });

  it("maps a 409 STOCK_INSUFFICIENT onto the qty field", async () => {
    mockGetEndpoints();
    mockedApi.POST.mockResolvedValueOnce({
      data: undefined,
      error: {
        error: {
          code: "STOCK_INSUFFICIENT",
          details: { variantId: "v1", locationId: "l1", available: "0.000" },
        },
      },
      response: new Response(null, { status: 409 }),
    } as never);

    renderDrawer(<StockTransferDrawer open onClose={vi.fn()} />);
    await pickVariant();

    fireEvent.mouseDown(screen.getByLabelText("From location"));
    await selectOption("Main Store");

    fireEvent.mouseDown(screen.getByLabelText("To location"));
    await selectOption("Warehouse");

    fireEvent.change(screen.getByLabelText("Quantity"), { target: { value: "3" } });

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText("Only 0.000 available")).toBeTruthy();
    expect(mockedApi.POST).toHaveBeenCalledWith("/stock/transfers", {
      body: { variantId: "v1", fromLocationId: "l1", toLocationId: "l2", qty: "3.000" },
    });
  });
});
