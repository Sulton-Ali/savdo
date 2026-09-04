import type { components } from "@savdo/api-client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { App as AntApp, ConfigProvider } from "antd";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../lib/api", () => ({
  api: { GET: vi.fn(), POST: vi.fn(), PATCH: vi.fn() },
}));

import { AuthProvider } from "../../../auth/AuthContext";
import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { StockLevelsPage } from "../StockLevelsPage";

type Me = components["schemas"]["Me"];
type StockLevel = components["schemas"]["StockLevel"];
type Product = components["schemas"]["Product"];
type Location = components["schemas"]["Location"];

const mockedApi = vi.mocked(api, { deep: true });

function buildMe(permissions: string[]): Me {
  return {
    user: {
      id: "u1",
      username: "cashier",
      fullName: "Test User",
      phone: null,
      role: permissions.includes("stock.write") ? "manager" : "cashier",
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
    permissions,
  };
}

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

const level1: StockLevel = { variantId: "v1", productId: "p1", locationId: "l1", qty: "3.000" };
const level2: StockLevel = { variantId: "v1", productId: "p1", locationId: "l2", qty: "2.000" };

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

function renderPage(permissions: string[]) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <AuthProvider me={buildMe(permissions)}>
            <StockLevelsPage />
          </AuthProvider>
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

function mockEndpoints() {
  mockedApi.GET.mockImplementation(((path: string) => {
    if (path === "/stock/levels") {
      return Promise.resolve({
        data: { items: [level1, level2], nextCursor: null },
        error: undefined,
        response: new Response(null, { status: 200 }),
      });
    }
    if (path === "/locations") {
      return Promise.resolve({
        data: { items: [locationA, locationB], nextCursor: null },
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
    if (path === "/products/{id}") {
      return Promise.resolve({
        data: product,
        error: undefined,
        response: new Response(null, { status: 200 }),
      });
    }
    throw new Error(`unexpected GET ${path}`);
  }) as never);
}

describe("StockLevelsPage", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    mockedApi.GET.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("renders one row per variant with a quantity column per location and a total, no cost, for a cashier", async () => {
    mockEndpoints();

    renderPage([]);

    await screen.findByText("T-Shirt");
    expect(screen.getByText("Main Store")).toBeTruthy();
    expect(screen.getByText("Warehouse")).toBeTruthy();
    expect(screen.getByText("SKU1")).toBeTruthy();
    expect(screen.getByText("size: M")).toBeTruthy();

    // 3 (Main Store) + 2 (Warehouse) = 5 total; each cell trims trailing
    // zeros ("3.000" -> "3").
    await waitFor(() => {
      expect(screen.getByText("3")).toBeTruthy();
      expect(screen.getByText("2")).toBeTruthy();
      expect(screen.getByText("5")).toBeTruthy();
    });

    expect(screen.queryByText("Cost")).toBeNull();
  });

  it("hides the Adjust/Transfer buttons for a cashier and shows them for a manager", async () => {
    mockEndpoints();
    renderPage([]);
    await screen.findByText("T-Shirt");
    expect(screen.queryByRole("button", { name: "Adjust stock" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Transfer stock" })).toBeNull();
    cleanup();

    mockEndpoints();
    renderPage(["stock.write"]);
    await screen.findByText("T-Shirt");
    expect(screen.getByRole("button", { name: "Adjust stock" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Transfer stock" })).toBeTruthy();
  });
});
