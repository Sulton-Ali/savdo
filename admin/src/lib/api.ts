import { createClient } from "@savdo/api-client";

/**
 * The one typed Savdo API client for the admin app (ADR-002, AGENTS.md hard
 * rule 6). Never call `fetch` directly anywhere else in `admin/src`.
 *
 * Base URL defaults to the Vite dev proxy (`/api`, see `vite.config.ts`),
 * which strips the `/api` prefix the same way production Caddy does — the
 * browser always talks to `<base>/v1/...`.
 */
export const api = createClient(`${import.meta.env.VITE_API_BASE ?? "/api"}/v1`);
