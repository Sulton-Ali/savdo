import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { App as AntApp } from "antd";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../lib/api", () => ({
  api: { GET: vi.fn(), POST: vi.fn(), PATCH: vi.fn(), PUT: vi.fn() },
}));

import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { LandingContentPage } from "../LandingContentPage";

const mockedApi = vi.mocked(api, { deep: true });

function jsonResult(data: unknown, status = 200) {
  return { data, error: undefined, response: new Response(null, { status }) } as never;
}

/** Builds a `ContentResource`-shaped object: `locales` maps each provided
 * locale to `{ data, updatedAt }` (O-19/O-21 — `GET /content/{key}`'s
 * response shape). */
function resource(key: string, locales: Partial<Record<"uz" | "ru" | "en", unknown>>) {
  const built: Record<string, unknown> = {};
  for (const [locale, data] of Object.entries(locales)) {
    built[locale] = { data, updatedAt: "2026-01-01T00:00:00Z" };
  }
  return { key, locales: built };
}

const OPEN_DAYS = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"].map((day) => ({
  day,
  closed: false,
  open: "09:00",
  close: "18:00",
}));

/** Mocks `GET /content/{key}` for every one of the six keys — every card
 * fetches on mount, so a test only interested in one block still has to
 * supply (at least an empty) response for the rest. */
function mockContentGet(resources: Record<string, unknown>) {
  mockedApi.GET.mockImplementation(((
    path: string,
    options: { params: { path: { key: string } } },
  ) => {
    if (path !== "/content/{key}") {
      throw new Error(`unexpected GET ${path}`);
    }
    const key = options.params.path.key;
    return Promise.resolve(jsonResult(resources[key] ?? { key, locales: {} }));
  }) as never);
}

/** Returns the render result plus the `QueryClient` it was rendered with, so
 * a test can call `queryClient.invalidateQueries` to simulate the kind of
 * background refetch `staleTime: 0`/`refetchOnWindowFocus` produce in the
 * real app. */
function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const view = render(
    <QueryClientProvider client={queryClient}>
      <AntApp>
        <LandingContentPage />
      </AntApp>
    </QueryClientProvider>,
  );
  return { ...view, queryClient };
}

