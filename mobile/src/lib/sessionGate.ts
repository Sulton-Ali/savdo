/**
 * The four states `useSession`/`RootNavigator` can be in, derived purely
 * from the two underlying TanStack Query observers (`token`, `me`) — no
 * I/O, no React, no React Native, so this is unit-testable on its own and
 * both `useSession`'s booleans and any future consumer read off the exact
 * same logic.
 */
export type SessionGate = "loading" | "unreachable" | "authenticated" | "unauthenticated";

export interface SessionGateInput {
  hasToken: boolean;
  tokenLoading: boolean;
  /** Whether `me` has ever loaded successfully — deliberately not
   * `meQuery.isError`: query-core keeps the last successful `data` across a
   * failed *background* refetch, so a transient blip must never tear down
   * an already-authenticated app. */
  hasMeData: boolean;
  meLoading: boolean;
}

export function deriveSessionGate({
  hasToken,
  tokenLoading,
  hasMeData,
  meLoading,
}: SessionGateInput): SessionGate {
  // `hasMeData` wins outright: once `me` has loaded, a concurrent
  // background refetch or its `isLoading`/error state must never hide the
  // app the user is already looking at (query-core keeps `data` on error,
  // so this branch also covers "error-with-data stays app").
  if (hasToken && hasMeData) {
    return "authenticated";
  }
  if (tokenLoading || (hasToken && meLoading)) {
    return "loading";
  }
  if (hasToken) {
    // A token exists, nothing is loading, and `me` still has no data —
    // offline, a 5xx, a token bound to a LAN IP that's since gone dark.
    return "unreachable";
  }
  return "unauthenticated";
}
