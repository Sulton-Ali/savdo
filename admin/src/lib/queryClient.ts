import { QueryClient } from "@tanstack/react-query";

/**
 * The single TanStack Query client, shared between `QueryClientProvider`
 * (`main.tsx`) and the router's `beforeLoad` context (`router.tsx`) so the
 * route guard's `ensureQueryData(meQueryOptions())` and any component's
 * `useMe()` read the same cache entry.
 */
export const queryClient = new QueryClient();
