import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App as AntApp, ConfigProvider } from "antd";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../lib/api", () => ({
  api: { GET: vi.fn(), POST: vi.fn() },
}));

import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { QuickSalePage } from "../QuickSalePage";

const mockedApi = vi.mocked(api, { deep: true });

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

const location = { id: "loc1", name: "Main store", kind: "store", isDefault: true, isActive: true };

const product = {
  id: "prod1",
  categoryId: null,
  slug: "shirt",
  sku: "SKU1",
  unitId: "unit1",
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
};

const variant = {
  id: "var1",
  sku: "SKU1-M",
  barcode: null,
  attributes: { size: "M" },
  priceOverride: null,
  isActive: true,
};

function completedSale() {
  return {
    id: "sale1",
    number: 42,
    kind: "sale",
    status: "completed",
    locationId: "loc1",
    locationName: "Main store",
    customerId: null,
    customerName: null,
    cashierId: "u1",
    cashierName: "Test Cashier",
    originalSaleId: null,
    subtotal: "20000.00",
    discountAmount: "0.00",
    discountReason: null,
    total: "20000.00",
    note: null,
    completedAt: "2026-06-15T12:00:00Z",
    voidedAt: null,
    voidedBy: null,
    voidReason: null,
    payment: { method: "cash", amount: "20000.00" },
    items: [
      {
        id: "item1",
        variantId: "var1",
        productId: "prod1",
        productName: "Shirt",
        variantLabel: "SKU1-M — M",
        qty: "2",
        unitPrice: "10000.00",
        lineTotal: "20000.00",
        returnedQty: "0",
      },
    ],
    hasReturns: false,
  };
}