describe("LandingContentPage", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    mockedApi.GET.mockReset();
    mockedApi.PUT.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("renders all six content blocks from the mocked GET responses", async () => {
    mockContentGet({
      hero: resource("hero", { uz: { title: "Salom" } }),
      about: resource("about", { uz: { body: "Biz haqimizda" } }),
      hours: resource("hours", { uz: { days: OPEN_DAYS } }),
      contacts: resource("contacts", { uz: { phone: "+998901234567", address: "Toshkent" } }),
      social: resource("social", {}),
      seo: resource("seo", { uz: { title: "Savdo", description: "A shop" } }),
    });

    renderPage();

    expect(await screen.findByText("Hero")).toBeTruthy();
    expect(screen.getByText("About")).toBeTruthy();
    expect(screen.getByText("Hours")).toBeTruthy();
    expect(screen.getByText("Contacts")).toBeTruthy();
    expect(screen.getByText("Social")).toBeTruthy();
    expect(screen.getByText("SEO")).toBeTruthy();
  });

  // Every test below that touches a non-active locale's pane clicks that
  // tab first — interacting with a hidden (inactive) Tabs pane's DOM
  // directly reproducibly hangs the jsdom test runner (found while writing
  // this file's cross-tab test), so always click the tab into view before
  // querying or firing events inside it.
  it("switching the locale tab shows that locale's saved data", async () => {
    mockContentGet({
      hero: resource("hero", { uz: { title: "Salom" }, ru: { title: "Privet" } }),
      about: resource("about", {}),
      hours: resource("hours", {}),
      contacts: resource("contacts", {}),
      social: resource("social", {}),
      seo: resource("seo", {}),
    });

    renderPage();

    expect(await screen.findByDisplayValue("Salom")).toBeTruthy();

    fireEvent.click(screen.getByRole("tab", { name: "Русский" }));

    expect(await screen.findByDisplayValue("Privet")).toBeTruthy();
  });

  it("saves the hero block via PUT with { locale, data }, omitting blank optionals", async () => {
    mockContentGet({
      hero: resource("hero", { uz: { title: "Salom" } }),
      about: resource("about", {}),
      hours: resource("hours", {}),
      contacts: resource("contacts", {}),
      social: resource("social", {}),
      seo: resource("seo", {}),
    });
    mockedApi.PUT.mockResolvedValueOnce(
      jsonResult({
        key: "hero",
        locale: "uz",
        data: { title: "Salom" },
        updatedAt: "2026-01-01T00:00:00Z",
      }),
    );

    renderPage();

    const titleInput = await screen.findByDisplayValue("Salom");
    const heroCard = titleInput.closest(".ant-card") as HTMLElement;
    fireEvent.click(within(heroCard).getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mockedApi.PUT).toHaveBeenCalledWith("/content/{key}", {
        params: { path: { key: "hero" } },
        body: { locale: "uz", data: { title: "Salom" } },
      });
    });
  });

  it("maps a 422 VALIDATION_FAILED field error onto the hero form", async () => {
    mockContentGet({
      hero: resource("hero", { uz: { title: "Salom" } }),
      about: resource("about", {}),
      hours: resource("hours", {}),
      contacts: resource("contacts", {}),
      social: resource("social", {}),
      seo: resource("seo", {}),
    });
    mockedApi.PUT.mockResolvedValueOnce({
      data: undefined,
      error: { error: { code: "VALIDATION_FAILED", details: { fields: { tagline: "too_long" } } } },
      response: new Response(null, { status: 422 }),
    } as never);

    renderPage();

    const titleInput = await screen.findByDisplayValue("Salom");
    const heroCard = titleInput.closest(".ant-card") as HTMLElement;
    fireEvent.click(within(heroCard).getByRole("button", { name: "Save" }));

    expect(await screen.findByText("This value is invalid")).toBeTruthy();
  });

  // Review MAJOR 1: the server names a nested `ContentHours.days` error
  // `"days[0].close"` (`api/internal/content/validate.go`); `applyApiErrorToForm`
  // must parse that into the `["days", 0, "close"]` NamePath the Monday
  // close `TimePicker` is actually registered under.
  it("maps a nested 422 field path (days[0].close) onto the Monday close TimePicker", async () => {
    mockContentGet({
      hero: resource("hero", {}),
      about: resource("about", {}),
      hours: resource("hours", { uz: { days: OPEN_DAYS } }),
      contacts: resource("contacts", {}),
      social: resource("social", {}),
      seo: resource("seo", {}),
    });
    mockedApi.PUT.mockResolvedValueOnce({
      data: undefined,
      error: {
        error: { code: "VALIDATION_FAILED", details: { fields: { "days[0].close": "invalid" } } },
      },
      response: new Response(null, { status: 422 }),
    } as never);

    renderPage();

    const mondayClose = await screen.findByLabelText("Monday Closes");
    const hoursCard = mondayClose.closest(".ant-card") as HTMLElement;
    fireEvent.click(within(hoursCard).getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(screen.getByLabelText("Monday Closes").getAttribute("aria-invalid")).toBe("true");
    });
  });

  // Review MAJOR 2: all three locale tabs share the same `["content", key]`
  // query cache, so saving one locale must not clobber an unsaved edit
  // sitting in another (still-mounted) locale's form.
  it("keeps an unsaved ru hero edit after saving the uz hero", async () => {
    mockContentGet({
      hero: resource("hero", { uz: { title: "Salom" }, ru: { title: "Privet" } }),
      about: resource("about", {}),
      hours: resource("hours", {}),
      contacts: resource("contacts", {}),
      social: resource("social", {}),
      seo: resource("seo", {}),
    });
    mockedApi.PUT.mockResolvedValueOnce(
      jsonResult({
        key: "hero",
        locale: "uz",
        data: { title: "Salom" },
        updatedAt: "2026-01-01T00:00:00Z",
      }),
    );

    renderPage();

    await screen.findByDisplayValue("Salom");

    // Mounts the ru pane (lazy on first activation) and edit its title.
    fireEvent.click(screen.getByRole("tab", { name: "Русский" }));
    const ruTitleInput = await screen.findByDisplayValue("Privet");
    fireEvent.change(ruTitleInput, { target: { value: "Privet edited" } });

    // Switch back to uz and save it there.
    fireEvent.click(screen.getByRole("tab", { name: "Oʻzbekcha" }));
    const uzTitleInput = await screen.findByDisplayValue("Salom");
    const uzHeroCard = uzTitleInput.closest(".ant-card") as HTMLElement;
    fireEvent.click(within(uzHeroCard).getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mockedApi.PUT).toHaveBeenCalledWith("/content/{key}", {
        params: { path: { key: "hero" } },
        body: { locale: "uz", data: { title: "Salom" } },
      });
    });

    // Switch to ru: the edit must still be there, untouched by the uz save.
    fireEvent.click(screen.getByRole("tab", { name: "Русский" }));
    expect(await screen.findByDisplayValue("Privet edited")).toBeTruthy();
  });

  // Re-review MAJOR: `form.isFieldsTouched()` flips true from the card's own
  // first `setFieldsValue` populate and never goes false again, so it must
  // not gate a resync — a card the user never edited still has to pick up a
  // background refetch (`staleTime: 0`/`refetchOnWindowFocus` in the real
  // app; simulated here via `queryClient.invalidateQueries`).
  it("resyncs an untouched card when its data is refetched in the background", async () => {
    mockContentGet({
      hero: resource("hero", { uz: { title: "Salom" } }),
      about: resource("about", {}),
      hours: resource("hours", {}),
      contacts: resource("contacts", {}),
      social: resource("social", {}),
      seo: resource("seo", {}),
    });

    const { queryClient } = renderPage();

    await screen.findByDisplayValue("Salom");

    mockContentGet({
      hero: resource("hero", { uz: { title: "Salom updated" } }),
      about: resource("about", {}),
      hours: resource("hours", {}),
      contacts: resource("contacts", {}),
      social: resource("social", {}),
      seo: resource("seo", {}),
    });
    await queryClient.invalidateQueries({ queryKey: ["content", "hero"] });

    expect(await screen.findByDisplayValue("Salom updated")).toBeTruthy();
  });

  it("resyncs ru from a later background refetch after ru's own save", async () => {
    mockContentGet({
      hero: resource("hero", { uz: { title: "Salom" }, ru: { title: "Privet" } }),
      about: resource("about", {}),
      hours: resource("hours", {}),
      contacts: resource("contacts", {}),
      social: resource("social", {}),
      seo: resource("seo", {}),
    });
    mockedApi.PUT.mockResolvedValueOnce(
      jsonResult({
        key: "hero",
        locale: "ru",
        data: { title: "Privet edited" },
        updatedAt: "2026-01-01T00:00:00Z",
      }),
    );

    const { queryClient } = renderPage();

    await screen.findByDisplayValue("Salom");
    fireEvent.click(screen.getByRole("tab", { name: "Русский" }));
    const ruTitleInput = await screen.findByDisplayValue("Privet");
    fireEvent.change(ruTitleInput, { target: { value: "Privet edited" } });

    const ruHeroCard = ruTitleInput.closest(".ant-card") as HTMLElement;
    fireEvent.click(within(ruHeroCard).getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(mockedApi.PUT).toHaveBeenCalledWith("/content/{key}", {
        params: { path: { key: "hero" } },
        body: { locale: "ru", data: { title: "Privet edited" } },
      });
    });
    await screen.findByDisplayValue("Privet edited");

    // The save cleared ru's edit flag, so a later refetch must still land.
    mockContentGet({
      hero: resource("hero", { uz: { title: "Salom" }, ru: { title: "Privet refetched" } }),
      about: resource("about", {}),
      hours: resource("hours", {}),
      contacts: resource("contacts", {}),
      social: resource("social", {}),
      seo: resource("seo", {}),
    });
    await queryClient.invalidateQueries({ queryKey: ["content", "hero"] });

    expect(await screen.findByDisplayValue("Privet refetched")).toBeTruthy();
  });

  it("hours form renders exactly 7 fixed weekday rows and disables open/close once closed", async () => {
    mockContentGet({
      hero: resource("hero", {}),
      about: resource("about", {}),
      hours: resource("hours", { uz: { days: OPEN_DAYS } }),
      contacts: resource("contacts", {}),
      social: resource("social", {}),
      seo: resource("seo", {}),
    });

    renderPage();

    await screen.findByText("Monday");

    for (const label of [
      "Monday",
      "Tuesday",
      "Wednesday",
      "Thursday",
      "Friday",
      "Saturday",
      "Sunday",
    ]) {
      expect(screen.getByText(label)).toBeTruthy();
    }

    const mondayOpen = (await screen.findByLabelText("Monday Opens")) as HTMLInputElement;
    const mondayClose = screen.getByLabelText("Monday Closes") as HTMLInputElement;
    expect(mondayOpen.disabled).toBe(false);
    expect(mondayClose.disabled).toBe(false);

    fireEvent.click(screen.getByRole("switch", { name: "Monday Closed" }));

    await waitFor(() => {
      expect((screen.getByLabelText("Monday Opens") as HTMLInputElement).disabled).toBe(true);
    });
    expect((screen.getByLabelText("Monday Closes") as HTMLInputElement).disabled).toBe(true);
  });
});
