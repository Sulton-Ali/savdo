import { queryOptions, useQuery } from "@tanstack/react-query";

import { fetchMe } from "./api";

/** Shared query key/fn so the route guard's `beforeLoad` and any component
 * calling `useMe()` hit the same TanStack Query cache entry. */
export function meQueryOptions() {
  return queryOptions({
    queryKey: ["auth", "me"] as const,
    queryFn: fetchMe,
    retry: false,
  });
}

/** `GET /auth/me` as a query. Session bootstrap for the whole admin app. */
export function useMe() {
  return useQuery(meQueryOptions());
}
