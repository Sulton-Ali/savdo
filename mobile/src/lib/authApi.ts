import type { components } from "@savdo/api-client";

import { api } from "./api";

export type Me = components["schemas"]["Me"];
export type User = components["schemas"]["User"];
export type ErrorCode = components["schemas"]["ErrorCode"];
export type TelegramLinkCode = components["schemas"]["TelegramLinkCode"];
export type TelegramLinkStatus = components["schemas"]["TelegramLinkStatus"];

/**
 * Thrown by every function in this module on a non-2xx response. Carries
 * only the machine-readable `code` (ADR-013) — callers translate it to a
 * display sentence via `@savdo/i18n`, never show `error.message` to a user.
 * Mirrors `admin/src/auth/api.ts`.
 */
export class ApiAuthError extends Error {
  readonly code: ErrorCode;
  readonly retryAfterSeconds?: number;

  constructor(code: ErrorCode, retryAfterSeconds?: number) {
    super(`auth request failed: ${code}`);
    this.name = "ApiAuthError";
    this.code = code;
    this.retryAfterSeconds = retryAfterSeconds;
  }
}

function parseRetryAfterSeconds(response: Response): number | undefined {
  const header = response.headers.get("Retry-After");
  if (!header) {
    return undefined;
  }
  const seconds = Number(header);
  return Number.isFinite(seconds) ? seconds : undefined;
}

/**
 * openapi-fetch only produces the `{ error: { code } }` envelope (ADR-013)
 * when the server actually returned JSON; a non-JSON error body (e.g. an
 * HTML 502 page from a proxy in front of the API) comes back as the raw
 * response text, so `error.error.code` isn't safe to read without checking
 * the shape first.
 */
function errorCodeFrom(error: unknown): ErrorCode {
  if (
    error &&
    typeof error === "object" &&
    "error" in error &&
    error.error &&
    typeof error.error === "object" &&
    "code" in error.error
  ) {
    return (error as { error: { code: ErrorCode } }).error.code;
  }
  return "INTERNAL";
}

/** `GET /auth/me` — the authenticated user, their shop and permissions. */
export async function fetchMe(): Promise<Me> {
  const { data, error } = await api.GET("/auth/me");
  if (error) {
    throw new ApiAuthError(errorCodeFrom(error));
  }
  return data;
}

/**
 * `POST /auth/login` with `client: "mobile"` (D-29): the response carries
 * `token`, sent afterwards as `Authorization: Bearer <token>` — no cookie.
 */
export async function login(credentials: {
  username: string;
  password: string;
}): Promise<{ token: string; user: User }> {
  const { data, error, response } = await api.POST("/auth/login", {
    body: { ...credentials, client: "mobile" },
  });
  if (error) {
    throw new ApiAuthError(errorCodeFrom(error), parseRetryAfterSeconds(response));
  }
  if (!data.token) {
    // The contract guarantees `token` for `client: "mobile"` — defensive only.
    throw new ApiAuthError("INTERNAL");
  }
  return { token: data.token, user: data.user };
}

/** `POST /auth/logout` — revokes the current session. */
export async function logout(): Promise<void> {
  const { error } = await api.POST("/auth/logout");
  if (error) {
    throw new ApiAuthError(errorCodeFrom(error));
  }
}

/** `GET /auth/telegram/link` — the caller's own Telegram link status. Any
 * authenticated role (Phase 7 T7 deliverable D). */
export async function fetchTelegramLinkStatus(): Promise<TelegramLinkStatus> {
  const { data, error } = await api.GET("/auth/telegram/link");
  if (error) {
    throw new ApiAuthError(errorCodeFrom(error));
  }
  return data;
}

/** `POST /auth/telegram/link` — starts linking the caller's own account;
 * returns a single-use 10-minute code and the deep link to open in
 * Telegram. Any authenticated role. */
export async function createTelegramLink(): Promise<TelegramLinkCode> {
  const { data, error } = await api.POST("/auth/telegram/link");
  if (error) {
    throw new ApiAuthError(errorCodeFrom(error));
  }
  return data;
}

/** `DELETE /auth/telegram/link` — unlinks the caller's own Telegram
 * account. Any authenticated role. */
export async function deleteTelegramLink(): Promise<void> {
  const { error } = await api.DELETE("/auth/telegram/link");
  if (error) {
    throw new ApiAuthError(errorCodeFrom(error));
  }
}
