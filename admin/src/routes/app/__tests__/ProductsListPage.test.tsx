import type { components } from "@savdo/api-client";
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

import { AuthProvider } from "../../../auth/AuthContext";
import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { ProductsListPage } from "../ProductsListPage";

type Me = components["schemas"]["Me"];
type Product = components["schemas"]["Product"];

const mockedApi = vi.mocked(api, { deep: true });

function buildMe(permissions: string[]): Me {
  return {
    user: {
      id: "u1",
      username: "manager",
      fullName: "Test User",
      phone: null,
      role: permissions.length > 0 ? "manager" : "cashier",
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
    },
    permissions,
  };
}

function product(overrides: Partial<Product>): Product {
  return {
    id: "p1",
    categoryId: null,
    slug: "shirt",
    sku: "SKU1",
    unitId: "u1",
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
    ...overrides,
  };
}

function renderPage(permissions: string[] = ["catalog.write"]) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <AuthProvider me={buildMe(permissions)}>
            <ProductsListPage />
          </AuthProvider>
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

function mockEndpoints(products: Product[]) {
  mockedApi.GET.mockImplementation(((path: string) => {
    if (path === "/categories") {
      return Promise.resolve({
        data: { items: [] },
        error: undefined,
        response: new Response(null, { status: 200 }),
      });
    }
    return Promise.resolve({
      data: { items: products, nextCursor: null },
      error: undefined,
      response: new Response(null, { status: 200 }),
    });
  }) as never);
}

describe("ProductsListPage", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    mockedApi.GET.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("hides the cost column when the response has no costPrice", async () => {
    mockEndpoints([product({})]);

    renderPage();

    await screen.findByText("Shirt");
    expect(screen.queryByText("Cost")).toBeNull();
  });

  it("shows the cost column when costPrice is present in the response", async () => {
    mockEndpoints([product({ costPrice: "6000.00" })]);

    renderPage();

    await screen.findByText("Shirt");
    expect(screen.getByText("Cost")).toBeTruthy();
    expect(screen.getByText("6000.00")).toBeTruthy();
  });

  it("debounces search and fires a request with q only at 2+ characters", async () => {
    mockEndpoints([product({})]);

    renderPage();
    await screen.findByText("Shirt");

    const searchInput = screen.getByPlaceholderText("Search products");

    // A single character stays below the minimum — no request should ever
    // carry `q: "s"`.
    fireEvent.change(searchInput, { target: { value: "s" } });
    await new Promise((resolve) => setTimeout(resolve, 400));
    expect(
      mockedApi.GET.mock.calls.some(
        (call) =>
          call[0] === "/products" &&
          (call[1] as never as { params: { query: { q?: string } } })?.params?.query?.q === "s",
      ),
    ).toBe(false);

    fireEvent.change(searchInput, { target: { value: "sh" } });
    await waitFor(
      () => {
        const matched = mockedApi.GET.mock.calls.some(
          (call) =>
            call[0] === "/products" &&
            (call[1] as never as { params: { query: { q?: string } } })?.params?.query?.q === "sh",
        );
        expect(matched).toBe(true);
      },
      { timeout: 2000 },
    );
  });
});
