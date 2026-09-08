/**
 * Flattens cursor-paginated pages (`useProductsSearch`'s `useInfiniteQuery`
 * `data.pages`, each a `CursorPage<Product>`) into the single ordered array
 * every consumer renders. A plain, generic function with no imports of its
 * own (mirrors `pricing.ts`/`qty.ts` in this folder) — not declared inline
 * at each `useMemo` call site, so it has one behaviour to unit-test
 * independently of React/React Native (`productPages.test.ts`; this
 * package's Vitest setup covers pure `src/features/**` modules only, not
 * hooks or screens — `vitest.config.mts`).
 *
 * Both `VariantPicker` and the Products tab (`app/(app)/(tabs)/products/
 * index.tsx`) must derive their list from `useProductsSearch`'s `pages`
 * this way, never from a second, differently-shaped query under the same
 * `catalogKeys.products` key — see `hooks.ts`'s `useProductsSearch` doc for
 * the bug that caused (T20, owner phone-test 2026-09-08): a second reader
 * expecting `{ items }` instead of `{ pages, pageParams }` read whichever
 * shape the other screen's query had last cached and saw an empty list.
 */
export function flattenProductPages<T>(pages: Array<{ items: T[] }> | undefined): T[] {
  return pages?.flatMap((page) => page.items) ?? [];
}
