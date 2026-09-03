import type { components } from "@savdo/api-client";

import { api } from "../lib/api";

export type Me = components["schemas"]["Me"];
export type LoginRequest = components["schemas"]["LoginRequest"];
export type ErrorCode = components["schemas"]["ErrorCode"];

/**
 * Thrown by every function in this module on a non-2xx response. Carries
 * only the machine-readable `code` (ADR-013) — callers translate it to a
 * display sentence via `@savdo/i18n`, never show `error.message` to a user.
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

/** `GET /auth/me` — the authenticated user, their shop and permissions. */
export async function fetchMe(): Promise<Me> {
  const { data, error } = await api.GET("/auth/me");
  if (error) {
    throw new ApiAuthError(error.error.code);
  }
  return data;
}

/**
 * `POST /auth/login` with `client: "web"` (D-29): sets the `savdo_session`
 * cookie, no `token` in the response.
 */
export async function login(credentials: {
  username: string;
  password: string;
}): Promise<Me["user"]> {
  const { data, error, response } = await api.POST("/auth/login", {
    body: { ...credentials, client: "web" },
  });
  if (error) {
    throw new ApiAuthError(error.error.code, parseRetryAfterSeconds(response));
  }
  return data.user;
}

/** `POST /auth/logout` — revokes the current session. */
export async function logout(): Promise<void> {
  const { error } = await api.POST("/auth/logout");
  if (error) {
    throw new ApiAuthError(error.error.code);
  }
}
