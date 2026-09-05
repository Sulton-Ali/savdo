import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";

import { ME_QUERY_KEY, resetSessionCache, TOKEN_QUERY_KEY } from "./queryKeys";

function newClient(): QueryClient {
  // No retries, no GC delay — this is exercising cache mechanics
  // synchronously, not real network behaviour.
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

describe("resetSessionCache", () => {
  it("notifies a mounted TOKEN_QUERY_KEY observer of the new value", () => {
    const queryClient = newClient();
    queryClient.setQueryData(TOKEN_QUERY_KEY, false);

    const observer = new QueryObserver<boolean>(queryClient, {
      queryKey: TOKEN_QUERY_KEY,
      queryFn: () => false,
    });
    const results: (boolean | undefined)[] = [];
    const unsubscribe = observer.subscribe((result) => {
      results.push(result.data);
    });

    resetSessionCache(queryClient, true);

    expect(observer.getCurrentResult().data).toBe(true);
    expect(results.at(-1)).toBe(true);

    unsubscribe();
  });

  it("removes a non-session query from the cache", () => {
    const queryClient = newClient();
    queryClient.setQueryData(["catalog", "products"], [{ id: 1 }]);
    queryClient.setQueryData(TOKEN_QUERY_KEY, true);

    resetSessionCache(queryClient, false);

    expect(queryClient.getQueryData(["catalog", "products"])).toBeUndefined();
    expect(queryClient.getQueryData(TOKEN_QUERY_KEY)).toBe(false);
  });

  it("resets ME_QUERY_KEY's data instead of leaving a previous session's value in place", () => {
    const queryClient = newClient();
    queryClient.setQueryData(ME_QUERY_KEY, { user: { role: "owner" } });

    resetSessionCache(queryClient, true);

    expect(queryClient.getQueryData(ME_QUERY_KEY)).toBeUndefined();
  });

  it("keeps a mounted ME_QUERY_KEY observer attached (not destroyed) across the reset", () => {
    const queryClient = newClient();
    queryClient.setQueryData(ME_QUERY_KEY, { user: { role: "owner" } });

    const observer = new QueryObserver(queryClient, {
      queryKey: ME_QUERY_KEY,
      queryFn: () => ({ user: { role: "cashier" } }),
      enabled: false,
    });
    const results: unknown[] = [];
    const unsubscribe = observer.subscribe((result) => {
      results.push(result.data);
    });

    resetSessionCache(queryClient, true);

    // The observer built above is still the one seeing updates — a `clear()`
    // would have destroyed its underlying Query and any later write would
    // have gone to a new, detached one instead (see the regression test
    // below).
    expect(observer.getCurrentResult().data).toBeUndefined();
    expect(results.at(-1)).toBeUndefined();

    unsubscribe();
  });

  it("regression: clear() + setQueryData is the bug this helper avoids", () => {
    const queryClient = newClient();
    queryClient.setQueryData(TOKEN_QUERY_KEY, false);

    const observer = new QueryObserver<boolean>(queryClient, {
      queryKey: TOKEN_QUERY_KEY,
      queryFn: () => false,
    });
    const results: (boolean | undefined)[] = [];
    const unsubscribe = observer.subscribe((result) => {
      results.push(result.data);
    });

    queryClient.clear();
    queryClient.setQueryData(TOKEN_QUERY_KEY, true);

    // The observer is still attached to the Query instance `clear()`
    // destroyed; the `setQueryData` right after built a new, detached one,
    // so the mounted observer never sees the update.
    expect(observer.getCurrentResult().data).not.toBe(true);
    expect(results.some((value) => value === true)).toBe(false);

    unsubscribe();
  });
});
