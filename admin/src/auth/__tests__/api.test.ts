import { describe, expect, it, vi } from "vitest";

vi.mock("../../lib/api", () => ({
  api: {
    GET: vi.fn(),
    POST: vi.fn(),
    DELETE: vi.fn(),
  },
}));

import { api } from "../../lib/api";
import {
  ApiAuthError,
  authenticateTelegram,
  createTelegramLink,
  deleteTelegramLink,
  fetchMe,
  fetchTelegramLinkStatus,
  login,
  logout,
  requestPasswordResetOtp,
  resetPassword,
  verifyPasswordResetOtp,
} from "../api";

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

describe("authenticateTelegram", () => {
  it("posts the widget payload and returns the user", async () => {
    mockedApi.POST.mockResolvedValueOnce({
      data: { user: { id: "u1" }, session: { id: "s1" } },
      error: undefined,
      response: new Response(null, { status: 200 }),
    } as never);

    const payload = {
      id: "12345",
      firstName: "Alice",
      authDate: 1_700_000_000,
      hash: "deadbeef",
    };
    const user = await authenticateTelegram(payload);

    expect(mockedApi.POST).toHaveBeenCalledWith("/auth/telegram", { body: payload });
    expect(user).toEqual({ id: "u1" });
  });

  it("throws ApiAuthError with UNAUTHENTICATED for an unlinked/bad-HMAC payload", async () => {
    mockedApi.POST.mockResolvedValueOnce({
      data: undefined,
      error: { error: { code: "UNAUTHENTICATED" } },
      response: new Response(null, { status: 401 }),
    } as never);

    await expect(
      authenticateTelegram({ id: "12345", authDate: 1, hash: "x" }),
    ).rejects.toMatchObject({ code: "UNAUTHENTICATED" });
  });

  it("captures Retry-After seconds on a 429 body", async () => {
    mockedApi.POST.mockResolvedValueOnce({
      data: undefined,
      error: { error: { code: "RATE_LIMITED" } },
      response: new Response(null, { status: 429, headers: { "Retry-After": "20" } }),
    } as never);

    const error = await authenticateTelegram({ id: "12345", authDate: 1, hash: "x" }).catch(
      (e) => e,
    );

    expect(error).toMatchObject({ code: "RATE_LIMITED", retryAfterSeconds: 20 });
  });
});

describe("requestPasswordResetOtp", () => {
  it("posts username and the password_reset purpose", async () => {
    mockedApi.POST.mockResolvedValueOnce({
      data: undefined,
      error: undefined,
      response: new Response(null, { status: 202 }),
    } as never);

    await requestPasswordResetOtp("alice");

    expect(mockedApi.POST).toHaveBeenCalledWith("/auth/otp/request", {
      body: { username: "alice", purpose: "password_reset" },
    });
  });

  it("captures Retry-After seconds on a 429 body", async () => {
    mockedApi.POST.mockResolvedValueOnce({
      data: undefined,
      error: { error: { code: "RATE_LIMITED" } },
      response: new Response(null, { status: 429, headers: { "Retry-After": "15" } }),
    } as never);

    const error = await requestPasswordResetOtp("alice").catch((e) => e);

    expect(error).toMatchObject({ code: "RATE_LIMITED", retryAfterSeconds: 15 });
  });
});

describe("verifyPasswordResetOtp", () => {
  it("posts username, purpose and code, returning the action token", async () => {
    mockedApi.POST.mockResolvedValueOnce({
      data: { actionToken: "tok_abc", expiresAt: "2026-09-09T00:10:00Z" },
      error: undefined,
      response: new Response(null, { status: 200 }),
    } as never);

    const result = await verifyPasswordResetOtp("alice", "123456");

    expect(mockedApi.POST).toHaveBeenCalledWith("/auth/otp/verify", {
      body: { username: "alice", purpose: "password_reset", code: "123456" },
    });
    expect(result).toEqual({ actionToken: "tok_abc", expiresAt: "2026-09-09T00:10:00Z" });
  });

  it("throws ApiAuthError with UNAUTHENTICATED for a wrong/expired code", async () => {
    mockedApi.POST.mockResolvedValueOnce({
      data: undefined,
      error: { error: { code: "UNAUTHENTICATED" } },
      response: new Response(null, { status: 401 }),
    } as never);

    await expect(verifyPasswordResetOtp("alice", "000000")).rejects.toMatchObject({
      code: "UNAUTHENTICATED",
    });
  });
});

describe("resetPassword", () => {
  it("posts the action token and new password", async () => {
    mockedApi.POST.mockResolvedValueOnce({
      data: undefined,
      error: undefined,
      response: new Response(null, { status: 204 }),
    } as never);

    await resetPassword("tok_abc", "hunter2222");

    expect(mockedApi.POST).toHaveBeenCalledWith("/auth/password/reset", {
      body: { actionToken: "tok_abc", newPassword: "hunter2222" },
    });
  });

  it("throws ApiAuthError with UNAUTHENTICATED for an expired action token", async () => {
    mockedApi.POST.mockResolvedValueOnce({
      data: undefined,
      error: { error: { code: "UNAUTHENTICATED" } },
      response: new Response(null, { status: 401 }),
    } as never);

    await expect(resetPassword("tok_dead", "hunter2222")).rejects.toMatchObject({
      code: "UNAUTHENTICATED",
    });
  });
});

describe("Telegram link status", () => {
  it("fetchTelegramLinkStatus returns the status", async () => {
    mockedApi.GET.mockResolvedValueOnce({
      data: { linked: true, telegramUsername: "alice_tg" },
      error: undefined,
      response: new Response(null, { status: 200 }),
    } as never);

    await expect(fetchTelegramLinkStatus()).resolves.toEqual({
      linked: true,
      telegramUsername: "alice_tg",
    });
  });

  it("createTelegramLink returns the code and deep link", async () => {
    mockedApi.POST.mockResolvedValueOnce({
      data: { code: "abc123", deepLink: "https://t.me/savdo_bot?start=link_abc123" },
      error: undefined,
      response: new Response(null, { status: 200 }),
    } as never);

    await expect(createTelegramLink()).resolves.toEqual({
      code: "abc123",
      deepLink: "https://t.me/savdo_bot?start=link_abc123",
    });
    expect(mockedApi.POST).toHaveBeenCalledWith("/auth/telegram/link");
  });

  it("deleteTelegramLink resolves on 204", async () => {
    mockedApi.DELETE.mockResolvedValueOnce({
      data: undefined,
      error: undefined,
      response: new Response(null, { status: 204 }),
    } as never);

    await expect(deleteTelegramLink()).resolves.toBeUndefined();
  });
});
