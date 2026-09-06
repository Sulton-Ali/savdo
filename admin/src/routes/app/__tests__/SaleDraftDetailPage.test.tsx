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
import { SaleDraftDetailPage } from "../SaleDraftDetailPage";

type Me = components["schemas"]["Me"];
type SaleDraft = components["schemas"]["SaleDraft"];
type Sale = components["schemas"]["Sale"];

const mockedApi = vi.mocked(api, { deep: true });

function buildMe(userId: string, permissions: string[]): Me {
  return {
    user: {
      id: userId,
      username: "user",
      fullName: "Test User",
      phone: null,
      role: permissions.includes("sales.void") ? "manager" : "cashier",
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

function draftFixture(overrides: Partial<SaleDraft> = {}): SaleDraft {
  return {
    id: "d1",
    locationId: "l1",
    customerId: null,
    customerName: null,
    discount: null,
    discountReason: null,
    note: "Ready for pickup",
    items: [
      {
        variantId: "v1",
        productId: "p1",
        productName: "Shirt",
        variantLabel: "M",
        qty: "2.000",
        unitPrice: "50000.00",
        lineTotal: "100000.00",
        available: true,
      },
    ],
    subtotal: "100000.00",
    discountAmount: "0.00",
    estimatedTotal: "100000.00",
    createdBy: "creator1",
    createdByName: "Creator One",
    createdAt: "2026-09-06T10:00:00Z",
    updatedAt: "2026-09-06T10:00:00Z",
    ...overrides,
  };
}

function saleFixture(): Sale {
  return {
    id: "sale1",
    number: 42,
    kind: "sale",
    status: "completed",
    locationId: "l1",
    locationName: "Main Store",
    customerId: null,
    customerName: null,
    cashierId: "u1",
    cashierName: "Cashier One",
    originalSaleId: null,
    subtotal: "100000.00",
    discountAmount: "0.00",
    discountReason: null,
    total: "100000.00",
    note: null,
    completedAt: "2026-09-06T10:05:00Z",
    voidedAt: null,
    voidedBy: null,
    voidReason: null,
    payment: { method: "cash", amount: "100000.00" },
    items: [],
    hasReturns: false,
  };
}

function apiResult(data: unknown, status = 200) {
  return { data, error: undefined, response: new Response(null, { status }) } as never;
}

function apiError(code: string, details: Record<string, unknown> = {}, status = 409) {
  return {
    data: undefined,
    error: { error: { code, details } },
    response: new Response(null, { status }),
  } as never;
}

function mockGet(draft: SaleDraft) {
  mockedApi.GET.mockImplementation(((path: string) => {
    if (path === "/sales/drafts/{id}") {
      return Promise.resolve(apiResult(draft));
    }
    if (path === "/locations") {
      return Promise.resolve(
        apiResult({
          items: [{ id: "l1", name: "Main Store", kind: "store", isDefault: true, isActive: true }],
          nextCursor: null,
        }),
      );
    }
    throw new Error(`unexpected GET ${path}`);
  }) as never);
}

function renderPage(draft: SaleDraft, userId: string, permissions: string[] = []) {
  mockGet(draft);
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <AuthProvider me={buildMe(userId, permissions)}>
            <SaleDraftDetailPage draftId={draft.id} />
          </AuthProvider>
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

describe("SaleDraftDetailPage", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    navigateMock.mockReset();
    mockedApi.GET.mockReset();
    mockedApi.POST.mockReset();
    mockedApi.PATCH.mockReset();
    mockedApi.DELETE.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("renders the draft's fields, lines and totals", async () => {
    renderPage(draftFixture(), "someone-else", ["sales.void"]);

    expect(await screen.findByText("Shirt")).toBeTruthy();
    expect(screen.getByText("M")).toBeTruthy();
    expect(screen.getByText("Main Store")).toBeTruthy();
    expect(screen.getByText("Ready for pickup")).toBeTruthy();
    // `formatMoneyDisplay` drops the trailing ".00" and adds thousands
    // separators (`lib/money.ts`) — the line total, subtotal and estimated
    // total are all "100000.00" on this single-line fixture.
    expect(screen.getAllByText("100,000").length).toBeGreaterThanOrEqual(2);
  });

  it("shows the server-resolved createdByName for another staff member's draft", async () => {
    renderPage(
      draftFixture({ createdBy: "creator1", createdByName: "Creator One" }),
      "someone-else",
      ["sales.void"],
    );

    expect(await screen.findByText("Creator One")).toBeTruthy();
  });

  it("shows 'You' for the caller's own draft even though createdByName is set", async () => {
    renderPage(draftFixture({ createdBy: "u1", createdByName: "Test User" }), "u1", []);

    expect(await screen.findByText("You")).toBeTruthy();
  });

  it("shows a dash when createdByName is null", async () => {
    renderPage(draftFixture({ createdBy: null, createdByName: null }), "u1", []);

    await screen.findByText("Shirt");
    // Customer and discount are also "—" on this fixture (neither set) —
    // created by is the third.
    expect(screen.getAllByText("—")).toHaveLength(3);
  });

  it("shows the server-resolved customerName for the draft's attached customer", async () => {
    renderPage(draftFixture({ customerId: "c1", customerName: "Jane Doe" }), "someone-else", [
      "sales.void",
    ]);

    expect(await screen.findByText("Jane Doe")).toBeTruthy();
  });

  it("appends the discount reason to the discount field", async () => {
    renderPage(
      draftFixture({
        discount: { type: "percent", value: "10.00" },
        discountReason: "loyal customer",
      }),
      "someone-else",
      ["sales.void"],
    );

    expect(await screen.findByText("Percent 10.00% — loyal customer")).toBeTruthy();
  });

  it("tags an unavailable line and excludes it visually from the price columns", async () => {
    renderPage(
      draftFixture({
        items: [
          {
            variantId: "v1",
            productId: "p1",
            productName: "Discontinued Shirt",
            variantLabel: "M",
            qty: "1.000",
            unitPrice: "0.00",
            lineTotal: "0.00",
            available: false,
          },
        ],
      }),
      "someone-else",
      ["sales.void"],
    );

    expect(await screen.findByText("Discontinued Shirt")).toBeTruthy();
    expect(screen.getByText("Unavailable")).toBeTruthy();
  });

  it("always shows Pay regardless of creator (D-96)", async () => {
    renderPage(draftFixture({ createdBy: "someone-else" }), "u1", []);
    expect(await screen.findByRole("button", { name: "Pay" })).toBeTruthy();
  });

  it("shows Edit/Delete for the draft's own creator even without manager+", async () => {
    renderPage(draftFixture({ createdBy: "u1" }), "u1", []);
    expect(await screen.findByRole("button", { name: "Edit" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Delete" })).toBeTruthy();
  });

  it("hides Edit/Delete for a non-creator cashier", async () => {
    renderPage(draftFixture({ createdBy: "someone-else" }), "u1", []);
    await screen.findByText("Shirt");
    expect(screen.queryByRole("button", { name: "Edit" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Delete" })).toBeNull();
  });

  it("shows Edit/Delete for manager+ regardless of creator", async () => {
    renderPage(draftFixture({ createdBy: "someone-else" }), "u1", ["sales.void"]);
    expect(await screen.findByRole("button", { name: "Edit" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Delete" })).toBeTruthy();
  });

  it("hides Edit/Delete for anyone but manager+ when the draft has no creator on record", async () => {
    renderPage(draftFixture({ createdBy: null }), "u1", []);
    await screen.findByText("Shirt");
    expect(screen.queryByRole("button", { name: "Edit" })).toBeNull();
  });

  it("completes the draft with an Idempotency-Key and navigates to the created sale", async () => {
    renderPage(draftFixture(), "u1", []);
    fireEvent.click(await screen.findByRole("button", { name: "Pay" }));

    mockedApi.POST.mockResolvedValueOnce(apiResult(saleFixture(), 201));

    fireEvent.click(await screen.findByRole("button", { name: "Complete sale" }));

    await waitFor(() => {
      expect(mockedApi.POST).toHaveBeenCalledWith(
        "/sales/drafts/{id}/complete",
        expect.objectContaining({
          params: expect.objectContaining({
            path: { id: "d1" },
            header: expect.objectContaining({
              "Idempotency-Key": expect.any(String),
            }),
          }),
          body: { paymentMethod: "cash" },
        }),
      );
    });

    await waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith({ to: "/sales/$id", params: { id: "sale1" } });
    });
  });

  it("shows the mapped stock-insufficient message on complete", async () => {
    renderPage(draftFixture(), "u1", []);
    fireEvent.click(await screen.findByRole("button", { name: "Pay" }));

    mockedApi.POST.mockResolvedValueOnce(
      apiError("STOCK_INSUFFICIENT", { variantId: "v1", available: "1.000" }),
    );
    fireEvent.click(await screen.findByRole("button", { name: "Complete sale" }));

    expect(await screen.findByText("Not enough stock: only 1.000 available.")).toBeTruthy();
  });

  it("deletes the draft and navigates back to the list", async () => {
    renderPage(draftFixture({ createdBy: "u1" }), "u1", []);
    fireEvent.click(await screen.findByRole("button", { name: "Delete" }));

    mockedApi.DELETE.mockResolvedValueOnce(apiResult(undefined, 204));
    fireEvent.click(await screen.findByRole("button", { name: "OK" }));

    await waitFor(() => {
      expect(mockedApi.DELETE).toHaveBeenCalledWith("/sales/drafts/{id}", {
        params: { path: { id: "d1" } },
      });
    });
    await waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith({ to: "/sales/drafts" });
    });
  });
});
