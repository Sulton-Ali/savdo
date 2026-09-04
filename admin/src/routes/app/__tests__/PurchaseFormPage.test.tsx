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
import { PurchaseFormPage } from "../PurchaseFormPage";

const mockedApi = vi.mocked(api, { deep: true });

function apiResult(data: unknown, status = 200) {
  return { data, error: undefined, response: new Response(null, { status }) } as never;
}

function apiError(code: string, details: Record<string, unknown>, status = 409) {
  return {
    data: undefined,
    error: { error: { code, details } },
    response: new Response(null, { status }),
  } as never;
}

const supplier = {
  id: "sup1",
  name: "Acme Textiles",
  contactName: null,
  phone: null,
  telegramUsername: null,
  note: null,
};

const location = { id: "loc1", name: "Main store", kind: "store", isDefault: true, isActive: true };

const product = {
  id: "prod1",
  categoryId: null,
  slug: "shirt",
  sku: "SKU1",
  unitId: "unit1",
  basePrice: "10000.00",
  promoPrice: null,
  promoFrom: null,
  promoTo: null,
  isActive: true,
  isFeatured: false,
  name: "Shirt",
  description: null,
  locale: "en",
  translationFallback: false,
  lowStockThreshold: null,
};

const variant = {
  id: "var1",
  sku: "SKU1-M",
  barcode: null,
  attributes: { size: "M" },
  priceOverride: null,
  costOverride: "5000.00",
  isActive: true,
};

function draftPurchase() {
  return {
    id: "pur1",
    number: "P-000012",
    supplierId: "sup1",
    locationId: "loc1",
    status: "draft" as const,
    supplierInvoiceNo: null,
    receivedAt: null,
    note: null,
    totalCost: "0.00",
    items: [{ id: "item1", variantId: "var1", qty: "2.000", unitCost: "5000.00" }],
    createdAt: "2026-01-01T00:00:00Z",
  };
}

function mockCommonEndpoints() {
  mockedApi.GET.mockImplementation(((
    path: string,
    options?: { params?: { query?: Record<string, unknown> } },
  ) => {
    if (path === "/suppliers") {
      return Promise.resolve(apiResult({ items: [supplier], nextCursor: null }));
    }
    if (path === "/locations") {
      return Promise.resolve(apiResult({ items: [location], nextCursor: null }));
    }
    if (path === "/products") {
      const q = options?.params?.query?.q;
      if (q === "Shirt") {
        return Promise.resolve(apiResult({ items: [product], nextCursor: null }));
      }
      return Promise.resolve(apiResult({ items: [], nextCursor: null }));
    }
    if (path === "/products/{id}/variants") {
      return Promise.resolve(apiResult({ items: [variant] }));
    }
    return Promise.resolve(apiResult({ items: [], nextCursor: null }));
  }) as never);
}

