import type { components } from "@savdo/api-client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App as AntApp, ConfigProvider } from "antd";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

const { mockNavigate } = vi.hoisted(() => ({ mockNavigate: vi.fn() }));

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>();
  return { ...actual, useNavigate: () => mockNavigate };
});

vi.mock("../../../lib/api", () => ({
  api: { GET: vi.fn(), POST: vi.fn(), PATCH: vi.fn() },
}));

import { AuthProvider } from "../../../auth/AuthContext";
import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { ProductFormPage } from "../ProductFormPage";

type Me = components["schemas"]["Me"];
type Product = components["schemas"]["Product"];
type Unit = components["schemas"]["Unit"];

const mockedApi = vi.mocked(api, { deep: true });

function buildMe(): Me {
  return {
    user: {
      id: "u1",
      username: "manager",
      fullName: "Test Manager",
      phone: null,
      role: "manager",
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
      defaultLocale: "uz",
      allowNegativeStock: false,
      updateCostOnPurchase: true,
    },
    permissions: ["catalog.write"],
  };
}

function unit(): Unit {
  return {
    id: "unit1",
    code: "pcs",
    precision: 0,
    name: "Pieces",
    locale: "en",
    translationFallback: false,
  };
}

function jsonResult(data: unknown, status = 200) {
  return { data, error: undefined, response: new Response(null, { status }) } as never;
}

