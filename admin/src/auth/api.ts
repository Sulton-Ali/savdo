import type { components } from "@savdo/api-client";

import { api } from "../lib/api";

export type Me = components["schemas"]["Me"];
export type LoginRequest = components["schemas"]["LoginRequest"];
export type ErrorCode = components["schemas"]["ErrorCode"];
export type TelegramAuthRequest = components["schemas"]["TelegramAuthRequest"];
export type OtpVerifyResponse = components["schemas"]["OtpVerifyResponse"];
export type TelegramLinkCode = components["schemas"]["TelegramLinkCode"];
export type TelegramLinkStatus = components["schemas"]["TelegramLinkStatus"];

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

/**
 * `POST /auth/telegram` — the Telegram Login Widget's callback payload
 * (ADR-005). Same session/cookie result as `login`'s `client: "web"`; a
 * failed HMAC and an unlinked Telegram id both answer `401 UNAUTHENTICATED`
 * (no enumeration, docs/05-API.md § Auth).
 */
export async function authenticateTelegram(payload: TelegramAuthRequest): Promise<Me["user"]> {
  const { data, error, response } = await api.POST("/auth/telegram", { body: payload });
  if (error) {
    throw new ApiAuthError(error.error.code, parseRetryAfterSeconds(response));
  }
  return data.user;
}

/**
 * `POST /auth/otp/request` for the `password_reset` purpose — always
 * answers `202` regardless of whether `username` exists or is linked, so
 * the caller must never infer anything from success alone (no
 * enumeration, docs/05-API.md § Auth).
 */
export async function requestPasswordResetOtp(username: string): Promise<void> {
  const { error, response } = await api.POST("/auth/otp/request", {
    body: { username, purpose: "password_reset" },
  });
  if (error) {
    throw new ApiAuthError(error.error.code, parseRetryAfterSeconds(response));
  }
}

/**
 * `POST /auth/otp/verify` for the `password_reset` purpose — exchanges the
 * 6-digit code for a short-lived `actionToken` (10 min, single use). `401
 * UNAUTHENTICATED` covers a wrong/expired code and an exhausted-attempts
 * code alike (no enumeration).
 */
export async function verifyPasswordResetOtp(
  username: string,
  code: string,
): Promise<OtpVerifyResponse> {
  const { data, error, response } = await api.POST("/auth/otp/verify", {
    body: { username, purpose: "password_reset", code },
  });
  if (error) {
    throw new ApiAuthError(error.error.code, parseRetryAfterSeconds(response));
  }
  return data;
}

/**
 * `POST /auth/password/reset` — `actionToken` from `verifyPasswordResetOtp`
 * is the credential; revokes every other session of that user server-side.
 */
export async function resetPassword(actionToken: string, newPassword: string): Promise<void> {
  const { error } = await api.POST("/auth/password/reset", {
    body: { actionToken, newPassword },
  });
  if (error) {
    throw new ApiAuthError(error.error.code);
  }
}

/** `GET /auth/telegram/link` — the caller's own Telegram link status. Any
 * authenticated role. */
export async function fetchTelegramLinkStatus(): Promise<TelegramLinkStatus> {
  const { data, error } = await api.GET("/auth/telegram/link");
  if (error) {
    throw new ApiAuthError(error.error.code);
  }
  return data;
}

/** `POST /auth/telegram/link` — starts linking the caller's own account;
 * returns a single-use 10-minute code and the deep link to open in
 * Telegram. Any authenticated role. */
export async function createTelegramLink(): Promise<TelegramLinkCode> {
  const { data, error } = await api.POST("/auth/telegram/link");
  if (error) {
    throw new ApiAuthError(error.error.code);
  }
  return data;
}

/** `DELETE /auth/telegram/link` — unlinks the caller's own Telegram
 * account. Any authenticated role. */
export async function deleteTelegramLink(): Promise<void> {
  const { error } = await api.DELETE("/auth/telegram/link");
  if (error) {
    throw new ApiAuthError(error.error.code);
  }
}
