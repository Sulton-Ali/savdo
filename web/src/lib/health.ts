import type { components } from "@savdo/api-client";

/**
 * Shape of the value the `/healthz` server function resolves to: whatever
 * `@savdo/api-client`'s typed `GET` returns, trimmed to what this page needs.
 */
export type HealthResult = {
  data?: { status: "ok" } | null;
  error?: components["schemas"]["Error"] | null;
};

/** Maps a health check result to the display string shown on the landing page. */
export function healthLabel(result: HealthResult): string {
  if (result.error) {
    return "API: error";
  }
  if (result.data?.status === "ok") {
    return "API: ok";
  }
  return "API: unknown";
}
