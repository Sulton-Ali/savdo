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

import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { DraftPayModal } from "../sale-draft-detail/DraftPayModal";

type SaleDraft = components["schemas"]["SaleDraft"];
type Sale = components["schemas"]["Sale"];

const mockedApi = vi.mocked(api, { deep: true });

function draftFixture(overrides: Partial<SaleDraft> = {}): SaleDraft {
  return {
    id: "d1",
    locationId: "l1",
    customerId: null,
    customerName: null,
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

function saleFixture(overrides: Partial<Sale> = {}): Sale {
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
    subtotal: "50000.00",
    discountAmount: "0.00",
    discountReason: null,
    total: "50000.00",
    note: null,
    completedAt: "2026-09-06T10:05:00Z",
    voidedAt: null,
    voidedBy: null,
    voidReason: null,
    payment: { method: "cash", amount: "50000.00" },
    items: [],
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

function postCallOf(callIndex: number) {
  const call = mockedApi.POST.mock.calls[callIndex];
  if (!call) {
    throw new Error(`expected a POST call at index ${callIndex}`);
  }
  return call[1] as unknown as {
    params: { path: { id: string }; header: { "Idempotency-Key": string } };
    body: { paymentMethod: string };
  };
}

function idempotencyKeyOf(callIndex: number): string {
  return postCallOf(callIndex).params.header["Idempotency-Key"];
}

function renderModal(draft: SaleDraft = draftFixture(), onClose = vi.fn()) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <DraftPayModal draft={draft} open onClose={onClose} />
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

describe("DraftPayModal", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    navigateMock.mockReset();
    mockedApi.POST.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("posts paymentMethod cash by default with a fresh Idempotency-Key, and keeps it stable across a network-error retry", async () => {
    renderModal();

    mockedApi.POST.mockResolvedValueOnce(apiError("INTERNAL", {}, 500));
    fireEvent.click(await screen.findByRole("button", { name: "Complete sale" }));

    await waitFor(() => expect(mockedApi.POST).toHaveBeenCalledTimes(1));
    expect(postCallOf(0).body).toEqual({ paymentMethod: "cash" });
    expect(postCallOf(0).params.path).toEqual({ id: "d1" });
    const firstKey = idempotencyKeyOf(0);
    expect(firstKey).toBeTruthy();

    // Retry the exact same submission (no field changed) — same key.
    mockedApi.POST.mockResolvedValueOnce(apiResult(saleFixture(), 201));
    fireEvent.click(screen.getByRole("button", { name: "Complete sale" }));

    await waitFor(() => expect(mockedApi.POST).toHaveBeenCalledTimes(2));
    expect(idempotencyKeyOf(1)).toBe(firstKey);
  });

  it("issues a new Idempotency-Key when the payment method changes", async () => {
    renderModal();

    mockedApi.POST.mockResolvedValueOnce(apiError("INTERNAL", {}, 500));
    fireEvent.click(await screen.findByRole("button", { name: "Complete sale" }));
    await waitFor(() => expect(mockedApi.POST).toHaveBeenCalledTimes(1));
    const firstKey = idempotencyKeyOf(0);

    fireEvent.mouseDown(screen.getByLabelText("Payment method"));
    fireEvent.click(await screen.findByText("Card"));

    mockedApi.POST.mockResolvedValueOnce(apiResult(saleFixture(), 201));
    fireEvent.click(screen.getByRole("button", { name: "Complete sale" }));

    await waitFor(() => expect(mockedApi.POST).toHaveBeenCalledTimes(2));
    expect(postCallOf(1).body).toEqual({ paymentMethod: "card" });
    expect(idempotencyKeyOf(1)).not.toBe(firstKey);
  });

  it("issues a new Idempotency-Key after IDEMPOTENCY_KEY_REUSED", async () => {
    renderModal();

    mockedApi.POST.mockResolvedValueOnce(apiError("IDEMPOTENCY_KEY_REUSED", {}));
    fireEvent.click(await screen.findByRole("button", { name: "Complete sale" }));

    expect(
      await screen.findByText("This sale may already be submitted. Please check and try again."),
    ).toBeTruthy();
    const firstKey = idempotencyKeyOf(0);

    mockedApi.POST.mockResolvedValueOnce(apiResult(saleFixture(), 201));
    fireEvent.click(screen.getByRole("button", { name: "Complete sale" }));

    await waitFor(() => expect(mockedApi.POST).toHaveBeenCalledTimes(2));
    expect(idempotencyKeyOf(1)).not.toBe(firstKey);
  });

  it("navigates to the created sale on success", async () => {
    const onClose = vi.fn();
    renderModal(draftFixture(), onClose);

    mockedApi.POST.mockResolvedValueOnce(apiResult(saleFixture({ id: "sale-99" }), 201));
    fireEvent.click(await screen.findByRole("button", { name: "Complete sale" }));

    await waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith({ to: "/sales/$id", params: { id: "sale-99" } });
    });
    expect(onClose).toHaveBeenCalled();
  });
});
