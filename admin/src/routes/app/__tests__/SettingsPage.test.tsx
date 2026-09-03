import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App as AntApp } from "antd";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../lib/api", () => ({
  api: { GET: vi.fn(), POST: vi.fn(), PATCH: vi.fn() },
}));

import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { SettingsPage } from "../SettingsPage";

const mockedApi = vi.mocked(api, { deep: true });

const shop = {
  id: "s1",
  slug: "test-shop",
  name: "Test Shop",
  currency: "UZS",
  timezone: "Asia/Tashkent",
  defaultLocale: "uz" as const,
  allowNegativeStock: false,
  updateCostOnPurchase: true,
};

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <AntApp>
        <SettingsPage />
      </AntApp>
    </QueryClientProvider>,
  );
}

describe("SettingsPage", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    mockedApi.GET.mockReset();
    mockedApi.POST.mockReset();
    mockedApi.PATCH.mockReset();
    mockedApi.GET.mockResolvedValue({
      data: shop,
      error: undefined,
      response: new Response(null, { status: 200 }),
    } as never);
  });

  it("posts a ShopPatch on save and shows the saved notification", async () => {
    mockedApi.PATCH.mockResolvedValueOnce({
      data: { ...shop, name: "New Shop Name" },
      error: undefined,
      response: new Response(null, { status: 200 }),
    } as never);

    renderPage();

    const nameInput = await screen.findByDisplayValue("Test Shop");
    fireEvent.change(nameInput, { target: { value: "New Shop Name" } });

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mockedApi.PATCH).toHaveBeenCalledWith("/shop", {
        body: {
          name: "New Shop Name",
          timezone: "Asia/Tashkent",
          defaultLocale: "uz",
          allowNegativeStock: false,
          updateCostOnPurchase: true,
        },
      });
    });

    expect(await screen.findByText("Settings saved")).toBeTruthy();
  });
});
