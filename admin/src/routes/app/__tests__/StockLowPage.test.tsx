import type { components } from "@savdo/api-client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { App as AntApp, ConfigProvider } from "antd";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../lib/api", () => ({
  api: { GET: vi.fn(), POST: vi.fn(), PATCH: vi.fn() },
}));

import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { StockLowPage } from "../StockLowPage";

type Product = components["schemas"]["Product"];
type StockLowItem = components["schemas"]["StockLowItem"];

const mockedApi = vi.mocked(api, { deep: true });

const lowItem: StockLowItem = { variantId: "v1", productId: "p1", qty: "1.000", threshold: 3 };

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
  variants: [
    {
      id: "v1",
      sku: "SKU1",
      barcode: null,
      attributes: { size: "M" },
      priceOverride: null,
      isActive: true,
    },
  ],
  images: [],
};

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <StockLowPage />
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

describe("StockLowPage", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    mockedApi.GET.mockReset();
    mockedApi.GET.mockImplementation(((path: string) => {
      if (path === "/stock/low") {
        return Promise.resolve({
          data: { items: [lowItem], nextCursor: null },
          error: undefined,
          response: new Response(null, { status: 200 }),
        });
      }
      if (path === "/products/{id}") {
        return Promise.resolve({
          data: product,
          error: undefined,
          response: new Response(null, { status: 200 }),
        });
      }
      throw new Error(`unexpected GET ${path}`);
    }) as never);
  });

  afterEach(() => {
    cleanup();
  });

  it("renders the product, variant, qty and effective threshold", async () => {
    renderPage();

    expect(await screen.findByText("T-Shirt")).toBeTruthy();
    expect(screen.getByText("size: M")).toBeTruthy();
    expect(screen.getByText("SKU1")).toBeTruthy();
    expect(screen.getByText("1")).toBeTruthy();
    expect(screen.getByText("3")).toBeTruthy();
  });
});
