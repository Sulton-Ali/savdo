import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";

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
      // Fresh data + an infinite `staleTime` means `subscribe()` below does
      // not itself trigger a mount fetch/notify — the only notifications
      // this test wants to observe are the ones (or absence of ones) from
      // the `clear()` + `setQueryData` sequence under test.
      staleTime: Number.POSITIVE_INFINITY,
    });
    const results: (boolean | undefined)[] = [];
    const unsubscribe = observer.subscribe((result) => {
      results.push(result.data);
    });

    queryClient.clear();
    queryClient.setQueryData(TOKEN_QUERY_KEY, true);

    // The observer is still attached to the Query instance `clear()`
    // destroyed; the `setQueryData` right after built a new, detached one,
    // so the mounted observer is never notified at all — not "never sees
    // true" (which a coincidental unrelated notification could satisfy),
    // but zero notifications, and its cached result keeps reading the
    // stale pre-clear value instead of picking up the new one.
    expect(results.length).toBe(0);
    expect(observer.getCurrentResult().data).toBe(false);

    unsubscribe();
  });

  it("resets ME_QUERY_KEY before flipping TOKEN_QUERY_KEY (ADR-010 ordering)", () => {
    const queryClient = newClient();
    queryClient.setQueryData(TOKEN_QUERY_KEY, false);
    queryClient.setQueryData(ME_QUERY_KEY, { user: { role: "owner" } });

    const order: string[] = [];
    const unsubscribe = queryClient.getQueryCache().subscribe((event) => {
      if (event.type !== "updated") return;
      const key = event.query.queryKey;
      if (JSON.stringify(key) === JSON.stringify(ME_QUERY_KEY)) order.push("me");
      if (JSON.stringify(key) === JSON.stringify(TOKEN_QUERY_KEY)) order.push("token");
    });

    resetSessionCache(queryClient, true);
    unsubscribe();

    // No mounted observer must ever be able to read the new token value
    // together with the previous session's stale `me` in the same tick:
    // `me` has to be gone (or resetting) before `TOKEN_QUERY_KEY` flips.
    expect(order.indexOf("me")).toBeGreaterThanOrEqual(0);
    expect(order.indexOf("token")).toBeGreaterThan(order.indexOf("me"));
  });

  it("logging out removes ME_QUERY_KEY without triggering a refetch", () => {
    const queryClient = newClient();
    queryClient.setQueryData(TOKEN_QUERY_KEY, true);
    queryClient.setQueryData(ME_QUERY_KEY, { user: { role: "owner" } });

    const fetchMe = vi.fn().mockResolvedValue({ user: { role: "owner" } });
    const observer = new QueryObserver(queryClient, {
      queryKey: ME_QUERY_KEY,
      queryFn: fetchMe,
      enabled: true,
      // Matches `session.ts`'s real `SESSION_STALE_TIME_MS` so this mounted
      // observer's own `subscribe()` doesn't trigger a mount fetch before
      // `resetSessionCache` even runs — the assertion below is about the
      // logout call, not incidental mount behaviour.
      staleTime: 60_000,
    });
    const unsubscribe = observer.subscribe(() => {});

    resetSessionCache(queryClient, false);

    // `removeQueries` (not `resetQueries`) on logout/escape: dropping the
    // cache must not fire one more tokenless `GET /auth/me` — there is no
    // session left for it to read, and on the escape-from-unreachable path
    // it would just retry against the address that already proved dead.
    expect(fetchMe).not.toHaveBeenCalled();
    expect(queryClient.getQueryData(ME_QUERY_KEY)).toBeUndefined();

    unsubscribe();
  });

  it("logging in resets ME_QUERY_KEY and lets a mounted observer refetch it", () => {
    const queryClient = newClient();
    queryClient.setQueryData(TOKEN_QUERY_KEY, false);
    queryClient.setQueryData(ME_QUERY_KEY, { user: { role: "owner" } });

    const fetchMe = vi.fn().mockResolvedValue({ user: { role: "cashier" } });
    const observer = new QueryObserver(queryClient, {
      queryKey: ME_QUERY_KEY,
      queryFn: fetchMe,
      enabled: true,
      staleTime: 60_000,
    });
    const unsubscribe = observer.subscribe(() => {});

    resetSessionCache(queryClient, true);

    // Logging in a new session (a different cashier, say) must fetch that
    // session's own `me` as soon as it's available, unlike the logout path.
    expect(fetchMe).toHaveBeenCalledTimes(1);

    unsubscribe();
  });
});
