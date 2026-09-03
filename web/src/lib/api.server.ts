import type { components } from "@savdo/api-client";
import { createClient } from "@savdo/api-client";

import type { HealthResult } from "./health";

/**
 * Reads the API's liveness status. Building the typed client from `API_URL`
 * happens only in this module: its `*.server.*` name is TanStack Start's
 * import-protection convention, so the Vite plugin refuses to bundle it for
 * the client — the API URL can never reach the browser (ADR-011). It must
 * only ever be called from inside a `createServerFn` handler (see
 * `api.functions.ts`), never imported directly by route/component code.
 */
export async function fetchHealthz(): Promise<HealthResult> {
  const client = createClient(process.env.API_URL ?? "http://localhost:8080/v1");
  const { data, error } = await client.GET("/healthz");
  return {
    data: data ?? null,
    // `/healthz` documents no error response (contracts/openapi.yaml), so
    // openapi-fetch widens `error` to `unknown`; narrow it to the shared
    // error envelope (ADR-013) so the server function result stays
    // serializable.
    error: (error as components["schemas"]["Error"] | undefined) ?? null,
  };
}
