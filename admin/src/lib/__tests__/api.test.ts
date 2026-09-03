import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

/**
 * `lib/api.ts` builds its client with `createClient(...)`, which binds to
 * whatever `fetch` is global at that moment — so each test stubs
 * `globalThis.fetch` and then re-imports the module fresh (`resetModules`)
 * to rebind it, rather than mocking `../api` itself. `VITE_API_BASE` is
 * also stubbed to an absolute URL: the real default (`/api`, resolved by
 * the browser against the page origin) is relative, which the WHATWG
 * `Request` constructor in this Node-based test environment can't parse
 * on its own.
 */
function jsonResponse() {
  return new Response(JSON.stringify({ status: "ok" }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

describe("api middleware", () => {
  const fetchMock = vi.fn(async (_request: Request) => jsonResponse());

  beforeEach(() => {
    vi.resetModules();
    fetchMock.mockClear();
    vi.stubGlobal("fetch", fetchMock);
    vi.stubEnv("VITE_API_BASE", "http://localhost/api");
    localStorage.clear();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.unstubAllEnvs();
  });

  it("sends X-Requested-With: savdo and Accept-Language from the stored locale", async () => {
    localStorage.setItem("savdo.locale", "ru");
    const { api } = await import("../api");

    await api.GET("/healthz");

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const request = fetchMock.mock.calls[0]?.[0] as Request;
    expect(request.headers.get("X-Requested-With")).toBe("savdo");
    expect(request.headers.get("Accept-Language")).toBe("ru");
  });

  it("falls back to the default locale (uz) when nothing is stored", async () => {
    const { api } = await import("../api");

    await api.GET("/healthz");

    const request = fetchMock.mock.calls[0]?.[0] as Request;
    expect(request.headers.get("Accept-Language")).toBe("uz");
  });
});
