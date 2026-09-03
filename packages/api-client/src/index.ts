import createFetchClient, { type Client, type ClientOptions } from "openapi-fetch";

import type { components, paths } from "./schema.js";

export type { components, paths };

/** The typed Savdo API client, bound to the contract's `paths`. */
export type ApiClient = Client<paths>;

/**
 * Creates the typed Savdo API client (ADR-002). `paths`/`components` come
 * from `contracts/openapi.yaml` via `openapi-typescript` (`src/schema.d.ts`,
 * generated, never hand-edited). web, admin and mobile call the API only
 * through a client built by this function — never a hand-written `fetch`
 * (AGENTS.md hard rule 6).
 */
export function createClient(baseUrl: string, init?: Omit<ClientOptions, "baseUrl">): ApiClient {
  return createFetchClient<paths>({ baseUrl, ...init });
}
