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
import { VariantMatrixGenerator } from "../VariantMatrixGenerator";

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
    {
      id: "a2",
      code: "color",
      sortOrder: 1,
      name: "Colour",
      locale: "en",
      translationFallback: false,
    },
  ];
}

function variant(overrides: Partial<Variant>): Variant {
  return {
    id: "v1",
    sku: null,
    barcode: null,
    attributes: {},
    priceOverride: null,
    isActive: true,
    ...overrides,
  };
}

function apiResult(data: unknown, status = 201) {
  return { data, error: undefined, response: new Response(null, { status }) } as never;
}

function renderGenerator(existingVariants: Variant[] = []) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <VariantMatrixGenerator
            productId="p1"
            attributeDefinitions={attributeDefinitions()}
            existingVariants={existingVariants}
          />
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

/** Types a tag value into a `Select mode="tags"` and commits it with Enter —
 * the standard rc-select interaction pattern for free-text tag entry. */
function addTagValue(container: HTMLElement, value: string) {
  const input = container.querySelector("input") as HTMLInputElement;
  fireEvent.change(input, { target: { value } });
  fireEvent.keyDown(input, { key: "Enter", code: "Enter" });
}

describe("VariantMatrixGenerator", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    mockedApi.POST.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("builds the Cartesian product, skips an existing combination, and posts exact VariantCreate bodies", async () => {
    mockedApi.POST.mockImplementation(((path: string, options: { body: unknown }) => {
      expect(path).toBe("/products/{id}/variants");
      return Promise.resolve(
        apiResult(variant({ id: `created-${Math.random()}`, ...(options.body as object) })),
      );
    }) as never);

    renderGenerator([variant({ id: "existing", attributes: { size: "S", color: "blue" } })]);

    fireEvent.click(screen.getByRole("button", { name: "Size" }));
    fireEvent.click(screen.getByRole("button", { name: "Colour" }));

    const sizeBlock = screen.getByTestId("matrix-attribute-size");
    const colorBlock = screen.getByTestId("matrix-attribute-color");
    addTagValue(sizeBlock, "S");
    addTagValue(sizeBlock, "M");
    addTagValue(colorBlock, "blue");

    // 2 combinations total (S/blue, M/blue); S/blue already exists — only
    // the new one should be offered for creation.
    await screen.findByText("Combinations to create");
    expect(screen.getByText("S / blue")).toBeTruthy();
    expect(screen.getByText("M / blue")).toBeTruthy();

    fireEvent.click(await screen.findByRole("button", { name: "Create 1 variants" }));

    await waitFor(() => {
      expect(mockedApi.POST).toHaveBeenCalledTimes(1);
    });
    expect(mockedApi.POST).toHaveBeenCalledWith("/products/{id}/variants", {
      params: { path: { id: "p1" } },
      body: { attributes: { size: "M", color: "blue" } },
    });
  });

  it("disables generation and shows the 'no new' hint when every combination already exists", async () => {
    renderGenerator([variant({ id: "existing", attributes: { size: "S" } })]);

    fireEvent.click(screen.getByRole("button", { name: "Size" }));
    addTagValue(screen.getByTestId("matrix-attribute-size"), "S");

    const generateButton = (await screen.findByRole("button", {
      name: "Every combination already has a variant.",
    })) as HTMLButtonElement;
    expect(generateButton.disabled).toBe(true);
    expect(mockedApi.POST).not.toHaveBeenCalled();
  });
});
