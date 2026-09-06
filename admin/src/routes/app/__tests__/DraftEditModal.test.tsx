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
  api: { GET: vi.fn(), PATCH: vi.fn() },
}));

import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { DraftEditModal } from "../sale-draft-detail/DraftEditModal";

type SaleDraft = components["schemas"]["SaleDraft"];
type Customer = components["schemas"]["Customer"];

const mockedApi = vi.mocked(api, { deep: true });

function draftFixture(overrides: Partial<SaleDraft> = {}): SaleDraft {
  return {
    id: "d1",
    locationId: "l1",
    customerId: null,
    discount: null,
    discountReason: null,
    note: null,
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
    createdByName: "Test User",
    createdAt: "2026-09-06T10:00:00Z",
    updatedAt: "2026-09-06T10:00:00Z",
    ...overrides,
  };
}

function customerFixture(overrides: Partial<Customer> = {}): Customer {
  return {
    id: "c1",
    fullName: "Jane Doe",
    phone: null,
    telegramUsername: null,
    note: null,
    tags: [],
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
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

function mockGet(customer: Customer | null = null) {
  mockedApi.GET.mockImplementation(((path: string) => {
    if (path === "/customers/{id}") {
      return customer
        ? Promise.resolve(apiResult(customer))
        : Promise.reject(new Error("no customer mocked"));
    }
    if (path === "/customers") {
      return Promise.resolve(apiResult({ items: customer ? [customer] : [], nextCursor: null }));
    }
    throw new Error(`unexpected GET ${path}`);
  }) as never);
}

function patchCallOf(callIndex: number) {
  const call = mockedApi.PATCH.mock.calls[callIndex];
  if (!call) {
    throw new Error(`expected a PATCH call at index ${callIndex}`);
  }
  return call[1] as unknown as {
    params: { path: { id: string } };
    body: Record<string, unknown>;
  };
}

/** Same helper `QuickSalePage.test.tsx` uses for its discount type Select —
 * waits for "Discount value" to become enabled so a caller never types into
 * a still-disabled field. */
async function selectDiscountType(label: string) {
  fireEvent.mouseDown(screen.getByLabelText("Discount"));
  fireEvent.click(await screen.findByText(label));
  await waitFor(() => {
    expect((screen.getByLabelText("Discount value") as HTMLInputElement).disabled).toBe(false);
  });
}

/** Clicks a labelled `Select`'s clear ("x") button — same technique
 * `QuickSalePage.test.tsx`'s `clearDiscountType` uses. */
function clearSelect(label: string) {
  const select = screen.getByLabelText(label);
  const clearButton = select.closest(".ant-select")?.querySelector('button[aria-label="Clear"]');
  if (!clearButton) {
    throw new Error(`expected a clear button for "${label}"`);
  }
  fireEvent.mouseDown(clearButton);
  fireEvent.click(clearButton);
}

function renderModal(draft: SaleDraft, onUpdated = vi.fn(), onClose = vi.fn()) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <DraftEditModal draft={draft} open onClose={onClose} onUpdated={onUpdated} />
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

describe("DraftEditModal", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    mockedApi.GET.mockReset();
    mockedApi.PATCH.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("sends a patch with only the touched field (note)", async () => {
    mockGet();
    const draft = draftFixture({ note: "old note" });
    renderModal(draft);

    fireEvent.change(await screen.findByLabelText("Note"), {
      target: { value: "new note" },
    });

    mockedApi.PATCH.mockResolvedValueOnce(apiResult({ ...draft, note: "new note" }));
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    await waitFor(() => expect(mockedApi.PATCH).toHaveBeenCalledTimes(1));
    expect(patchCallOf(0)).toEqual({
      params: { path: { id: "d1" } },
      body: { note: "new note" },
    });
  });

  it("sends both discountType: null and discountValue: null when clearing an existing discount", async () => {
    mockGet();
    const draft = draftFixture({
      discount: { type: "percent", value: "10.00" },
      discountReason: "loyal customer",
    });
    renderModal(draft);

    await screen.findByLabelText("Discount");
    clearSelect("Discount");

    mockedApi.PATCH.mockResolvedValueOnce(apiResult({ ...draft, discount: null }));
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    await waitFor(() => expect(mockedApi.PATCH).toHaveBeenCalledTimes(1));
    expect(patchCallOf(0).body).toEqual({
      discountType: null,
      discountValue: null,
      discountReason: null,
    });
  });

  it("sends customerId: null when the customer is cleared", async () => {
    const customer = customerFixture();
    mockGet(customer);
    const draft = draftFixture({ customerId: "c1" });
    renderModal(draft);

    await screen.findByText("Jane Doe");
    clearSelect("Customer");

    mockedApi.PATCH.mockResolvedValueOnce(apiResult({ ...draft, customerId: null }));
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    await waitFor(() => expect(mockedApi.PATCH).toHaveBeenCalledTimes(1));
    expect(patchCallOf(0).body).toEqual({ customerId: null });
  });

  it("blocks submit with a required-value error when a discount type is picked but no value is entered", async () => {
    mockGet();
    renderModal(draftFixture());

    await selectDiscountType("Percent");
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    expect(await screen.findByText("This field is required")).toBeTruthy();
    expect(mockedApi.PATCH).not.toHaveBeenCalled();
  });

  it("sends discountReason only when touched, and clears it to null when emptied", async () => {
    mockGet();
    const draft = draftFixture({
      discount: { type: "fixed", value: "5000.00" },
      discountReason: "old reason",
    });
    renderModal(draft);

    await screen.findByLabelText("Discount");
    fireEvent.change(screen.getByLabelText("Discount reason"), { target: { value: "" } });

    mockedApi.PATCH.mockResolvedValueOnce(apiResult({ ...draft, discountReason: null }));
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    await waitFor(() => expect(mockedApi.PATCH).toHaveBeenCalledTimes(1));
    expect(patchCallOf(0).body).toEqual({ discountReason: null });
  });

  it("shows the mapped message on DISCOUNT_EXCEEDS_SUBTOTAL", async () => {
    mockGet();
    const draft = draftFixture();
    renderModal(draft);

    await selectDiscountType("Fixed amount");
    fireEvent.change(screen.getByLabelText("Discount value"), { target: { value: "999999" } });

    mockedApi.PATCH.mockResolvedValueOnce(apiError("DISCOUNT_EXCEEDS_SUBTOTAL"));
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    expect(await screen.findByText("Discount cannot exceed the subtotal.")).toBeTruthy();
  });
});
