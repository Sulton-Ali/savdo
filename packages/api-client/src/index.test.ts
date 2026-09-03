import { describe, expect, it, vi } from "vitest";

import { createClient } from "./index.js";

describe("createClient", () => {
  it("GET /healthz decodes the typed response body", async () => {
    const fetchMock = vi.fn(
      async (_request: Request) =>
        new Response(JSON.stringify({ status: "ok" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    );

    const client = createClient("http://localhost:8080/v1", { fetch: fetchMock });

    const { data, error } = await client.GET("/healthz");

    expect(error).toBeUndefined();
    expect(data).toEqual({ status: "ok" });

    // Compile-time check: `data?.status` is the literal "ok", not a bare
    // `string` — a hand-widened type here would be a review finding
    // (AGENTS.md hard rule 6, ADR-002).
    const status: "ok" | undefined = data?.status;
    expect(status).toBe("ok");

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const requestArg = fetchMock.mock.calls[0]?.[0];
    expect(requestArg).toBeInstanceOf(Request);
    expect(requestArg?.url).toBe("http://localhost:8080/v1/healthz");
  });
});
