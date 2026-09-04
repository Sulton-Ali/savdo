import type { components } from "@savdo/api-client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App as AntApp, ConfigProvider } from "antd";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../lib/api", () => ({
  api: { GET: vi.fn(), POST: vi.fn(), PATCH: vi.fn(), DELETE: vi.fn() },
}));

import { AuthProvider } from "../../../auth/AuthContext";
import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { CategoriesPage } from "../CategoriesPage";

type Me = components["schemas"]["Me"];
type Category = components["schemas"]["Category"];

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
      defaultLocale: "en",
      allowNegativeStock: false,
      updateCostOnPurchase: true,
      lowStockThreshold: 2,
    },
    permissions: ["catalog.write"],
  };
}

function category(overrides: Partial<Category>): Category {
  return {
    id: "cat",
    parentId: null,
    slug: "cat",
    sortOrder: 1,
    isActive: true,
    imageId: null,
    name: "Category",
    description: null,
    locale: "en",
    translationFallback: false,
    ...overrides,
  };
}

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

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <AuthProvider me={buildMe()}>
            <CategoriesPage />
          </AuthProvider>
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

describe("CategoriesPage", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    mockedApi.GET.mockReset();
    mockedApi.POST.mockReset();
    mockedApi.PATCH.mockReset();
    mockedApi.DELETE.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("renders nesting from parentId and blocks adding a 4th level", async () => {
    const categories = [
      category({ id: "a", parentId: null, name: "Category A" }),
      category({ id: "b", parentId: "a", name: "Category B" }),
      category({ id: "c", parentId: "b", name: "Category C" }),
    ];
    mockedApi.GET.mockResolvedValue(apiResult({ items: categories }));

    renderPage();

    await screen.findByText("Category A");
    await screen.findByText("Category B");
    await screen.findByText("Category C");

    const addChildButtons = screen.getAllByTitle("Add subcategory") as HTMLButtonElement[];
    expect(addChildButtons).toHaveLength(3);
    // Depth 1 (A) and depth 2 (B) can still gain a child; depth 3 (C) cannot
    // — categories are capped at depth 3 (docs/04-DATA-MODEL.md § 2).
    expect(addChildButtons[0]?.disabled).toBe(false);
    expect(addChildButtons[1]?.disabled).toBe(false);
    expect(addChildButtons[2]?.disabled).toBe(true);
  });

  it("shows an inactive badge for an inactive category", async () => {
    mockedApi.GET.mockResolvedValue(
      apiResult({ items: [category({ id: "a", name: "Category A", isActive: false })] }),
    );

    renderPage();

    await screen.findByText("Category A");
    expect(screen.getByText("Inactive")).toBeTruthy();
  });

  it("maps a 409 CONFLICT on products to the categoryHasProducts notification", async () => {
    mockedApi.GET.mockResolvedValue(
      apiResult({ items: [category({ id: "a", name: "Category A" })] }),
    );
    mockedApi.DELETE.mockResolvedValueOnce(apiError("CONFLICT", { field: "products" }));

    renderPage();

    await screen.findByText("Category A");
    fireEvent.click(screen.getByTitle("Delete"));
    fireEvent.click(await screen.findByRole("button", { name: "OK" }));

    await waitFor(() => {
      expect(
        screen.getByText("This category still has products and can't be deleted."),
      ).toBeTruthy();
    });
  });

  // T6a review MAJOR 1: the edit Drawer must remount per category and read
  // its starting values from `initialValues`, never `setFieldsValue` after
  // mount — the latter marks already-registered fields "touched", which
  // would make the PATCH resend every locale instead of only the one the
  // user actually opened and changed.
  it("patches only the ru locale when only its name is edited", async () => {
    mockedApi.GET.mockResolvedValue(
      apiResult({
        items: [
          category({
            id: "a",
            name: "Category A",
            slug: "cat-a",
            sortOrder: 1,
            translations: {
              uz: { name: "Kategoriya A" },
              en: { name: "Category A" },
            },
          }),
        ],
      }),
    );
    mockedApi.PATCH.mockResolvedValueOnce(apiResult(category({ id: "a", name: "Category A" })));

    renderPage();

    await screen.findByText("Category A");
    fireEvent.click(screen.getByTitle("Edit category"));

    // D-38: all three locales' name fields render together, labelled with
    // the language, instead of behind per-locale tabs.
    fireEvent.change(await screen.findByLabelText("Name (Русский)"), {
      target: { value: "Категория А" },
    });

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mockedApi.PATCH).toHaveBeenCalledWith("/categories/{id}", {
        params: { path: { id: "a" } },
        body: {
          slug: "cat-a",
          sortOrder: 1,
          isActive: true,
          translations: { ru: { name: "Категория А" } },
        },
      });
    });
  });
});
