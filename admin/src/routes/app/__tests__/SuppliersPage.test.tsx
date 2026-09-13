import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App as AntApp, ConfigProvider } from "antd";
import { useState } from "react";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../lib/api", () => ({
  api: { GET: vi.fn(), POST: vi.fn(), PATCH: vi.fn(), DELETE: vi.fn() },
}));

import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { SuppliersPage } from "../SuppliersPage";
import type { SuppliersSearch } from "../suppliersRoute";

const mockedApi = vi.mocked(api, { deep: true });

function emptySuppliersList() {
  return {
    data: { items: [], nextCursor: null },
    error: undefined,
    response: new Response(null, { status: 200 }),
  } as never;
}

/** Mirrors how `suppliersRoute`'s wrapper drives `SuppliersPage`
 * (`search`/`onSearchChange`), except the search state lives in this test
 * harness instead of the router — `onSearchChangeSpy` observes every call
 * the page makes while `useState` keeps the page controlled, same as a real
 * `navigate({ search })` round-trip would (mirrors
 * `StockMovementsPage.test.tsx`'s harness). */
function renderPage(initialSearch: SuppliersSearch = {}) {
  const onSearchChangeSpy = vi.fn<(next: SuppliersSearch) => void>();
  // Lets a test simulate a change that does not go through the page's own
  // `onSearchChange` (browser Back/Forward, another navigation) — a real
  // `setSearch` call from the harness, not the spied round-trip.
  let setExternalSearchImpl: (next: SuppliersSearch) => void = () => {};

  function Harness() {
    const [search, setSearch] = useState<SuppliersSearch>(initialSearch);
    setExternalSearchImpl = setSearch;
    return (
      <SuppliersPage
        search={search}
        onSearchChange={(next) => {
          onSearchChangeSpy(next);
          setSearch(next);
        }}
      />
    );
  }

  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const utils = render(
    // Motion disabled (D-25 default theme token) so Modal/Drawer close
    // synchronously in jsdom instead of waiting on a real `transitionend`.
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <Harness />
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
  return {
    ...utils,
    onSearchChangeSpy,
    setExternalSearch: (next: SuppliersSearch) => act(() => setExternalSearchImpl(next)),
  };
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

  it("debounces search and fires a request with q only at 2+ characters", async () => {
    renderPage();
    await waitFor(() => expect(mockedApi.GET).toHaveBeenCalled());

    const searchInput = screen.getByPlaceholderText("Search suppliers");

    fireEvent.change(searchInput, { target: { value: "a" } });
    await new Promise((resolve) => setTimeout(resolve, 400));
    expect(
      mockedApi.GET.mock.calls.some(
        (call) =>
          call[0] === "/suppliers" &&
          (call[1] as never as { params: { query: { q?: string } } })?.params?.query?.q === "a",
      ),
    ).toBe(false);

    fireEvent.change(searchInput, { target: { value: "ac" } });
    await waitFor(
      () => {
        const matched = mockedApi.GET.mock.calls.some(
          (call) =>
            call[0] === "/suppliers" &&
            (call[1] as never as { params: { query: { q?: string } } })?.params?.query?.q === "ac",
        );
        expect(matched).toBe(true);
      },
      { timeout: 2000 },
    );
  });

  it("pushes the debounced query into the route's search params (replace, not push)", async () => {
    const { onSearchChangeSpy } = renderPage();
    await waitFor(() => expect(mockedApi.GET).toHaveBeenCalled());

    fireEvent.change(screen.getByPlaceholderText("Search suppliers"), {
      target: { value: "ac" },
    });

    await waitFor(
      () => {
        expect(onSearchChangeSpy).toHaveBeenCalledWith({ q: "ac" });
      },
      { timeout: 2000 },
    );
  });

  it("pre-fills the input and queries with it when the route search already has ?q=", async () => {
    renderPage({ q: "acme" });

    expect((screen.getByPlaceholderText("Search suppliers") as HTMLInputElement).value).toBe(
      "acme",
    );
    await waitFor(() => {
      const matched = mockedApi.GET.mock.calls.some(
        (call) =>
          call[0] === "/suppliers" &&
          (call[1] as never as { params: { query: { q?: string } } })?.params?.query?.q === "acme",
      );
      expect(matched).toBe(true);
    });
  });

  it("shows a below-minimum URL q in the input but never queries with it", async () => {
    renderPage({ q: "a" });

    expect((screen.getByPlaceholderText("Search suppliers") as HTMLInputElement).value).toBe("a");
    await waitFor(() => expect(mockedApi.GET).toHaveBeenCalled());
    expect(
      mockedApi.GET.mock.calls.some(
        (call) =>
          call[0] === "/suppliers" &&
          (call[1] as never as { params: { query: { q?: string } } })?.params?.query?.q !==
            undefined,
      ),
    ).toBe(false);
  });

  it("syncs the input from an external search change when nothing is being typed", async () => {
    const { setExternalSearch } = renderPage();
    await waitFor(() => expect(mockedApi.GET).toHaveBeenCalled());

    setExternalSearch({ q: "external" });

    expect((screen.getByPlaceholderText("Search suppliers") as HTMLInputElement).value).toBe(
      "external",
    );
    await waitFor(() => {
      const matched = mockedApi.GET.mock.calls.some(
        (call) =>
          call[0] === "/suppliers" &&
          (call[1] as never as { params: { query: { q?: string } } })?.params?.query?.q ===
            "external",
      );
      expect(matched).toBe(true);
    });
  });

  it("does not let an external search change clobber in-flight typing", async () => {
    vi.useFakeTimers();
    try {
      const { onSearchChangeSpy, setExternalSearch } = renderPage();
      await act(() => vi.advanceTimersByTimeAsync(0));

      fireEvent.change(screen.getByPlaceholderText("Search suppliers"), {
        target: { value: "ac" },
      });

      // Debounce has not elapsed yet — an external change (Back/Forward,
      // another navigation) lands while the user is still typing.
      setExternalSearch({ q: "old" });
      expect((screen.getByPlaceholderText("Search suppliers") as HTMLInputElement).value).toBe(
        "ac",
      );

      await act(() => vi.advanceTimersByTimeAsync(400));

      expect((screen.getByPlaceholderText("Search suppliers") as HTMLInputElement).value).toBe(
        "ac",
      );
      expect(onSearchChangeSpy).toHaveBeenCalledWith({ q: "ac" });
    } finally {
      vi.useRealTimers();
    }
  });

  it("Reset clears both the search input and the route's q", async () => {
    const { onSearchChangeSpy } = renderPage({ q: "acme" });
    expect((screen.getByPlaceholderText("Search suppliers") as HTMLInputElement).value).toBe(
      "acme",
    );

    fireEvent.click(screen.getByRole("button", { name: "Reset" }));

    expect(onSearchChangeSpy).toHaveBeenCalledWith({ q: undefined });
    expect((screen.getByPlaceholderText("Search suppliers") as HTMLInputElement).value).toBe("");
  });

  it("shows the result count line", async () => {
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

    renderPage();

    expect(await screen.findByText("1 result")).toBeTruthy();
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
