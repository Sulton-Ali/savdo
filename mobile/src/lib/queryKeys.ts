/**
 * Shared TanStack Query keys for session state — kept in one place so
 * `lib/api.ts` (outside the component tree) and `lib/session.ts` (React
 * hooks) invalidate/update the same cache entries without importing each
 * other (that pair would form an import cycle through `lib/authApi.ts`).
 */
export const TOKEN_QUERY_KEY = ["session", "token"] as const;
export const ME_QUERY_KEY = ["auth", "me"] as const;
export const SERVER_URL_QUERY_KEY = ["session", "serverUrl"] as const;
