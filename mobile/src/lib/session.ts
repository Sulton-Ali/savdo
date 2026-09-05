import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { ApiAuthError, login as apiLogin, logout as apiLogout, fetchMe } from "./authApi";
import { ME_QUERY_KEY, TOKEN_QUERY_KEY } from "./queryKeys";
import { clearToken, getToken, setToken } from "./token";

function tokenQueryOptions() {
  return queryOptions({ queryKey: TOKEN_QUERY_KEY, queryFn: async () => !!(await getToken()) });
}

function meQueryOptions(enabled: boolean) {
  return queryOptions({
    queryKey: ME_QUERY_KEY,
    queryFn: fetchMe,
    enabled,
    // A `401 UNAUTHENTICATED` is a real "logged out" signal — `lib/api.ts`'s
    // middleware already clears the token for it, which disables this query
    // on the next render, so retrying it would be pointless. Anything else
    // (offline, a 5xx, a flaky proxy) gets a bounded retry before surfacing
    // as `isUnreachable`, so the gate can offer a retry instead of bouncing
    // a still-validly-authenticated user to the login form.
    retry: (failureCount, error) =>
      !(error instanceof ApiAuthError && error.code === "UNAUTHENTICATED") && failureCount < 3,
  });
}

/**
 * Session bootstrap for the whole app (D-29 mobile bearer session): the
 * SecureStore-backed token plus `GET /auth/me` once a token exists. The root
 * layout's auth gate and any screen needing the current role/shop/
 * permissions share this one hook, so they all read the same TanStack Query
 * cache entries that `useLogin`/`useLogout` and the `lib/api.ts` 401
 * middleware update.
 */
export function useSession() {
  const queryClient = useQueryClient();
  const tokenQuery = useQuery(tokenQueryOptions());
  const hasToken = !!tokenQuery.data;
  const meQuery = useQuery(meQueryOptions(hasToken));

  return {
    isLoading: tokenQuery.isLoading || (hasToken && meQuery.isLoading),
    /** A token exists but `GET /auth/me` couldn't be reached (offline, a
     * 5xx) after its retries — the token itself is still valid, so this is
     * an outage, not a logout. A `401 UNAUTHENTICATED` never reaches this
     * state: the `lib/api.ts` middleware clears `hasToken` first. */
    isUnreachable: hasToken && meQuery.isError,
    // Marks the me query stale and, since it's actively observed here with
    // `enabled: true`, triggers a new attempt — same mechanism `useLogin`
    // uses below.
    retry: () => {
      void queryClient.invalidateQueries({ queryKey: ME_QUERY_KEY });
    },
    isAuthenticated: hasToken && !!meQuery.data,
    me: meQuery.data,
    role: meQuery.data?.user.role,
    shop: meQuery.data?.shop,
    /** Checks a `me.permissions` capability string (ADR-010, D-81) — mirrors
     * `admin/src/auth/AuthContext.tsx`. The UI hides what a role cannot do;
     * the API stays the enforcement point. */
    can: (permission: string) => !!meQuery.data?.permissions.includes(permission),
  };
}

/** Logs in against the server currently configured in `serverUrl.ts` and
 * stores the returned bearer token. */
export function useLogin() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: apiLogin,
    onSuccess: async ({ token }) => {
      await setToken(token);
      queryClient.setQueryData(TOKEN_QUERY_KEY, true);
      // `me` may already be cached in an error state from a failed startup
      // fetch (no token, or an old one) — `enabled` and the query key are
      // unchanged by logging in, so nothing else would trigger a refetch
      // and the app would stay stuck on the login screen despite a
      // successful login.
      await queryClient.invalidateQueries({ queryKey: ME_QUERY_KEY });
    },
  });
}

/**
 * Revokes the session server-side, then always clears the local token and
 * the entire query cache — even if the request failed, a stale local token
 * is worse than a session still alive on the server (the owner can revoke
 * it from the admin), and stale cached data is worse still: the next login
 * (a different cashier, say) must never see the previous user's data
 * (ADR-010). Mirrors `admin/src/auth/AuthContext.tsx`. The token entry is
 * re-seeded after `clear()` so the reactive auth gate sees `false`
 * immediately instead of an empty (re-fetching) cache entry.
 */
export function useLogout() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: apiLogout,
    onSettled: async () => {
      await clearToken();
      queryClient.clear();
      queryClient.setQueryData(TOKEN_QUERY_KEY, false);
    },
  });
}
