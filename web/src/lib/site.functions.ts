import { createServerFn } from "@tanstack/react-start";

import { getSiteUrlValue } from "./site.server";

/**
 * Client-safe handle to the server-only `getSiteUrlValue` — TanStack
 * Start's build replaces this handler with an RPC stub in the client
 * bundle, the same pattern as `getHealthz`/`getPublicShop`
 * (`api.functions.ts`/`publicApi.functions.ts`, ADR-011).
 */
export const getSiteUrl = createServerFn({ method: "GET" }).handler(() => getSiteUrlValue());
