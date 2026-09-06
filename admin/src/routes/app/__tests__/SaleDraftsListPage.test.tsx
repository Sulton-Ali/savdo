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
  api: { GET: vi.fn() },
}));

import { AuthProvider } from "../../../auth/AuthContext";
import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { SaleDraftsListPage } from "../SaleDraftsListPage";

type Me = components["schemas"]["Me"];
type SaleDraft = components["schemas"]["SaleDraft"];

const mockedApi = vi.mocked(api, { deep: true });

function buildMe(userId: string, role: "owner" | "manager" | "cashier"): Me {
  return {
    user: {
      id: userId,
      username: "user",
      fullName: "Test User",
      phone: null,
      role,
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
    permissions: role === "cashier" ? [] : ["sales.void"],
  };
}

function draft(overrides: Partial<SaleDraft> = {}): SaleDraft {
  return {
    id: "d1",
    locationId: "l1",
    customerId: null,
    discount: null,
    discountReason: null,
    note: "Gift wrap",
    items: [
      {
        variantId: "v1",
        productId: "p1",
        productName: "Shirt",
        variantLabel: "M",
        qty: "1.000",
        unitPrice: "50000.00",
        lineTotal: "50000.00",
        available: true,
      },
    ],
    subtotal: "50000.00",
    discountAmount: "0.00",
    estimatedTotal: "50000.00",
    createdBy: "u1",
    createdAt: "2026-09-06T11:55:00Z",
    updatedAt: "2026-09-06T11:55:00Z",
    ...overrides,
  };
}

function apiResult(data: unknown, status = 200) {
  return { data, error: undefined, response: new Response(null, { status }) } as never;
}

function mockEndpoints(draftItems: SaleDraft[] = [draft()]) {
  mockedApi.GET.mockImplementation(((path: string) => {
    if (path === "/sales/drafts") {
      return Promise.resolve(apiResult({ items: draftItems, nextCursor: null }));
    }
    if (path === "/staff") {
      return Promise.resolve(
        apiResult({
          items: [
            {
              id: "u1",
              username: "cashier1",
              fullName: "Cashier One",
              phone: null,
              role: "cashier",
              locale: "en",
              isActive: true,
              lastLoginAt: null,
              createdAt: "2026-01-01T00:00:00Z",
            },
          ],
          nextCursor: null,
        }),
      );
    }
    if (path === "/customers/{id}") {
      return Promise.resolve(
        apiResult({
          id: "c1",
          fullName: "Jane Doe",
          phone: null,
          telegramUsername: null,
          note: null,
          tags: [],
        }),
      );
    }
    return Promise.resolve(apiResult({ items: [], nextCursor: null }));
  }) as never);
}

function renderPage(userId = "u1", role: "owner" | "manager" | "cashier" = "manager") {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <AuthProvider me={buildMe(userId, role)}>
            <SaleDraftsListPage />
          </AuthProvider>
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

describe("SaleDraftsListPage", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    navigateMock.mockReset();
    mockedApi.GET.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("renders drafts returned by GET /sales/drafts", async () => {
    mockEndpoints();
    renderPage();

    expect(await screen.findByText("Gift wrap")).toBeTruthy();
    expect(screen.getByText("50,000")).toBeTruthy();
    expect(screen.getByText("1")).toBeTruthy();
  });

  it("shows 'You' for the caller's own draft", async () => {
    mockEndpoints([draft({ createdBy: "u1" })]);
    renderPage("u1");

    expect(await screen.findByText("You")).toBeTruthy();
  });

  it("navigates to the draft detail page on row click", async () => {
    mockEndpoints();
    renderPage();
    fireEvent.click(await screen.findByText("Gift wrap"));

    expect(navigateMock).toHaveBeenCalledWith({ to: "/sales/drafts/$id", params: { id: "d1" } });
  });

  it("passes createdBy=<me> through to GET /sales/drafts when 'my drafts only' is checked", async () => {
    mockEndpoints();
    renderPage("u1");
    await screen.findByText("Gift wrap");

    fireEvent.click(screen.getByText("My drafts only"));

    await waitFor(() => {
      const matched = mockedApi.GET.mock.calls.some(
        (call) =>
          call[0] === "/sales/drafts" &&
          (call[1] as never as { params: { query: { createdBy?: string } } })?.params?.query
            ?.createdBy === "u1",
      );
      expect(matched).toBe(true);
    });
  });
});