const me = {
  user: {
    id: "u1",
    username: "cashier",
    fullName: "Test Cashier",
    phone: null,
    role: "cashier",
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
  permissions: ["cashier.sell"],
};

function mockCommonEndpoints() {
  mockedApi.GET.mockImplementation(((
    path: string,
    options?: { params?: { query?: Record<string, unknown> } },
  ) => {
    if (path === "/auth/me") {
      return Promise.resolve(apiResult(me));
    }
    if (path === "/locations") {
      return Promise.resolve(apiResult({ items: [location], nextCursor: null }));
    }
    if (path === "/products") {
      const q = options?.params?.query?.q;
      if (q === "Shirt") {
        return Promise.resolve(apiResult({ items: [product], nextCursor: null }));
      }
      return Promise.resolve(apiResult({ items: [], nextCursor: null }));
    }
    if (path === "/products/{id}/variants") {
      return Promise.resolve(apiResult({ items: [variant] }));
    }
    if (path === "/customers") {
      return Promise.resolve(apiResult({ items: [], nextCursor: null }));
    }
    return Promise.resolve(apiResult({ items: [], nextCursor: null }));
  }) as never);
}

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <QuickSalePage />
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

/** Clicks a real AntD dropdown option by its visible label — AntD renders
 * both the clickable `.ant-select-item-option` and a visually-hidden
 * `role="option"` mirror used only for `aria-activedescendant` (whose text
 * content is the option's *value*, not its label), so neither `getByRole`
 * nor a plain `findByText` reliably picks the right, clickable element:
 * `findByText` also matches the cart table once a line with the same
 * label (e.g. re-adding the same variant) is already in the cart. Scoping
 * to the option class side-steps both. */
async function clickDropdownOption(label: string) {
  const target = await waitFor(() => {
    const options = Array.from(
      document.querySelectorAll<HTMLElement>(".ant-select-item-option"),
    ).filter((el) => el.textContent === label);
    const last = options.at(-1);
    if (!last) {
      throw new Error(`expected a dropdown option "${label}"`);
    }
    return last;
  });
  fireEvent.click(target);
}

async function addShirtToCart(qty = "2") {
  const productSearch = screen.getByRole("combobox", { name: "Search by product name or SKU" });
  fireEvent.mouseDown(productSearch);
  fireEvent.change(productSearch, { target: { value: "Shirt" } });

  await waitFor(() => {
    expect(
      mockedApi.GET.mock.calls.some(
        (call) =>
          call[0] === "/products" &&
          (call[1] as never as { params: { query: { q?: string } } })?.params?.query?.q === "Shirt",
      ),
    ).toBe(true);
  });

  await clickDropdownOption("Shirt (SKU1)");

  fireEvent.mouseDown(await screen.findByRole("combobox", { name: "Select variant" }));
  await clickDropdownOption("SKU1-M — M");

  fireEvent.change(screen.getByLabelText("Qty"), { target: { value: qty } });
  fireEvent.click(screen.getByRole("button", { name: "Add" }));

  await screen.findByText("Shirt");
}

/** Selects a discount type and waits for the discount value field (disabled
 * while no type is picked) to become enabled before the caller types into
 * it. */
async function selectDiscountType(label: string) {
  fireEvent.mouseDown(screen.getByLabelText("Discount"));
  fireEvent.click(await screen.findByText(label));
  await waitFor(() => {
    expect((screen.getByLabelText("Discount value") as HTMLInputElement).disabled).toBe(false);
  });
}

/** Clicks the discount type `Select`'s clear ("x") button — AntD renders it
 * as a real `<button aria-label="Clear">` inside the `.ant-select` wrapper;
 * `mousedown` only guards focus/dropdown side effects, the actual clear
 * runs on `click`. */
function clearDiscountType() {
  const discountSelect = screen.getByLabelText("Discount");
  const clearButton = discountSelect
    .closest(".ant-select")
    ?.querySelector('button[aria-label="Clear"]');
  if (!clearButton) {
    throw new Error("expected a discount type clear button");
  }
  fireEvent.mouseDown(clearButton);
  fireEvent.click(clearButton);
}

function postCallOf(callIndex: number) {
  const call = mockedApi.POST.mock.calls[callIndex];
  if (!call) {
    throw new Error(`expected a POST call at index ${callIndex}`);
  }
  return call[1] as unknown as {
    params: { header: { "Idempotency-Key": string } };
    body: Record<string, unknown>;
  };
}

function idempotencyKeyOf(callIndex: number): string {
  return postCallOf(callIndex).params.header["Idempotency-Key"];
}

describe("QuickSalePage", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    mockedApi.GET.mockReset();
    mockedApi.POST.mockReset();
    localStorage.clear();
  });

  afterEach(() => {
    cleanup();
  });

  it("adds an item to the cart, previews the subtotal, then lets removing it and clearing the cart", async () => {
    mockCommonEndpoints();
    renderPage();

    await addShirtToCart("2");

    expect(screen.getByText("10,000")).toBeTruthy(); // unit price
    expect(screen.getByText("Subtotal: 20,000")).toBeTruthy();
    expect(screen.getByText("Total: 20,000")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Clear cart" }));

    expect(
      await screen.findByText("No items yet — search for a product above to add one."),
    ).toBeTruthy();
    expect(screen.getByText("Add at least one item before completing the sale.")).toBeTruthy();
  });

  it("removes a single cart line", async () => {
    mockCommonEndpoints();
    renderPage();

    await addShirtToCart("2");

    fireEvent.click(screen.getByRole("button", { name: "Remove" }));

    expect(
      await screen.findByText("No items yet — search for a product above to add one."),
    ).toBeTruthy();
  });

  it("previews a percent discount, then a fixed discount, against the cart subtotal", async () => {
    mockCommonEndpoints();
    renderPage();

    await addShirtToCart("2"); // subtotal 20,000.00

    await selectDiscountType("Percent");
    fireEvent.change(screen.getByLabelText("Discount value"), { target: { value: "10" } });

    await waitFor(() => {
      expect(screen.getByText("Discount: 2,000")).toBeTruthy();
    });
    expect(screen.getByText("Total: 18,000")).toBeTruthy();

    await selectDiscountType("Fixed amount");
    fireEvent.change(screen.getByLabelText("Discount value"), { target: { value: "5000" } });

    await waitFor(() => {
      expect(screen.getByText("Discount: 5,000")).toBeTruthy();
    });
    expect(screen.getByText("Total: 15,000")).toBeTruthy();
  });

  it("posts a SaleCreate with variantId/qty strings and no price fields, keeps the Idempotency-Key stable across a retry, and issues a new one after success", async () => {
    mockCommonEndpoints();
    renderPage();

    fireEvent.mouseDown(await screen.findByLabelText("Location"));
    fireEvent.click(await screen.findByText("Main store"));

    await addShirtToCart("2");

    mockedApi.POST.mockResolvedValueOnce(apiError("INTERNAL", {}, 500));
    mockedApi.POST.mockResolvedValueOnce(apiResult(completedSale(), 201));

    fireEvent.click(screen.getByRole("button", { name: "Complete sale" }));

    await waitFor(() => expect(mockedApi.POST).toHaveBeenCalledTimes(1));
    expect(mockedApi.POST.mock.calls[0]?.[0]).toBe("/sales");
    expect(postCallOf(0)).toMatchObject({
      body: {
        locationId: "loc1",
        items: [{ variantId: "var1", qty: "2" }],
        payment: { method: "cash" },
      },
    });
    const firstCallBody = postCallOf(0).body;
    expect(firstCallBody.discount).toBeUndefined();
    expect(firstCallBody.customerId).toBeUndefined();
    expect(firstCallBody.items).toEqual([{ variantId: "var1", qty: "2" }]);
    const firstKey = idempotencyKeyOf(0);
    expect(firstKey).toBeTruthy();

    // Retry the same cart — same key.
    fireEvent.click(screen.getByRole("button", { name: "Complete sale" }));
    await waitFor(() => expect(mockedApi.POST).toHaveBeenCalledTimes(2));
    expect(idempotencyKeyOf(1)).toBe(firstKey);

    // Now the sale completed — start a new sale and submit again. The
    // location is remembered, not reset by a successful sale, so there is
    // no need to re-pick it.
    await screen.findByText("Sale completed");
    fireEvent.click(screen.getByRole("button", { name: "New sale" }));

    await addShirtToCart("1");

    mockedApi.POST.mockResolvedValueOnce(apiResult({ ...completedSale(), number: 43 }, 201));
    fireEvent.click(screen.getByRole("button", { name: "Complete sale" }));

    await waitFor(() => expect(mockedApi.POST).toHaveBeenCalledTimes(3));
    const thirdKey = idempotencyKeyOf(2);
    expect(thirdKey).not.toBe(firstKey);
  });

  it("sends discount and customerId only when set", async () => {
    mockCommonEndpoints();
    renderPage();

    fireEvent.mouseDown(await screen.findByLabelText("Location"));
    fireEvent.click(await screen.findByText("Main store"));

    await addShirtToCart("2");

    await selectDiscountType("Fixed amount");
    fireEvent.change(screen.getByLabelText("Discount value"), { target: { value: "1000" } });

    mockedApi.POST.mockResolvedValueOnce(apiResult(completedSale(), 201));
    fireEvent.click(screen.getByRole("button", { name: "Complete sale" }));

    await waitFor(() => expect(mockedApi.POST).toHaveBeenCalledTimes(1));
    const body = postCallOf(0).body;
    expect(body.discount).toEqual({ type: "fixed", value: "1000.00" });
  });

  it("shows a STOCK_INSUFFICIENT message on the matching line's qty control", async () => {
    mockCommonEndpoints();
    renderPage();

    fireEvent.mouseDown(await screen.findByLabelText("Location"));
    fireEvent.click(await screen.findByText("Main store"));

    await addShirtToCart("5");

    mockedApi.POST.mockResolvedValueOnce(
      apiError("STOCK_INSUFFICIENT", { variantId: "var1", available: "2.000" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Complete sale" }));

    expect(await screen.findByText("Not enough stock: only 2.000 available.")).toBeTruthy();
  });

  it("shows the sale number and total on success", async () => {
    mockCommonEndpoints();
    renderPage();

    fireEvent.mouseDown(await screen.findByLabelText("Location"));
    fireEvent.click(await screen.findByText("Main store"));

    await addShirtToCart("2");

    mockedApi.POST.mockResolvedValueOnce(apiResult(completedSale(), 201));
    fireEvent.click(screen.getByRole("button", { name: "Complete sale" }));

    expect(await screen.findByText("Sale #42")).toBeTruthy();
    expect(screen.getByText("Total: 20,000")).toBeTruthy();
  });

  it("remembers the last picked location for next time", async () => {
    mockCommonEndpoints();
    renderPage();

    fireEvent.mouseDown(await screen.findByLabelText("Location"));
    fireEvent.click(await screen.findByText("Main store"));

    await waitFor(() => {
      expect(localStorage.getItem("savdo.quickSale.locationId")).toBe("loc1");
    });

    cleanup();
    mockCommonEndpoints();
    renderPage();

    // No need to re-pick — the remembered location is already selected.
    expect(await screen.findByText("Main store")).toBeTruthy();
  });

  it("gates discountReason on the same condition as discount: typed then discount removed leaves neither in the payload", async () => {
    mockCommonEndpoints();
    renderPage();

    fireEvent.mouseDown(await screen.findByLabelText("Location"));
    fireEvent.click(await screen.findByText("Main store"));

    await addShirtToCart("2");

    await selectDiscountType("Fixed amount");
    fireEvent.change(screen.getByLabelText("Discount value"), { target: { value: "1000" } });
    fireEvent.change(screen.getByLabelText("Discount reason"), {
      target: { value: "Loyal customer" },
    });

    clearDiscountType();

    mockedApi.POST.mockResolvedValueOnce(apiResult(completedSale(), 201));
    fireEvent.click(screen.getByRole("button", { name: "Complete sale" }));

    await waitFor(() => expect(mockedApi.POST).toHaveBeenCalledTimes(1));
    const body = postCallOf(0).body;
    expect(body.discount).toBeUndefined();
    expect(body.discountReason).toBeUndefined();
  });

  it("issues a new Idempotency-Key after fixing a qty following STOCK_INSUFFICIENT", async () => {
    mockCommonEndpoints();
    renderPage();

    fireEvent.mouseDown(await screen.findByLabelText("Location"));
    fireEvent.click(await screen.findByText("Main store"));

    await addShirtToCart("5");

    mockedApi.POST.mockResolvedValueOnce(
      apiError("STOCK_INSUFFICIENT", { variantId: "var1", available: "2.000" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Complete sale" }));

    await screen.findByText("Not enough stock: only 2.000 available.");
    const firstKey = idempotencyKeyOf(0);

    // Fix the qty on the offending line — an unmodified retry must reuse
    // the key, but a modified one must not.
    fireEvent.change(screen.getByLabelText(/Qty: Shirt/), { target: { value: "2" } });

    mockedApi.POST.mockResolvedValueOnce(apiResult(completedSale(), 201));
    fireEvent.click(screen.getByRole("button", { name: "Complete sale" }));

    await waitFor(() => expect(mockedApi.POST).toHaveBeenCalledTimes(2));
    const secondKey = idempotencyKeyOf(1);
    expect(secondKey).not.toBe(firstKey);
  });

  it("shows a generic retry message for IDEMPOTENCY_KEY_REUSED", async () => {
    mockCommonEndpoints();
    renderPage();

    fireEvent.mouseDown(await screen.findByLabelText("Location"));
    fireEvent.click(await screen.findByText("Main store"));

    await addShirtToCart("2");

    mockedApi.POST.mockResolvedValueOnce(apiError("IDEMPOTENCY_KEY_REUSED", {}));
    fireEvent.click(screen.getByRole("button", { name: "Complete sale" }));

    expect(
      await screen.findByText("This sale may already be submitted. Please check and try again."),
    ).toBeTruthy();
  });

  it("increments qty when adding a variant already in the cart, instead of replacing it", async () => {
    mockCommonEndpoints();
    renderPage();

    await addShirtToCart("2");
    await addShirtToCart("3");

    expect((screen.getByLabelText(/Qty: Shirt/) as HTMLInputElement).value).toBe("5");
    expect(screen.getByText("Subtotal: 50,000")).toBeTruthy();
  });
});
