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
  /** Omitting `filters` returns the bare `["catalog", "stockLevels"]` prefix
   * — every specific-filter query key below is a TanStack partial match of
   * it, so `queryClient.invalidateQueries({ queryKey: catalogKeys.stockLevels() })`
   * invalidates every stock-levels query regardless of its filters
   * (`features/stock/hooks.ts`'s `useCreateStockAdjustment`,
   * `features/purchases/hooks.ts`'s `useReceivePurchase`) without needing to
   * know which variant/product/location was actually cached. */
  stockLevels(filters?: { variantId?: string; productId?: string; locationId?: string }) {
    return filters
      ? (["catalog", "stockLevels", filters] as const)
      : (["catalog", "stockLevels"] as const);
  },
  locations: () => ["catalog", "locations"] as const,
  /** Caches `getServerUrl()` for `features/catalog/hooks.ts`'s `useMediaUrl`
   * (resolving a relative `MediaUrls` path to an absolute one) — this
   * module used to export a shared `SERVER_URL_QUERY_KEY` for exactly this
   * kind of reactive read, removed when nothing else needed it; this is its
   * only other consumer now, so the key lives locally under this namespace
   * instead. */
  serverUrl: () => ["catalog", "serverUrl"] as const,
};

/**
 * Reports query keys (Phase 5 T6), grouped the same way `catalogKeys` is.
 * `summary`'s `filters` is `{ from?, to? }` — a cashier's own-day view
 * always calls with `{}` (the server ignores anything else, D-55), so its
 * key is stable across refetches for that role; manager+'s period toggle
 * varies it.
 */
export const reportsKeys = {
  summary: (filters: { from?: string; to?: string }) => ["reports", "summary", filters] as const,
  byProduct: (filters: { from: string; to: string }) => ["reports", "byProduct", filters] as const,
  lowStock: () => ["reports", "lowStock"] as const,
};

/**
 * Stock/purchases query keys (Phase 5 T5), added alongside `catalogKeys`
 * above for the same reason: a `filters`/`id` dimension each feature's
 * `hooks.ts` needs, kept here rather than declared ad hoc so every mutation's
 * `invalidateQueries` call (create/receive a purchase, post an adjustment)
 * targets the exact same keys the list/detail queries use. Stock *levels*
 * are deliberately not duplicated here — the shared `VariantPicker` (T2, used
 * by both the levels browser and the adjustment/purchase item pickers here)
 * already caches them under `catalogKeys.stockLevels`, so `features/stock`
 * reuses that key/fetcher instead of a second cache for the same data.
 */
export const stockKeys = {
  low: () => ["stock", "low"] as const,
  /** Omitting `filters` returns the bare `["stock", "movements"]` prefix —
   * see `catalogKeys.stockLevels`'s doc for why this shape exists. */
  movements(filters?: { variantId?: string; locationId?: string }) {
    return filters ? (["stock", "movements", filters] as const) : (["stock", "movements"] as const);
  },
};

export const purchasesKeys = {
  /** The bare `["purchases"]` prefix — every key below is a TanStack partial
   * match of it, so invalidating this one covers suppliers, every purchase
   * list filter and every purchase detail at once (`useReceivePurchase`). */
  all: ["purchases"] as const,
  suppliers: () => ["purchases", "suppliers"] as const,
  list: (filters: { status?: "draft" | "received" | "cancelled" }) =>
    ["purchases", "list", filters] as const,
  detail: (id: string) => ["purchases", "detail", id] as const,
};
