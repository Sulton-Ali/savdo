import { createServerFn } from "@tanstack/react-start";

import { fetchHealthz } from "./api.server";

/**
 * The client-safe handle to the server-only `fetchHealthz`. TanStack Start's
 * build replaces this handler with an RPC stub in the client bundle — the
 * import of `./api.server` (and everything it touches: `API_URL`, the typed
 * client) never leaves the server (ADR-011).
 */
export const getHealthz = createServerFn({ method: "GET" }).handler(() => fetchHealthz());