function renderForm(productId?: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <AuthProvider me={buildMe()}>
            <ProductFormPage productId={productId} />
          </AuthProvider>
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

function mockGetByPath(handlers: Record<string, unknown>) {
  mockedApi.GET.mockImplementation(((path: string) => {
    if (path in handlers) {
      return Promise.resolve(jsonResult(handlers[path]));
    }
    throw new Error(`unexpected GET ${path}`);
  }) as never);
}

describe("ProductFormPage", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    mockedApi.GET.mockReset();
    mockedApi.POST.mockReset();
    mockedApi.PATCH.mockReset();
    mockNavigate.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("posts the exact ProductCreate body — money as strings, translations as an object", async () => {
    mockGetByPath({
      "/categories": { items: [] },
      "/units": { items: [unit()] },
    });
    mockedApi.POST.mockResolvedValueOnce(
      jsonResult(
        {
          id: "new1",
          categoryId: null,
          slug: "t-shirt",
          sku: null,
          unitId: "unit1",
          basePrice: "100.00",
          promoPrice: null,
          promoFrom: null,
          promoTo: null,
          isActive: true,
          isFeatured: false,
          name: "T-Shirt",
          description: null,
          locale: "uz",
          translationFallback: false,
        },
        201,
      ),
    );

    renderForm(undefined);

    // D-38: all three locales' name fields render together, labelled with
    // the language — "uz" is the shop default locale, so only its name is
    // required.
    fireEvent.change(await screen.findByLabelText("Name (Oʻzbekcha)"), {
      target: { value: "T-Shirt" },
    });

    fireEvent.mouseDown(screen.getByLabelText("Unit"));
    fireEvent.click(await screen.findByText("Pieces"));

    fireEvent.click(screen.getByRole("tab", { name: "Prices" }));
    fireEvent.change(await screen.findByLabelText("Base price"), {
      target: { value: "100" },
    });

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mockedApi.POST).toHaveBeenCalledWith("/products", {
        body: {
          unitId: "unit1",
          basePrice: "100.00",
          translations: { uz: { name: "T-Shirt" } },
          isActive: true,
          isFeatured: false,
        },
      });
    });
  });

  it("patches only the promo fields, as null, when the promo is cleared and nothing else changes", async () => {
    const existing: Product = {
      id: "p1",
      categoryId: null,
      slug: "existing-product",
      sku: null,
      unitId: "unit1",
      basePrice: "10000.00",
      promoPrice: "8000.00",
      promoFrom: "2026-01-01T00:00:00Z",
      promoTo: "2026-01-31T00:00:00Z",
      isActive: true,
      isFeatured: false,
      name: "Existing product",
      description: null,
      locale: "uz",
      translationFallback: false,
      translations: { uz: { name: "Existing product" } },
    };
    mockGetByPath({
      "/categories": { items: [] },
      "/units": { items: [unit()] },
      "/products/{id}": existing,
    });
    mockedApi.PATCH.mockResolvedValueOnce(
      jsonResult({ ...existing, promoPrice: null, promoFrom: null, promoTo: null }),
    );

    const { container } = renderForm("p1");

    await screen.findByRole("button", { name: "Save" });
    fireEvent.click(screen.getByRole("tab", { name: "Prices" }));

    const promoPriceInput = await screen.findByLabelText("Promo price");
    fireEvent.change(promoPriceInput, { target: { value: "" } });

    const clearRange = container.querySelector(".ant-picker-clear");
    expect(clearRange).toBeTruthy();
    if (clearRange) {
      fireEvent.click(clearRange);
    }

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mockedApi.PATCH).toHaveBeenCalledWith("/products/{id}", {
        params: { path: { id: "p1" } },
        body: {
          promoPrice: null,
          promoFrom: null,
          promoTo: null,
        },
      });
    });
  });

  // T6a review MINOR: a 409 CONFLICT naming a real, mounted field (slug is a
  // top-level Form.Item, unlike the nested translation fields) must still
  // land as an inline field error.
  it("maps a 409 CONFLICT on slug to the slug field", async () => {
    mockGetByPath({
      "/categories": { items: [] },
      "/units": { items: [unit()] },
    });
    mockedApi.POST.mockResolvedValueOnce({
      data: undefined,
      error: { error: { code: "CONFLICT", details: { field: "slug" } } },
      response: new Response(null, { status: 409 }),
    } as never);

    renderForm(undefined);

    fireEvent.change(await screen.findByLabelText("Name (Oʻzbekcha)"), {
      target: { value: "T-Shirt" },
    });
    fireEvent.mouseDown(screen.getByLabelText("Unit"));
    fireEvent.click(await screen.findByText("Pieces"));
    fireEvent.click(screen.getByRole("tab", { name: "Prices" }));
    fireEvent.change(await screen.findByLabelText("Base price"), {
      target: { value: "100" },
    });

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText("This value is already in use")).toBeTruthy();
  });

  // T6a review MINOR: a 409 CONFLICT naming a field with no matching
  // mounted Form.Item — a flat "name" from a translation entry, which every
  // locale here renders as a nested `["translations", locale, "name"]`
  // path — must fall back to a page-level notification instead of being
  // silently swallowed by `form.setFields` on a field nothing displays.
  it("shows a page-level notification for a 409 CONFLICT on a field with no matching Form.Item", async () => {
    mockGetByPath({
      "/categories": { items: [] },
      "/units": { items: [unit()] },
    });
    mockedApi.POST.mockResolvedValueOnce({
      data: undefined,
      error: { error: { code: "CONFLICT", details: { field: "name" } } },
      response: new Response(null, { status: 409 }),
    } as never);

    renderForm(undefined);

    fireEvent.change(await screen.findByLabelText("Name (Oʻzbekcha)"), {
      target: { value: "T-Shirt" },
    });
    fireEvent.mouseDown(screen.getByLabelText("Unit"));
    fireEvent.click(await screen.findByText("Pieces"));
    fireEvent.click(screen.getByRole("tab", { name: "Prices" }));
    fireEvent.change(await screen.findByLabelText("Base price"), {
      target: { value: "100" },
    });

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText("Something went wrong. Please try again.")).toBeTruthy();
  });

  // T6b: the Variants & images tab hangs off a product id, so it must stay
  // disabled until the product exists.
  it("disables the Variants & images tab in create mode", async () => {
    mockGetByPath({
      "/categories": { items: [] },
      "/units": { items: [unit()] },
    });

    renderForm(undefined);

    const tab = await screen.findByRole("tab", { name: "Variants & images" });
    expect(tab.getAttribute("aria-disabled")).toBe("true");
  });

  it("renders the variants matrix and image gallery once an existing product is loaded", async () => {
    const existing: Product = {
      id: "p1",
      categoryId: null,
      slug: "existing-product",
      sku: null,
      unitId: "unit1",
      basePrice: "10000.00",
      promoPrice: null,
      promoFrom: null,
      promoTo: null,
      isActive: true,
      isFeatured: false,
      name: "Existing product",
      description: null,
      locale: "uz",
      translationFallback: false,
      translations: { uz: { name: "Existing product" } },
      variants: [],
      images: [],
    };
    mockGetByPath({
      "/categories": { items: [] },
      "/units": { items: [unit()] },
      "/products/{id}": existing,
      "/attribute-definitions": { items: [] },
      "/products/{id}/variants": { items: [] },
    });

    renderForm("p1");

    fireEvent.click(await screen.findByRole("tab", { name: "Variants & images" }));

    expect(await screen.findByText("Generate variants")).toBeTruthy();
    expect(screen.getByText("Images")).toBeTruthy();
  });

  // D-39: breadcrumb + back arrow on the edit and create pages, both
  // navigating to the product list route.
  it("shows a Products › <product name> breadcrumb and navigates back to the list", async () => {
    const existing: Product = {
      id: "p1",
      categoryId: null,
      slug: "existing-product",
      sku: null,
      unitId: "unit1",
      basePrice: "10000.00",
      promoPrice: null,
      promoFrom: null,
      promoTo: null,
      isActive: true,
      isFeatured: false,
      name: "Existing product",
      description: null,
      locale: "uz",
      translationFallback: false,
      translations: { uz: { name: "Existing product" } },
    };
    mockGetByPath({
      "/categories": { items: [] },
      "/units": { items: [unit()] },
      "/products/{id}": existing,
    });

    renderForm("p1");

    expect(await screen.findByText("Existing product")).toBeTruthy();
    expect(screen.getByText("Products")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Back" }));

    expect(mockNavigate).toHaveBeenCalledWith({ to: "/products" });
  });

  it("shows a Products › New product breadcrumb in create mode", async () => {
    mockGetByPath({
      "/categories": { items: [] },
      "/units": { items: [unit()] },
    });

    renderForm(undefined);

    expect(await screen.findByText("New product")).toBeTruthy();
    expect(screen.getByText("Products")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Back" }));

    expect(mockNavigate).toHaveBeenCalledWith({ to: "/products" });
  });
});
