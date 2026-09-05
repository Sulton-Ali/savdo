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
