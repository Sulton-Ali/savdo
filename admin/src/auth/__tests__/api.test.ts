import { describe, expect, it, vi } from "vitest";

vi.mock("../../lib/api", () => ({
  api: {
    GET: vi.fn(),
    POST: vi.fn(),
  },
}));

import { api } from "../../lib/api";
import { ApiAuthError, fetchMe, login, logout } from "../api";

const mockedApi = vi.mocked(api, { deep: true });

describe("login", () => {
  it("sends client: web alongside the credentials", async () => {
    mockedApi.POST.mockResolvedValueOnce({
      data: { user: { id: "u1" }, session: { id: "s1" } },
      error: undefined,
      response: new Response(null, { status: 200 }),
    } as never);

    await login({ username: "alice", password: "hunter22" });

    expect(mockedApi.POST).toHaveBeenCalledWith("/auth/login", {
      body: { username: "alice", password: "hunter22", client: "web" },
    });
  });

  it("throws ApiAuthError with the UNAUTHENTICATED code on a 401 body", async () => {
    mockedApi.POST.mockResolvedValueOnce({
      data: undefined,
      error: { error: { code: "UNAUTHENTICATED" } },
      response: new Response(null, { status: 401 }),
    } as never);

    await expect(login({ username: "alice", password: "wrong" })).rejects.toMatchObject({
      code: "UNAUTHENTICATED",
    });
  });

  it("captures Retry-After seconds on a 429 body", async () => {
    mockedApi.POST.mockResolvedValueOnce({
      data: undefined,
      error: { error: { code: "RATE_LIMITED" } },
      response: new Response(null, { status: 429, headers: { "Retry-After": "30" } }),
    } as never);

    const error = await login({ username: "alice", password: "hunter22" }).catch((e) => e);

    expect(error).toBeInstanceOf(ApiAuthError);
    expect(error).toMatchObject({ code: "RATE_LIMITED", retryAfterSeconds: 30 });
  });
});

describe("fetchMe", () => {
  it("throws ApiAuthError on a 401 body", async () => {
    mockedApi.GET.mockResolvedValueOnce({
      data: undefined,
      error: { error: { code: "UNAUTHENTICATED" } },
      response: new Response(null, { status: 401 }),
    } as never);

    await expect(fetchMe()).rejects.toMatchObject({ code: "UNAUTHENTICATED" });
  });
});

describe("logout", () => {
  it("resolves on 204", async () => {
    mockedApi.POST.mockResolvedValueOnce({
      data: undefined,
      error: undefined,
      response: new Response(null, { status: 204 }),
    } as never);

    await expect(logout()).resolves.toBeUndefined();
  });
});
