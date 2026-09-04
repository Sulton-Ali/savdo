import type { components } from "@savdo/api-client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { App as AntApp, ConfigProvider } from "antd";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../lib/api", () => ({
  api: { GET: vi.fn(), POST: vi.fn(), PATCH: vi.fn() },
}));

import { AuthProvider } from "../../../auth/AuthContext";
import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { AttributesPage } from "../AttributesPage";

type Me = components["schemas"]["Me"];

const mockedApi = vi.mocked(api, { deep: true });

function buildMe(): Me {
  return {
    user: {
      id: "u1",
      username: "manager",
      fullName: "Test Manager",
      phone: null,
      role: "manager",
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
      defaultLocale: "uz",
      allowNegativeStock: false,
      updateCostOnPurchase: true,
    },
    permissions: ["catalog.write"],
  };
}

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
          <AuthProvider me={buildMe()}>
            <AttributesPage />
          </AuthProvider>
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
    // The shop's default locale is "uz" (see `buildMe`), which is also the
    // first — and so, by default, active — locale tab; that's the one
    // whose "Name" field is mounted and visible without switching tabs.
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

  // T6a review MAJOR 1: the edit Drawer must remount per attribute and read
  // its starting values from `initialValues`, never `setFieldsValue` after
  // mount — the latter marks already-registered fields "touched", which
  // would make the PATCH resend every locale instead of only the one the
  // user actually opened and changed.
  it("patches only the ru locale when only its name is edited", async () => {
    mockedApi.GET.mockResolvedValue({
      data: {
        items: [
          {
            id: "attr1",
            code: "material",
            sortOrder: 0,
            name: "Material",
            locale: "en",
            translationFallback: false,
            translations: {
              uz: { name: "Material UZ" },
              en: { name: "Material EN" },
            },
          },
        ],
      },
      error: undefined,
      response: new Response(null, { status: 200 }),
    } as never);
    mockedApi.PATCH.mockResolvedValueOnce({
      data: {
        id: "attr1",
        code: "material",
        sortOrder: 0,
        name: "Material",
        locale: "en",
        translationFallback: false,
      },
      error: undefined,
      response: new Response(null, { status: 200 }),
    } as never);

    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Edit attribute" }));

    fireEvent.click(await screen.findByRole("tab", { name: "Русский" }));
    // Both locale tabs mounted so far ("uz" from the initial active tab,
    // "ru" just clicked) stay in the DOM (AntD Tabs doesn't destroy a
    // visited pane), so scope to the active tabpanel to reach the right
    // "Name" input.
    const activePanel = await screen.findByRole("tabpanel");
    fireEvent.change(within(activePanel).getByLabelText("Name"), {
      target: { value: "Материал" },
    });

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mockedApi.PATCH).toHaveBeenCalledWith("/attribute-definitions/{id}", {
        params: { path: { id: "attr1" } },
        body: {
          sortOrder: 0,
          translations: { ru: { name: "Материал" } },
        },
      });
    });
  });
});
