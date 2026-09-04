import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App as AntApp, ConfigProvider } from "antd";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../lib/api", () => ({
  api: { GET: vi.fn(), POST: vi.fn(), PATCH: vi.fn(), DELETE: vi.fn() },
}));

import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { SuppliersPage } from "../SuppliersPage";

const mockedApi = vi.mocked(api, { deep: true });

function emptySuppliersList() {
  return {
    data: { items: [], nextCursor: null },
    error: undefined,
    response: new Response(null, { status: 200 }),
  } as never;
}

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    // Motion disabled (D-25 default theme token) so Modal/Drawer close
    // synchronously in jsdom instead of waiting on a real `transitionend`.
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <SuppliersPage />
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

describe("SuppliersPage", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    mockedApi.GET.mockReset();
    mockedApi.POST.mockReset();
    mockedApi.PATCH.mockReset();
    mockedApi.DELETE.mockReset();
    mockedApi.GET.mockResolvedValue(emptySuppliersList());
  });

  afterEach(() => {
    cleanup();
  });

  it("renders the suppliers returned by GET /suppliers", async () => {
    mockedApi.GET.mockResolvedValue({
      data: {
        items: [
          {
            id: "sup1",
            name: "Acme Textiles",
            contactName: "Jane",
            phone: "+998901112233",
            telegramUsername: "acme",
            note: null,
          },
        ],
        nextCursor: null,
      },
      error: undefined,
      response: new Response(null, { status: 200 }),
    } as never);

    renderPage();

    expect(await screen.findByText("Acme Textiles")).toBeTruthy();
    expect(screen.getByText("Jane")).toBeTruthy();
  });

  it("posts the exact SupplierCreate payload and closes the modal on success", async () => {
    mockedApi.POST.mockResolvedValueOnce({
      data: {
        id: "sup2",
        name: "New Supplier",
        contactName: null,
        phone: null,
        telegramUsername: null,
        note: null,
      },
      error: undefined,
      response: new Response(null, { status: 201 }),
    } as never);

    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Add supplier" }));

    fireEvent.change(await screen.findByLabelText("Name"), {
      target: { value: "New Supplier" },
    });

    fireEvent.click(screen.getByRole("button", { name: "OK" }));

    await waitFor(() => {
      expect(mockedApi.POST).toHaveBeenCalledWith("/suppliers", {
        body: { name: "New Supplier" },
      });
    });

    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
  });

  it("shows a field error on name for a 409 CONFLICT with details.field", async () => {
    mockedApi.POST.mockResolvedValueOnce({
      data: undefined,
      error: { error: { code: "CONFLICT", details: { field: "name" } } },
      response: new Response(null, { status: 409 }),
    } as never);

    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Add supplier" }));

    fireEvent.change(await screen.findByLabelText("Name"), {
      target: { value: "Dup Supplier" },
    });

    fireEvent.click(screen.getByRole("button", { name: "OK" }));

    expect(await screen.findByText("This value is already in use")).toBeTruthy();
    // The modal stays open on a field-level error.
    expect(screen.getByRole("dialog")).toBeTruthy();
  });

  it("deletes a supplier after the confirm popconfirm", async () => {
    mockedApi.GET.mockResolvedValue({
      data: {
        items: [
          {
            id: "sup1",
            name: "Acme Textiles",
            contactName: null,
            phone: null,
            telegramUsername: null,
            note: null,
          },
        ],
        nextCursor: null,
      },
      error: undefined,
      response: new Response(null, { status: 200 }),
    } as never);
    mockedApi.DELETE.mockResolvedValueOnce({
      data: undefined,
      error: undefined,
      response: new Response(null, { status: 204 }),
    } as never);

    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Delete" }));
    fireEvent.click(await screen.findByRole("button", { name: "OK" }));

    await waitFor(() => {
      expect(mockedApi.DELETE).toHaveBeenCalledWith("/suppliers/{id}", {
        params: { path: { id: "sup1" } },
      });
    });
  });
});
