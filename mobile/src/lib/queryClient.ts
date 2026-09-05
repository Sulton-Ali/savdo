import { QueryClient } from "@tanstack/react-query";

/**
 * The single TanStack Query client (mirrors `admin/src/lib/queryClient.ts`):
 * shared between `QueryClientProvider` (`app/_layout.tsx`) and `api.ts`'s
 * 401 middleware, which needs to drop the cached session on a revoked
 * session from outside the component tree.
 */
export const queryClient = new QueryClient();