function renderForm(purchaseId?: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <PurchaseFormPage purchaseId={purchaseId} />
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

describe("PurchaseFormPage", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    mockedApi.GET.mockReset();
    mockedApi.POST.mockReset();
    mockedApi.PATCH.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("posts a PurchaseCreate with string money/qty and no server-computed fields", async () => {
    mockCommonEndpoints();
    mockedApi.POST.mockResolvedValueOnce(
      apiResult(
        {
          id: "new1",
          number: "P-000013",
          supplierId: "sup1",
          locationId: "loc1",
          status: "draft",
          supplierInvoiceNo: null,
          receivedAt: null,
          note: null,
          totalCost: "10000.00",
          items: [{ id: "item1", variantId: "var1", qty: "2.000", unitCost: "5000.00" }],
          createdAt: "2026-01-01T00:00:00Z",
        },
        201,
      ),
    );

    renderForm(undefined);

    fireEvent.mouseDown(await screen.findByLabelText("Supplier"));
    fireEvent.click(await screen.findByText("Acme Textiles"));

    fireEvent.mouseDown(screen.getByLabelText("Location"));
    fireEvent.click(await screen.findByText("Main store"));

    const productSearch = screen.getByRole("combobox", {
      name: "Search by product name or SKU",
    });
    fireEvent.mouseDown(productSearch);
    fireEvent.change(productSearch, { target: { value: "Shirt" } });

    await waitFor(() => {
      expect(
        mockedApi.GET.mock.calls.some(
          (call) =>
            call[0] === "/products" &&
            (call[1] as never as { params: { query: { q?: string } } })?.params?.query?.q ===
              "Shirt",
        ),
      ).toBe(true);
    });

    fireEvent.click(await screen.findByText("Shirt (SKU1)"));

    fireEvent.mouseDown(await screen.findByRole("combobox", { name: "Select variant" }));
    fireEvent.click(await screen.findByText("SKU1-M — M"));

    fireEvent.change(screen.getByLabelText("Qty"), { target: { value: "2" } });
    fireEvent.change(screen.getByLabelText("Unit cost"), { target: { value: "5000" } });

    fireEvent.click(screen.getByRole("button", { name: "Add item" }));

    await screen.findByText("Shirt");

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mockedApi.POST).toHaveBeenCalledWith("/purchases", {
        body: {
          supplierId: "sup1",
          locationId: "loc1",
          items: [{ variantId: "var1", qty: "2.000", unitCost: "5000.00" }],
        },
      });
    });
  });

  it("disables Save and shows a message when there are no item rows", async () => {
    mockCommonEndpoints();

    renderForm(undefined);

    fireEvent.mouseDown(await screen.findByLabelText("Supplier"));
    fireEvent.click(await screen.findByText("Acme Textiles"));

    fireEvent.mouseDown(screen.getByLabelText("Location"));
    fireEvent.click(await screen.findByText("Main store"));

    const saveButton = screen.getByRole("button", { name: "Save" }) as HTMLButtonElement;
    expect(saveButton.disabled).toBe(true);
    expect(await screen.findByText("Add at least one item before saving.")).toBeTruthy();
    expect(mockedApi.POST).not.toHaveBeenCalled();
  });

  it("sends Idempotency-Key on receive and reuses the same key on retry", async () => {
    mockCommonEndpoints();
    mockedApi.GET.mockImplementation(((path: string) => {
      if (path === "/purchases/{id}") {
        return Promise.resolve(apiResult(draftPurchase()));
      }
      if (path === "/suppliers") {
        return Promise.resolve(apiResult({ items: [supplier], nextCursor: null }));
      }
      if (path === "/locations") {
        return Promise.resolve(apiResult({ items: [location], nextCursor: null }));
      }
      return Promise.resolve(apiResult({ items: [], nextCursor: null }));
    }) as never);

    mockedApi.POST.mockResolvedValueOnce(apiError("INTERNAL", {}, 500));
    mockedApi.POST.mockResolvedValueOnce(apiResult({ ...draftPurchase(), status: "received" }));

    renderForm("pur1");

    fireEvent.click(await screen.findByRole("button", { name: "Receive" }));
    fireEvent.click(await screen.findByRole("button", { name: "OK" }));

    await waitFor(() => {
      expect(mockedApi.POST).toHaveBeenCalledTimes(1);
    });

    function idempotencyKeyOf(callIndex: number): string {
      const call = mockedApi.POST.mock.calls[callIndex];
      if (!call) {
        throw new Error(`expected a POST call at index ${callIndex}`);
      }
      const options = call[1] as unknown as {
        params: { path: { id: string }; header: { "Idempotency-Key": string } };
      };
      return options.params.header["Idempotency-Key"];
    }

    expect(mockedApi.POST.mock.calls[0]?.[0]).toBe("/purchases/{id}/receive");
    const firstKey = idempotencyKeyOf(0);
    expect(firstKey).toBeTruthy();

    // Retry from the same dialog session — the key must be identical.
    fireEvent.click(screen.getByRole("button", { name: "OK" }));

    await waitFor(() => {
      expect(mockedApi.POST).toHaveBeenCalledTimes(2);
    });

    const secondKey = idempotencyKeyOf(1);
    expect(secondKey).toBe(firstKey);
  });

  it("shows a readable STOCK_INSUFFICIENT message on cancel", async () => {
    mockedApi.GET.mockImplementation(((path: string) => {
      if (path === "/purchases/{id}") {
        return Promise.resolve(apiResult({ ...draftPurchase(), status: "received" }));
      }
      if (path === "/suppliers") {
        return Promise.resolve(apiResult({ items: [supplier], nextCursor: null }));
      }
      if (path === "/locations") {
        return Promise.resolve(apiResult({ items: [location], nextCursor: null }));
      }
      return Promise.resolve(apiResult({ items: [], nextCursor: null }));
    }) as never);
    mockedApi.POST.mockResolvedValueOnce(
      apiError("STOCK_INSUFFICIENT", { variantId: "var1", available: "1.000" }),
    );

    renderForm("pur1");

    fireEvent.click(await screen.findByRole("button", { name: "Cancel purchase" }));
    fireEvent.click(await screen.findByRole("button", { name: "OK" }));

    expect(
      await screen.findByText(/Not enough stock to cancel: only 1\.000 available/),
    ).toBeTruthy();
  });
});
