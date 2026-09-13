import type { components } from "@savdo/api-client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
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

import { AuthProvider } from "../../../auth/AuthContext";
import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { ProductsListPage } from "../ProductsListPage";
import type { ProductsSearch } from "../productsRoute";

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
      lowStockThreshold: 2,
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
    lowStockThreshold: null,
    ...overrides,
  };
}

/** Mirrors how `productsRoute`'s wrapper drives `ProductsListPage`
 * (`search`/`onSearchChange`), except the search state lives in this test
 * harness instead of the router — `onSearchChangeSpy` observes every call
 * the page makes while `useState` keeps the page controlled, same as a real
 * `navigate({ search })` round-trip would (mirrors
 * `CustomersPage.test.tsx`'s harness). */
function renderPage(permissions: string[] = ["catalog.write"], initialSearch: ProductsSearch = {}) {
  const onSearchChangeSpy = vi.fn<(next: ProductsSearch) => void>();
  // Lets a test simulate a change that does not go through the page's own
  // `onSearchChange` (browser Back/Forward, another navigation) — a real
  // `setSearch` call from the harness, not the spied round-trip.
  let setExternalSearchImpl: (next: ProductsSearch) => void = () => {};

  function Harness() {
    const [search, setSearch] = useState<ProductsSearch>(initialSearch);
    setExternalSearchImpl = setSearch;
    return (
      <ProductsListPage
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
          <AuthProvider me={buildMe(permissions)}>
            <Harness />
          </AuthProvider>
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
  return {
    ...utils,
    onSearchChangeSpy,
    setExternalSearch: (next: ProductsSearch) => act(() => setExternalSearchImpl(next)),
  };
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

function lastProductsQuery() {
  const call = [...mockedApi.GET.mock.calls].reverse().find((entry) => entry[0] === "/products");
  return (call?.[1] as never as { params: { query: Record<string, unknown> } } | undefined)?.params
    .query;
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
    expect(lastProductsQuery()?.q).not.toBe("s");

    fireEvent.change(searchInput, { target: { value: "sh" } });
    await waitFor(
      () => {
        expect(lastProductsQuery()?.q).toBe("sh");
      },
      { timeout: 2000 },
    );
  });

  it("pushes the debounced query into the route's search params (replace, not push)", async () => {
    mockEndpoints([product({})]);
    const { onSearchChangeSpy } = renderPage();
    await screen.findByText("Shirt");

    fireEvent.change(screen.getByPlaceholderText("Search products"), {
      target: { value: "sh" },
    });

    await waitFor(
      () => {
        expect(onSearchChangeSpy).toHaveBeenCalledWith(expect.objectContaining({ q: "sh" }));
      },
      { timeout: 2000 },
    );
  });

  it("shows a below-minimum URL q in the input but never queries with it", async () => {
    mockEndpoints([product({})]);

    renderPage(["catalog.write"], { q: "a" });

    expect((screen.getByPlaceholderText("Search products") as HTMLInputElement).value).toBe("a");
    await screen.findByText("Shirt");
    expect(lastProductsQuery()?.q).toBeUndefined();
  });

  it("syncs the input from an external search change when nothing is being typed", async () => {
    mockEndpoints([product({})]);
    const { setExternalSearch } = renderPage(["catalog.write"]);
    await screen.findByText("Shirt");

    setExternalSearch({ q: "external" });

    expect((screen.getByPlaceholderText("Search products") as HTMLInputElement).value).toBe(
      "external",
    );
    await waitFor(() => {
      expect(lastProductsQuery()?.q).toBe("external");
    });
  });

  it("does not let an external search change clobber in-flight typing", async () => {
    mockEndpoints([product({})]);
    vi.useFakeTimers();
    try {
      const { onSearchChangeSpy, setExternalSearch } = renderPage(["catalog.write"]);
      await act(() => vi.advanceTimersByTimeAsync(0));

      fireEvent.change(screen.getByPlaceholderText("Search products"), {
        target: { value: "sh" },
      });

      // Debounce has not elapsed yet — an external change (Back/Forward,
      // another navigation) lands while the user is still typing.
      setExternalSearch({ q: "old" });
      expect((screen.getByPlaceholderText("Search products") as HTMLInputElement).value).toBe("sh");

      await act(() => vi.advanceTimersByTimeAsync(400));

      expect((screen.getByPlaceholderText("Search products") as HTMLInputElement).value).toBe("sh");
      expect(onSearchChangeSpy).toHaveBeenCalledWith(expect.objectContaining({ q: "sh" }));
    } finally {
      vi.useRealTimers();
    }
  });

  // T6a review MAJOR 2: a cashier has no `catalog.write` and must never
  // request inactive categories for the filter dropdown.
  it("requests /categories without includeInactive for a cashier", async () => {
    mockEndpoints([product({})]);

    renderPage([]);
    await screen.findByText("Shirt");

    await waitFor(() => {
      const categoriesCall = mockedApi.GET.mock.calls.find((call) => call[0] === "/categories");
      expect(categoriesCall).toBeTruthy();
      const query = (
        categoriesCall?.[1] as never as { params?: { query?: { includeInactive?: boolean } } }
      )?.params?.query;
      expect(query?.includeInactive).toBeFalsy();
    });
  });

  // T6a review MAJOR 3: row-click navigation has no keyboard path — an
  // explicit action must exist for catalog.write, and must not exist for a
  // read-only cashier.
  it("shows an explicit Edit action for catalog.write but not for a cashier", async () => {
    mockEndpoints([product({})]);

    renderPage(["catalog.write"]);
    await screen.findByText("Shirt");
    expect(screen.getByRole("button", { name: "Edit" })).toBeTruthy();
    cleanup();

    renderPage([]);
    await screen.findByText("Shirt");
    expect(screen.queryByRole("button", { name: "Edit" })).toBeNull();
  });

  it("pre-fills the search input and category, and queries with them, on reload with ?q=&categoryId=", async () => {
    mockEndpoints([product({})]);

    renderPage(["catalog.write"], { q: "shirt", categoryId: "c1" });

    expect((screen.getByPlaceholderText("Search products") as HTMLInputElement).value).toBe(
      "shirt",
    );
    await waitFor(() => {
      const query = lastProductsQuery();
      expect(query?.q).toBe("shirt");
      expect(query?.categoryId).toBe("c1");
    });
  });

  it("shows the result count line", async () => {
    mockEndpoints([product({})]);

    renderPage();

    expect(await screen.findByText("1 result")).toBeTruthy();
  });

  it("shows the includeInactive switch for catalog.write, pre-checks it from ?includeInactive=true and forwards it to the API", async () => {
    mockEndpoints([product({})]);

    renderPage(["catalog.write"], { includeInactive: true });
    await screen.findByText("Shirt");

    expect(screen.getByRole("switch", { name: "Show inactive" }).getAttribute("aria-checked")).toBe(
      "true",
    );
    await waitFor(() => {
      expect(lastProductsQuery()?.includeInactive).toBe(true);
    });
  });

  it("toggling includeInactive pushes it into the search params (replace)", async () => {
    mockEndpoints([product({})]);
    const { onSearchChangeSpy } = renderPage(["catalog.write"]);
    await screen.findByText("Shirt");

    fireEvent.click(screen.getByRole("switch", { name: "Show inactive" }));

    expect(onSearchChangeSpy).toHaveBeenCalledWith(
      expect.objectContaining({ includeInactive: true }),
    );
  });

  it("hides the includeInactive switch for a cashier and never forwards it to the API even if present in the URL", async () => {
    mockEndpoints([product({})]);

    renderPage([], { includeInactive: true });
    await screen.findByText("Shirt");

    expect(screen.queryByRole("switch", { name: "Show inactive" })).toBeNull();
    await waitFor(() => {
      expect(lastProductsQuery()?.includeInactive).toBeUndefined();
    });
  });

  it("Reset clears the search input, category and includeInactive", async () => {
    mockEndpoints([product({})]);
    const { onSearchChangeSpy } = renderPage(["catalog.write"], {
      q: "shirt",
      categoryId: "c1",
      includeInactive: true,
    });
    expect((screen.getByPlaceholderText("Search products") as HTMLInputElement).value).toBe(
      "shirt",
    );

    fireEvent.click(screen.getByRole("button", { name: "Reset" }));

    expect(onSearchChangeSpy).toHaveBeenCalledWith({});
    expect((screen.getByPlaceholderText("Search products") as HTMLInputElement).value).toBe("");
  });
});
