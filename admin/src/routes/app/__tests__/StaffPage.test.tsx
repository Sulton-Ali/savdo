import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App as AntApp, ConfigProvider } from "antd";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../lib/api", () => ({
  api: { GET: vi.fn(), POST: vi.fn(), PATCH: vi.fn() },
}));

import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { StaffPage } from "../StaffPage";

const mockedApi = vi.mocked(api, { deep: true });

function emptyStaffList() {
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
          <StaffPage />
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

describe("StaffPage", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    mockedApi.GET.mockReset();
    mockedApi.POST.mockReset();
    mockedApi.PATCH.mockReset();
    mockedApi.GET.mockResolvedValue(emptyStaffList());
  });

  afterEach(() => {
    cleanup();
  });

  it("posts the exact StaffCreate payload and closes the modal on success", async () => {
    mockedApi.POST.mockResolvedValueOnce({
      data: {
        id: "u2",
        username: "newcashier",
        fullName: "New Cashier",
        phone: null,
        role: "cashier",
        locale: "uz",
        isActive: true,
        lastLoginAt: null,
        createdAt: "2026-01-01T00:00:00Z",
      },
      error: undefined,
      response: new Response(null, { status: 201 }),
    } as never);

    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Add staff" }));

    fireEvent.change(await screen.findByLabelText("Username"), {
      target: { value: "newcashier" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "hunter22pass" },
    });
    fireEvent.change(screen.getByLabelText("Full name"), {
      target: { value: "New Cashier" },
    });

    fireEvent.click(screen.getByRole("button", { name: "OK" }));

    await waitFor(() => {
      expect(mockedApi.POST).toHaveBeenCalledWith("/staff", {
        body: {
          username: "newcashier",
          password: "hunter22pass",
          fullName: "New Cashier",
          role: "cashier",
          locale: "uz",
        },
      });
    });

    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
  });

  it("shows a field error on username for a 409 CONFLICT with details.field", async () => {
    mockedApi.POST.mockResolvedValueOnce({
      data: undefined,
      error: { error: { code: "CONFLICT", details: { field: "username" } } },
      response: new Response(null, { status: 409 }),
    } as never);

    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Add staff" }));

    fireEvent.change(await screen.findByLabelText("Username"), {
      target: { value: "owner" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "hunter22pass" },
    });
    fireEvent.change(screen.getByLabelText("Full name"), {
      target: { value: "Dup Owner" },
    });

    fireEvent.click(screen.getByRole("button", { name: "OK" }));

    expect(await screen.findByText("This value is already in use")).toBeTruthy();
    // The modal stays open on a field-level error.
    expect(screen.getByRole("dialog")).toBeTruthy();
  });

  // D-35: PATCH clears a field with a real JSON `null`, not `""`. AntD's
  // Form reports a cleared Input as an empty string, so StaffPage must
  // normalize that to `null` itself before calling the API.
  it("sends phone: null, not an empty string, when the phone field is cleared", async () => {
    mockedApi.GET.mockResolvedValue({
      data: {
        items: [
          {
            id: "u1",
            username: "cashier1",
            fullName: "Cashier One",
            phone: "+998901112233",
            role: "cashier",
            locale: "uz",
            isActive: true,
            lastLoginAt: null,
            createdAt: "2026-01-01T00:00:00Z",
          },
        ],
        nextCursor: null,
      },
      error: undefined,
      response: new Response(null, { status: 200 }),
    } as never);
    mockedApi.PATCH.mockResolvedValueOnce({
      data: {
        id: "u1",
        username: "cashier1",
        fullName: "Cashier One",
        phone: null,
        role: "cashier",
        locale: "uz",
        isActive: true,
        lastLoginAt: null,
        createdAt: "2026-01-01T00:00:00Z",
      },
      error: undefined,
      response: new Response(null, { status: 200 }),
    } as never);

    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Edit staff" }));

    const phoneInput = await screen.findByLabelText("Phone");
    fireEvent.change(phoneInput, { target: { value: "" } });

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mockedApi.PATCH).toHaveBeenCalledWith("/staff/{id}", {
        params: { path: { id: "u1" } },
        body: {
          fullName: "Cashier One",
          phone: null,
          locale: "uz",
          role: "cashier",
        },
      });
    });
  });
});
