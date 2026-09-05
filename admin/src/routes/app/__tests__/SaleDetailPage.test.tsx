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
  api: { GET: vi.fn(), POST: vi.fn() },
}));

import { AuthProvider } from "../../../auth/AuthContext";
import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { SaleDetailPage } from "../SaleDetailPage";

type Me = components["schemas"]["Me"];
type Sale = components["schemas"]["Sale"];

const mockedApi = vi.mocked(api, { deep: true });

function buildMe(permissions: string[]): Me {
  return {
    user: {
      id: "u1",
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

function saleFixture(overrides: Partial<Sale> = {}): Sale {
  return {
    id: "sale1",
    number: 100,
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
    completedAt: "2026-02-01T10:00:00Z",
    voidedAt: null,
    voidedBy: null,
    voidReason: null,
    payment: { method: "cash", amount: "100000.00" },
    items: [
      {
        id: "item1",
        variantId: "v1",
        productId: "p1",
        productName: "Shirt",
        variantLabel: "M",
        qty: "2.000",
        unitPrice: "50000.00",
        lineTotal: "100000.00",
        returnedQty: "0.000",
      },
    ],
    hasReturns: false,
    ...overrides,
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

function renderPage(sale: Sale, permissions: string[] = ["sales.void"]) {
  mockedApi.GET.mockImplementation(((path: string) => {
    if (path === "/sales/{id}") {
      return Promise.resolve(apiResult(sale));
    }
    throw new Error(`unexpected GET ${path}`);
  }) as never);

  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <AuthProvider me={buildMe(permissions)}>
            <SaleDetailPage saleId={sale.id} />
          </AuthProvider>
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

describe("SaleDetailPage", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    navigateMock.mockReset();
    mockedApi.GET.mockReset();
    mockedApi.POST.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("renders the sale's items and hides the cost column when unitCost is absent", async () => {
    renderPage(saleFixture());

    expect(await screen.findByText("Shirt")).toBeTruthy();
    expect(screen.getByText("M")).toBeTruthy();
    expect(screen.getByText("50000.00")).toBeTruthy();
    // The sale's subtotal, item line total and grand total are all
    // "100000.00" for this single-line fixture — assert the count instead
    // of a single unique match.
    expect(screen.getAllByText("100000.00").length).toBeGreaterThanOrEqual(3);
    expect(screen.queryByText("Unit cost")).toBeNull();
  });

  it("shows the cost column when at least one item carries unitCost", async () => {
    renderPage(
      saleFixture({
        items: [{ ...saleFixture().items[0], unitCost: "30000.00" } as Sale["items"][number]],
      }),
    );

    expect(await screen.findByText("Shirt")).toBeTruthy();
    expect(screen.getByText("Unit cost")).toBeTruthy();
    expect(screen.getByText("30000.00")).toBeTruthy();
  });

  it("shows the void button for a manager on a completed sale with no returns", async () => {
    renderPage(saleFixture(), ["sales.void"]);
    expect(await screen.findByRole("button", { name: "Void sale" })).toBeTruthy();
  });

  it("hides the void button for a cashier", async () => {
    renderPage(saleFixture(), []);
    await screen.findByText("Shirt");
    expect(screen.queryByRole("button", { name: "Void sale" })).toBeNull();
  });

  it("hides the void button when the sale is already voided", async () => {
    renderPage(saleFixture({ status: "voided", voidedAt: "2026-02-01T12:00:00Z" }), ["sales.void"]);
    await screen.findByText("Shirt");
    expect(screen.queryByRole("button", { name: "Void sale" })).toBeNull();
  });

  it("hides the void button when the sale has returns", async () => {
    renderPage(saleFixture({ hasReturns: true }), ["sales.void"]);
    await screen.findByText("Shirt");
    expect(screen.queryByRole("button", { name: "Void sale" })).toBeNull();
  });

  it("hides the void button for a return-kind sale", async () => {
    renderPage(saleFixture({ kind: "return", originalSaleId: "sale0" }), ["sales.void"]);
    await screen.findByText("Shirt");
    expect(screen.queryByRole("button", { name: "Void sale" })).toBeNull();
  });

  it("hides the return button for a return-kind sale (a return is itself final, D-66)", async () => {
    renderPage(saleFixture({ kind: "return", originalSaleId: "sale0" }), ["sales.void"]);
    await screen.findByText("Shirt");
    expect(screen.queryByRole("button", { name: "Return" })).toBeNull();
  });

  it("hides the return button when the sale is already voided", async () => {
    renderPage(saleFixture({ status: "voided", voidedAt: "2026-02-01T12:00:00Z" }), ["sales.void"]);
    await screen.findByText("Shirt");
    expect(screen.queryByRole("button", { name: "Return" })).toBeNull();
  });

  it("keeps the return button visible when the sale already has a partial return (further returns are allowed)", async () => {
    renderPage(saleFixture({ hasReturns: true }), ["sales.void"]);
    expect(await screen.findByRole("button", { name: "Return" })).toBeTruthy();
  });

  it("posts the reason and shows the mapped message on SALE_VOID_WINDOW_CLOSED", async () => {
    renderPage(saleFixture(), ["sales.void"]);
    fireEvent.click(await screen.findByRole("button", { name: "Void sale" }));

    fireEvent.change(await screen.findByLabelText("Reason"), {
      target: { value: "Customer changed their mind" },
    });

    mockedApi.POST.mockResolvedValueOnce(apiError("SALE_VOID_WINDOW_CLOSED"));

    fireEvent.click(screen.getByRole("button", { name: "OK" }));

    await waitFor(() => {
      expect(mockedApi.POST).toHaveBeenCalledWith("/sales/{id}/void", {
        params: { path: { id: "sale1" } },
        body: { reason: "Customer changed their mind" },
      });
    });

    expect(
      await screen.findByText("This sale can no longer be voided — its calendar day has passed."),
    ).toBeTruthy();
  });

  it("shows the mapped message on SALE_NOT_VOIDABLE", async () => {
    // Unreachable through the normal UI — the void button is hidden for a
    // return-kind sale — but the server remains the enforcement point
    // (ADR-010), so a stale page or direct navigation still gets a specific
    // message rather than the generic fallback (D-66).
    renderPage(saleFixture(), ["sales.void"]);
    fireEvent.click(await screen.findByRole("button", { name: "Void sale" }));

    mockedApi.POST.mockResolvedValueOnce(apiError("SALE_NOT_VOIDABLE"));

    fireEvent.click(screen.getByRole("button", { name: "OK" }));

    expect(
      await screen.findByText(
        "This is a return and cannot be voided. To correct a mistaken return, sell the item again.",
      ),
    ).toBeTruthy();
  });

  it("caps the return qty at sold minus returned, posts the exact body with an Idempotency-Key, and navigates to the new sale", async () => {
    // AntD's `InputNumber` doesn't expose a way to drive a typed value
    // through jsdom's `fireEvent.change` reliably (its internal input
    // tracking ignores a programmatically dispatched native event), so this
    // exercises the same cap via the up-step control instead — sold 2,
    // already returned 1, so the cap is 1 and a second increase must be a
    // no-op.
    renderPage(
      saleFixture({
        items: [{ ...saleFixture().items[0], qty: "2", returnedQty: "1" } as Sale["items"][number]],
      }),
      ["sales.void"],
    );

    fireEvent.click(await screen.findByRole("button", { name: "Return" }));

    const increase = (await screen.findAllByLabelText("Increase Value"))[0] as HTMLElement;
    const qtyInput = (await screen.findAllByRole("spinbutton"))[0] as HTMLInputElement;
    fireEvent.mouseDown(increase);
    fireEvent.mouseUp(increase);
    await waitFor(() => expect(qtyInput.value).toBe("1"));

    // A second increase must not push the value past the cap.
    fireEvent.mouseDown(increase);
    fireEvent.mouseUp(increase);
    await waitFor(() => expect(qtyInput.value).toBe("1"));

    fireEvent.change(screen.getByLabelText("Note"), { target: { value: "Wrong size" } });

    mockedApi.POST.mockResolvedValueOnce(
      apiResult({ ...saleFixture(), id: "return1", kind: "return", originalSaleId: "sale1" }, 201),
    );

    fireEvent.click(screen.getByRole("button", { name: "Submit return" }));

    await waitFor(() => {
      expect(mockedApi.POST).toHaveBeenCalledTimes(1);
    });

    const call = mockedApi.POST.mock.calls[0];
    expect(call?.[0]).toBe("/sales/{id}/return");
    const options = call?.[1] as unknown as {
      params: { path: { id: string }; header: { "Idempotency-Key": string } };
      body: { items: { saleItemId: string; qty: string }[]; note?: string };
    };
    expect(options.params.path.id).toBe("sale1");
    expect(options.params.header["Idempotency-Key"]).toBeTruthy();
    expect(options.body).toEqual({
      items: [{ saleItemId: "item1", qty: "1" }],
      note: "Wrong size",
    });

    await waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith({
        to: "/sales/$id",
        params: { id: "return1" },
      });
    });
  });

  it("computes the return cap and step for a realistic 3-decimal line, and disables a fully-returned line", async () => {
    // Sold 2.000, already returned 0.500 — 1.500 left, in 0.001 steps (the
    // `NUMERIC(12,3)` scale, hard rule 3). A second line already returned
    // in full (sold 3, returned 3.000) must not be returnable at all.
    renderPage(
      saleFixture({
        items: [
          {
            ...saleFixture().items[0],
            qty: "2.000",
            returnedQty: "0.500",
          } as Sale["items"][number],
          {
            ...saleFixture().items[0],
            id: "item2",
            productName: "Trousers",
            qty: "3",
            returnedQty: "3.000",
          } as Sale["items"][number],
        ],
      }),
      ["sales.void"],
    );

    fireEvent.click(await screen.findByRole("button", { name: "Return" }));

    const [returnableInput, exhaustedInput] = (await screen.findAllByRole(
      "spinbutton",
    )) as HTMLInputElement[];
    expect(returnableInput).toBeTruthy();
    expect(exhaustedInput).toBeTruthy();

    expect(returnableInput?.getAttribute("aria-valuemax")).toBe("1.5");
    expect(returnableInput?.getAttribute("step")).toBe("0.001");
    expect(returnableInput?.disabled).toBe(false);

    expect(exhaustedInput?.getAttribute("aria-valuemax")).toBe("0");
    expect(exhaustedInput?.disabled).toBe(true);
  });

  it("posts a fractional return qty exactly as the drawer shows it", async () => {
    // A small cap (0.005, still the 0.001 scale) reachable in a handful of
    // clicks — proves the posted string matches the displayed decimal
    // exactly (`"0.005"`, not `"0.005000"` or a float-rounded value) rather
    // than reaching the same cap size as the arithmetic test above, which
    // would take 1500 clicks for no extra assurance.
    renderPage(
      saleFixture({
        items: [
          {
            ...saleFixture().items[0],
            qty: "1.005",
            returnedQty: "1.000",
          } as Sale["items"][number],
        ],
      }),
      ["sales.void"],
    );

    fireEvent.click(await screen.findByRole("button", { name: "Return" }));

    const increase = (await screen.findAllByLabelText("Increase Value"))[0] as HTMLElement;
    const qtyInput = (await screen.findAllByRole("spinbutton"))[0] as HTMLInputElement;
    expect(qtyInput.getAttribute("aria-valuemax")).toBe("0.005");

    for (let i = 0; i < 5; i += 1) {
      fireEvent.mouseDown(increase);
      fireEvent.mouseUp(increase);
    }
    await waitFor(() => expect(qtyInput.value).toBe("0.005"));

    // A sixth increase must stay at the cap.
    fireEvent.mouseDown(increase);
    fireEvent.mouseUp(increase);
    await waitFor(() => expect(qtyInput.value).toBe("0.005"));

    mockedApi.POST.mockResolvedValueOnce(
      apiResult({ ...saleFixture(), id: "return1", kind: "return", originalSaleId: "sale1" }, 201),
    );

    fireEvent.click(screen.getByRole("button", { name: "Submit return" }));

    await waitFor(() => {
      expect(mockedApi.POST).toHaveBeenCalledWith(
        "/sales/{id}/return",
        expect.objectContaining({
          body: { items: [{ saleItemId: "item1", qty: "0.005" }] },
        }),
      );
    });
  });

  it("shows the mapped message on RETURN_EXCEEDS_SOLD", async () => {
    renderPage(saleFixture(), ["sales.void"]);

    fireEvent.click(await screen.findByRole("button", { name: "Return" }));

    const increase = (await screen.findAllByLabelText("Increase Value"))[0] as HTMLElement;
    fireEvent.mouseDown(increase);
    fireEvent.mouseUp(increase);

    mockedApi.POST.mockResolvedValueOnce(apiError("RETURN_EXCEEDS_SOLD", { saleItemId: "item1" }));

    fireEvent.click(await screen.findByRole("button", { name: "Submit return" }));

    expect(
      await screen.findByText("The return quantity exceeds what is left to return."),
    ).toBeTruthy();
  });

  it("shows the mapped message on SALE_NOT_RETURNABLE", async () => {
    // Unreachable through the normal UI — the return button is hidden for a
    // return-kind sale — but the server remains the enforcement point
    // (ADR-010), so a stale page or direct navigation still gets a specific
    // message rather than the generic fallback (D-70).
    renderPage(saleFixture(), ["sales.void"]);

    fireEvent.click(await screen.findByRole("button", { name: "Return" }));

    const increase = (await screen.findAllByLabelText("Increase Value"))[0] as HTMLElement;
    fireEvent.mouseDown(increase);
    fireEvent.mouseUp(increase);

    mockedApi.POST.mockResolvedValueOnce(apiError("SALE_NOT_RETURNABLE"));

    fireEvent.click(await screen.findByRole("button", { name: "Submit return" }));

    expect(await screen.findByText("This is a return and cannot itself be returned.")).toBeTruthy();
  });
});
