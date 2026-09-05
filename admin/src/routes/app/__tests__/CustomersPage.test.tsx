import type { components } from "@savdo/api-client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App as AntApp, ConfigProvider } from "antd";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

const navigateMock = vi.fn();

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>();
  return { ...actual, useNavigate: () => navigateMock };
});

vi.mock("../../../lib/api", () => ({
  api: { GET: vi.fn(), POST: vi.fn(), PATCH: vi.fn(), DELETE: vi.fn() },
}));

import { AuthProvider } from "../../../auth/AuthContext";
import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { CustomersPage } from "../CustomersPage";

type Me = components["schemas"]["Me"];
type Customer = components["schemas"]["Customer"];

const mockedApi = vi.mocked(api, { deep: true });

function buildMe(permissions: string[]): Me {
  return {
    user: {
      id: "u1",
      username: "manager",
      fullName: "Test User",
      phone: null,
      role: permissions.includes("customers.write") ? "manager" : "cashier",
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

function customer(overrides: Partial<Customer> = {}): Customer {
  return {
    id: "c1",
    fullName: "Jane Doe",
    phone: "+998901112233",
    telegramUsername: "janedoe",
    note: null,
    tags: [],
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function renderPage(permissions: string[] = ["customers.write"]) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <AuthProvider me={buildMe(permissions)}>
            <CustomersPage />
          </AuthProvider>
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

function emptyCustomersList() {
  return {
    data: { items: [], nextCursor: null },
    error: undefined,
    response: new Response(null, { status: 200 }),
  } as never;
}

describe("CustomersPage", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    navigateMock.mockReset();
    mockedApi.GET.mockReset();
    mockedApi.POST.mockReset();
    mockedApi.PATCH.mockReset();
    mockedApi.DELETE.mockReset();
    mockedApi.GET.mockResolvedValue(emptyCustomersList());
  });

  afterEach(() => {
    cleanup();
  });

  it("renders the customers returned by GET /customers", async () => {
    mockedApi.GET.mockResolvedValue({
      data: { items: [customer()], nextCursor: null },
      error: undefined,
      response: new Response(null, { status: 200 }),
    } as never);

    renderPage();

    expect(await screen.findByText("Jane Doe")).toBeTruthy();
    expect(screen.getByText("+998901112233")).toBeTruthy();
    expect(screen.getByText("janedoe")).toBeTruthy();
  });

  it("debounces search and fires a request with q only at 2+ characters", async () => {
    renderPage();
    await waitFor(() => expect(mockedApi.GET).toHaveBeenCalled());

    const searchInput = screen.getByPlaceholderText("Search customers");

    fireEvent.change(searchInput, { target: { value: "j" } });
    await new Promise((resolve) => setTimeout(resolve, 400));
    expect(
      mockedApi.GET.mock.calls.some(
        (call) =>
          call[0] === "/customers" &&
          (call[1] as never as { params: { query: { q?: string } } })?.params?.query?.q === "j",
      ),
    ).toBe(false);

    fireEvent.change(searchInput, { target: { value: "ja" } });
    await waitFor(
      () => {
        const matched = mockedApi.GET.mock.calls.some(
          (call) =>
            call[0] === "/customers" &&
            (call[1] as never as { params: { query: { q?: string } } })?.params?.query?.q === "ja",
        );
        expect(matched).toBe(true);
      },
      { timeout: 2000 },
    );
  });

  it("navigates to the customer detail page from the View action", async () => {
    mockedApi.GET.mockResolvedValue({
      data: { items: [customer()], nextCursor: null },
      error: undefined,
      response: new Response(null, { status: 200 }),
    } as never);

    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "View" }));

    expect(navigateMock).toHaveBeenCalledWith({
      to: "/customers/$id",
      params: { id: "c1" },
    });
  });

  it("posts the exact CustomerCreate payload and closes the modal on success", async () => {
    mockedApi.POST.mockResolvedValueOnce({
      data: customer({ id: "c2", fullName: "New Customer", phone: null, telegramUsername: null }),
      error: undefined,
      response: new Response(null, { status: 201 }),
    } as never);

    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Add customer" }));

    fireEvent.change(await screen.findByLabelText("Full name"), {
      target: { value: "New Customer" },
    });

    fireEvent.click(screen.getByRole("button", { name: "OK" }));

    await waitFor(() => {
      expect(mockedApi.POST).toHaveBeenCalledWith("/customers", {
        body: { fullName: "New Customer" },
      });
    });

    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
  });

  it("shows a field error on phone for a 409 CONFLICT with details.field", async () => {
    mockedApi.POST.mockResolvedValueOnce({
      data: undefined,
      error: { error: { code: "CONFLICT", details: { field: "phone" } } },
      response: new Response(null, { status: 409 }),
    } as never);

    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Add customer" }));

    fireEvent.change(await screen.findByLabelText("Full name"), {
      target: { value: "Dup Customer" },
    });
    fireEvent.change(await screen.findByLabelText("Phone"), {
      target: { value: "+998900000000" },
    });

    fireEvent.click(screen.getByRole("button", { name: "OK" }));

    expect(await screen.findByText("This value is already in use")).toBeTruthy();
    expect(screen.getByRole("dialog")).toBeTruthy();
  });

  it("shows an explicit Edit action for customers.write but not for a cashier, and submits the PATCH", async () => {
    mockedApi.GET.mockResolvedValue({
      data: { items: [customer()], nextCursor: null },
      error: undefined,
      response: new Response(null, { status: 200 }),
    } as never);
    mockedApi.PATCH.mockResolvedValueOnce({
      data: customer({ fullName: "Jane Updated" }),
      error: undefined,
      response: new Response(null, { status: 200 }),
    } as never);

    renderPage(["customers.write"]);
    await screen.findByText("Jane Doe");
    expect(screen.getByRole("button", { name: "Edit customer" })).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Edit customer" }));
    fireEvent.change(await screen.findByLabelText("Full name"), {
      target: { value: "Jane Updated" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mockedApi.PATCH).toHaveBeenCalledWith("/customers/{id}", {
        params: { path: { id: "c1" } },
        body: {
          fullName: "Jane Updated",
          phone: "+998901112233",
          telegramUsername: "janedoe",
          note: null,
          tags: [],
        },
      });
    });
    cleanup();

    mockedApi.GET.mockResolvedValue({
      data: { items: [customer()], nextCursor: null },
      error: undefined,
      response: new Response(null, { status: 200 }),
    } as never);
    renderPage([]);
    await screen.findByText("Jane Doe");
    expect(screen.queryByRole("button", { name: "Edit customer" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Delete" })).toBeNull();
  });

  it("deletes a customer after the confirm popconfirm", async () => {
    mockedApi.GET.mockResolvedValue({
      data: { items: [customer()], nextCursor: null },
      error: undefined,
      response: new Response(null, { status: 200 }),
    } as never);
    mockedApi.DELETE.mockResolvedValueOnce({
      data: undefined,
      error: undefined,
      response: new Response(null, { status: 204 }),
    } as never);

    renderPage(["customers.write"]);

    fireEvent.click(await screen.findByRole("button", { name: "Delete" }));
    fireEvent.click(await screen.findByRole("button", { name: "OK" }));

    await waitFor(() => {
      expect(mockedApi.DELETE).toHaveBeenCalledWith("/customers/{id}", {
        params: { path: { id: "c1" } },
      });
    });
  });
});
