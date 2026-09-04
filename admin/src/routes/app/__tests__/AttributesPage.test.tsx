import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App as AntApp, ConfigProvider } from "antd";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../lib/api", () => ({
  api: { GET: vi.fn(), POST: vi.fn(), PATCH: vi.fn() },
}));

import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { AttributesPage } from "../AttributesPage";

const mockedApi = vi.mocked(api, { deep: true });

function emptyAttributeList() {
  return {
    data: { items: [] },
    error: undefined,
    response: new Response(null, { status: 200 }),
  } as never;
}

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <AttributesPage />
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

describe("AttributesPage", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    mockedApi.GET.mockReset();
    mockedApi.POST.mockReset();
    mockedApi.PATCH.mockReset();
    mockedApi.GET.mockResolvedValue(emptyAttributeList());
  });

  afterEach(() => {
    cleanup();
  });

  it("posts code and translations for a new attribute definition", async () => {
    mockedApi.POST.mockResolvedValueOnce({
      data: {
        id: "attr1",
        code: "material",
        sortOrder: 0,
        name: "Material",
        locale: "en",
        translationFallback: false,
      },
      error: undefined,
      response: new Response(null, { status: 201 }),
    } as never);

    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Add attribute" }));

    fireEvent.change(await screen.findByLabelText("Code"), {
      target: { value: "material" },
    });
    // The English tab's name field is rendered (forceRender) and visible by
    // default (Tabs opens on its first item).
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Material" },
    });

    fireEvent.click(screen.getByRole("button", { name: "OK" }));

    await waitFor(() => {
      expect(mockedApi.POST).toHaveBeenCalledWith("/attribute-definitions", {
        body: {
          code: "material",
          translations: { uz: { name: "Material" } },
        },
      });
    });
  });
});
