import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App as AntApp, ConfigProvider } from "antd";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../lib/api", () => ({
  api: { GET: vi.fn(), POST: vi.fn(), PATCH: vi.fn(), DELETE: vi.fn() },
}));

import type { AttributeDefinition, Variant } from "../../../catalog/api";
import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { VariantsTable } from "../VariantsTable";

const mockedApi = vi.mocked(api, { deep: true });

function attributeDefinitions(): AttributeDefinition[] {
  return [
    {
      id: "a1",
      code: "size",
      sortOrder: 0,
      name: "Size",
      locale: "en",
      translationFallback: false,
    },
  ];
}

function variant(overrides: Partial<Variant>): Variant {
  return {
    id: "v1",
    sku: "SKU-1",
    barcode: null,
    attributes: { size: "M" },
    priceOverride: "100.00",
    isActive: true,
    ...overrides,
  };
}

function apiResult(data: unknown, status = 200) {
  return { data, error: undefined, response: new Response(null, { status }) } as never;
}

function renderTable(variants: Variant[]) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <VariantsTable
            productId="p1"
            variants={variants}
            attributeDefinitions={attributeDefinitions()}
            canWrite
          />
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

describe("VariantsTable", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    mockedApi.PATCH.mockReset();
    mockedApi.DELETE.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("hides the cost override column when no variant carries the key (no cost.read)", () => {
    renderTable([variant({})]);
    expect(screen.queryByText("Cost override")).toBeNull();
  });

  it("shows the cost override column when at least one variant carries the key", () => {
    renderTable([variant({ costOverride: "50.00" })]);
    expect(screen.getByText("Cost override")).toBeTruthy();
  });

  it("sends null to clear the price override via the edit drawer", async () => {
    mockedApi.PATCH.mockResolvedValueOnce(apiResult(variant({ priceOverride: null })));
    renderTable([variant({})]);

    fireEvent.click(screen.getByRole("button", { name: "Edit variant" }));

    const priceInput = await screen.findByLabelText("Price override");
    fireEvent.change(priceInput, { target: { value: "" } });

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mockedApi.PATCH).toHaveBeenCalledWith("/variants/{id}", {
        params: { path: { id: "v1" } },
        body: { priceOverride: null },
      });
    });
  });

  it("maps the 'only variant' 400 to a notification instead of a field error", async () => {
    mockedApi.DELETE.mockResolvedValueOnce({
      data: undefined,
      error: {
        error: { code: "VALIDATION_FAILED", details: { fields: { variantId: "invalid" } } },
      },
      response: new Response(null, { status: 400 }),
    } as never);
    renderTable([variant({})]);

    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    fireEvent.click(await screen.findByRole("button", { name: "OK" }));

    expect(
      await screen.findByText("A product must always keep at least one variant."),
    ).toBeTruthy();
  });
});
