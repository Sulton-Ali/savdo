import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { login as apiLogin, logout as apiLogout, fetchMe } from "./authApi";
import { ME_QUERY_KEY, TOKEN_QUERY_KEY } from "./queryKeys";
import { clearToken, getToken, setToken } from "./token";

function tokenQueryOptions() {
  return queryOptions({ queryKey: TOKEN_QUERY_KEY, queryFn: getToken });
}

function meQueryOptions(enabled: boolean) {
  return queryOptions({ queryKey: ME_QUERY_KEY, queryFn: fetchMe, enabled, retry: false });
}

/**
 * Session bootstrap for the whole app (D-29 mobile bearer session): the
 * SecureStore-backed token plus `GET /auth/me` once a token exists. The root
 * layout's auth gate and any screen needing the current role/shop share this
 * one hook, so they all read the same TanStack Query cache entries that
 * `useLogin`/`useLogout` and the `lib/api.ts` 401 middleware update.
 */
export function useSession() {
  const tokenQuery = useQuery(tokenQueryOptions());
  const hasToken = !!tokenQuery.data;
  const meQuery = useQuery(meQueryOptions(hasToken));

  return {
    isLoading: tokenQuery.isLoading || (hasToken && meQuery.isLoading),
    isAuthenticated: hasToken && !!meQuery.data,
    me: meQuery.data,
    role: meQuery.data?.user.role,
    shop: meQuery.data?.shop,
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
      queryClient.setQueryData(TOKEN_QUERY_KEY, token);
    },
  });
}

/** Revokes the session server-side, then always clears the local token —
 * even if the request failed, a stale local token is worse than a session
 * still alive on the server (the owner can revoke it from the admin). */
export function useLogout() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: apiLogout,
    onSettled: async () => {
      await clearToken();
      queryClient.setQueryData(TOKEN_QUERY_KEY, null);
      queryClient.removeQueries({ queryKey: ME_QUERY_KEY });
    },
  });
}
