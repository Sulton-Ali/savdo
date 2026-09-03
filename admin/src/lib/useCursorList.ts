import { useInfiniteQuery } from "@tanstack/react-query";

/** Every cursor-paginated collection response shape (`docs/05-API.md` §
 * Conventions): `{ items, nextCursor }`, never `?page=`/`?offset=`. */
export interface CursorPage<T> {
  items: T[];
  nextCursor: string | null;
}

/**
 * Wraps `useInfiniteQuery` over a `{ items, nextCursor }` endpoint (limit 50
 * per page, per the endpoints' default). Pages are exposed via `.data.pages`;
 * callers flatten them for a "load more" list rather than page-based
 * pagination (D-25/ADR conventions — cursor only, never `?page=`).
 */
export function useCursorList<T>(
  queryKey: readonly unknown[],
  fetchPage: (cursor: string | null) => Promise<CursorPage<T>>,
) {
  return useInfiniteQuery({
    queryKey,
    queryFn: ({ pageParam }) => fetchPage(pageParam),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.nextCursor,
  });
}
