import type { QueryClient, QueryKey } from "@tanstack/react-query";

/**
 * Shared TanStack Query keys for session state — kept in one place so
 * `lib/api.ts` (outside the component tree) and `lib/session.ts` (React
 * hooks) invalidate/update the same cache entries without importing each
 * other (that pair would form an import cycle through `lib/authApi.ts`).
 *
 * `TOKEN_QUERY_KEY` caches only a `boolean` — "is a token currently stored"
 * — never the raw token string; the token itself lives in SecureStore only
 * (`lib/token.ts`), never in the TanStack Query cache, React state, or logs.
 */
export const TOKEN_QUERY_KEY = ["session", "token"] as const;
export const ME_QUERY_KEY = ["auth", "me"] as const;

const SESSION_QUERY_KEYS: readonly QueryKey[] = [TOKEN_QUERY_KEY, ME_QUERY_KEY];

function keysEqual(a: readonly unknown[], b: readonly unknown[]): boolean {
  return a.length === b.length && a.every((part, i) => part === b[i]);
}

function isSessionKey(queryKey: readonly unknown[]): boolean {
  return SESSION_QUERY_KEYS.some((key) => keysEqual(key, queryKey));
}

/**
 * Crosses a session boundary (login, logout, an outage escape, a 401) —
 * ADR-010 requires no cross-session data to survive it, but a
 * `queryClient.clear()` followed by `setQueryData` is the wrong way to get
 * there: `clear()` calls `query.destroy()` on every cached `Query`,
 * including `TOKEN_QUERY_KEY`/`ME_QUERY_KEY` themselves, and does *not*
 * detach or notify the `QueryObserver`s the root layout's and the `(app)`
 * tabs layout's mounted `useSession()` calls hold — verified against
 * `@tanstack/query-core` 5.102.8 by constructing a `QueryObserver`,
 * subscribing, then calling `clear()` + `setQueryData`: zero notifications,
 * the observer's result never changes (see `queryKeys.test.ts`). The
 * `setQueryData` right after `clear()` builds a brand-new `Query` object
 * those already-mounted observers were never attached to, so the auth gate
 * can be stuck showing the pre-transition screen (still on the login form
 * after a successful login, still inside the previous cashier's tabs after
 * logging out) until something unrelated forces a re-render.
 *
 * Instead: update the token flag on the *existing*, still-attached
 * `TOKEN_QUERY_KEY` query (so the mounted observer is notified
 * synchronously), reset `ME_QUERY_KEY`'s data rather than removing it (same
 * reason — a screen may be observing it right now) so a previous session's
 * `me` never lingers even for one frame, and only *remove* every other
 * query (product lists, customers, …) — those have no long-lived observer
 * tied to the session boundary the way the two session queries do, so
 * destroying them outright is safe.
 */
export function resetSessionCache(queryClient: QueryClient, hasToken: boolean): void {
  queryClient.setQueryData(TOKEN_QUERY_KEY, hasToken);
  queryClient.removeQueries({ predicate: (query) => !isSessionKey(query.queryKey) });
  queryClient.resetQueries({ queryKey: ME_QUERY_KEY });
}

/**
 * Catalog query keys (Phase 5 T2), added to this shared file rather than
 * declared ad hoc in `features/catalog/hooks.ts` — T1 left no key-builder
 * extension point (the keys above are fixed, param-less arrays), so these
 * are the first to need a `filters`/`id` dimension and are grouped under
 * one namespace object to keep the export surface small.
 */
export const catalogKeys = {
  products: (filters: { q?: string }) => ["catalog", "products", filters] as const,
  product: (id: string) => ["catalog", "product", id] as const,
  variants: (productId: string) => ["catalog", "variants", productId] as const,
  stockLevels: (filters: { variantId?: string; productId?: string; locationId?: string }) =>
    ["catalog", "stockLevels", filters] as const,
  locations: () => ["catalog", "locations"] as const,
  /** Caches `getServerUrl()` for `features/catalog/hooks.ts`'s `useMediaUrl`
   * (resolving a relative `MediaUrls` path to an absolute one) — this
   * module used to export a shared `SERVER_URL_QUERY_KEY` for exactly this
   * kind of reactive read, removed when nothing else needed it; this is its
   * only other consumer now, so the key lives locally under this namespace
   * instead. */
  serverUrl: () => ["catalog", "serverUrl"] as const,
};
