import {
  useInfiniteQuery,
  useMutation,
  useQueries,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useMemo } from "react";

import { getProduct, listAllStockLevels } from "@/features/catalog/api";
import { catalogKeys, purchasesKeys, reportsKeys, stockKeys } from "@/lib/queryKeys";

import {
  createStockAdjustment,
  listLowStock,
  listStockMovements,
  type StockAdjustmentCreate,
  type StockLowItem,
} from "./api";

/** A variant's stock levels across every location it has a `stock_levels`
 * row for (deliverable 3's variant movements screen also wants this to show
 * "current qty" alongside the ledger). Reuses `features/catalog/api.ts`'s
 * `listAllStockLevels`/`catalogKeys.stockLevels` — the same cache the shared
 * `VariantPicker` (T2) already fills for this app's other screens, so a
 * write here and a write there invalidate one cache, not two. Not
 * cursor-paginated for the caller — a variant has at most a handful of
 * locations. */
export function useVariantStockLevels(variantId: string | undefined) {
  return useQuery({
    queryKey: catalogKeys.stockLevels({ variantId }),
    queryFn: () => listAllStockLevels({ variantId }),
    enabled: variantId != null,
  });
}

/** `GET /stock/low`, cursor-paginated — requires `stock.write` (manager+,
 * `docs/05-API.md` § Stock). `enabled` lets a caller skip firing this query
 * at all for a role that would only get a `403` back (a cashier). */
export function useLowStock(enabled: boolean) {
  return useInfiniteQuery({
    queryKey: stockKeys.low(),
    queryFn: ({ pageParam }) => listLowStock(pageParam),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.nextCursor,
    enabled,
  });
}

/** `GET /stock/movements` for one variant — requires `stock.write`. Powers
 * the optional variant movements-history screen. */
export function useVariantMovements(variantId: string | undefined) {
  return useInfiniteQuery({
    queryKey: stockKeys.movements({ variantId }),
    queryFn: ({ pageParam }) => listStockMovements({ variantId, cursor: pageParam }),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.nextCursor,
    enabled: variantId != null,
  });
}

export interface LowStockRow extends StockLowItem {
  productName: string;
  variantLabel: string;
  sku: string | null;
}

/**
 * `useLowStock` flattened and joined to product/variant display fields —
 * `StockLowItem` carries only ids and numbers (`contracts/openapi.yaml`),
 * so this fetches each distinct product once via `useQueries` and looks up
 * the matching variant client-side. Mirrors
 * `admin/src/routes/app/StockLowPage.tsx` exactly.
 */
export function useLowStockRows(enabled: boolean) {
  const lowStock = useLowStock(enabled);
  const items = useMemo(
    () => lowStock.data?.pages.flatMap((page) => page.items) ?? [],
    [lowStock.data],
  );

  const productIds = useMemo(
    () => Array.from(new Set(items.map((item) => item.productId))),
    [items],
  );
  const productQueries = useQueries({
    queries: productIds.map((id) => ({
      queryKey: catalogKeys.product(id),
      queryFn: () => getProduct(id),
    })),
  });

  const rows = useMemo<LowStockRow[]>(() => {
    const productsById = new Map(productIds.map((id, index) => [id, productQueries[index]?.data]));
    return items.map((item) => {
      const product = productsById.get(item.productId);
      const variant = product?.variants?.find((candidate) => candidate.id === item.variantId);
      const attributesLabel = variant
        ? Object.entries(variant.attributes)
            .map(([key, val]) => `${key}: ${val}`)
            .join(", ")
        : "";
      return {
        ...item,
        productName: product?.name ?? "…",
        variantLabel: attributesLabel,
        sku: variant?.sku ?? null,
      };
    });
  }, [items, productIds, productQueries]);

  return { rows, ...lowStock };
}

/**
 * Invalidates every cache a stock-affecting write can change: stock levels
 * (`catalogKeys.stockLevels` — the same cache `VariantPicker` fills, per
 * this module's `useVariantStockLevels` doc), low stock, every variant's
 * movement ledger, the purchases list/detail, and every reports query
 * (`reportsKeys.all`) — a receive or adjustment changes the home screen's
 * low-stock count (`features/reports/hooks.ts`'s own `useLowStock`, cached
 * under `reportsKeys.lowStock()`, a *different* key from this module's
 * `stockKeys.low()` below) and can move today's summary too, so without
 * this the home screen (`app/(app)/index.tsx`) kept showing stale numbers
 * until a manual pull-to-refresh (T11 follow-up). `stockKeys.low()` stays
 * — this module's own `useLowStock` still reads it. Shared by
 * `useCreateStockAdjustment` below and `features/purchases/hooks.ts`'s
 * `useReceivePurchase` — a receive changes a purchase's own status too, and
 * an adjustment invalidating purchases costs nothing beyond an extra
 * background refetch, so one shared helper stands in for two near-identical
 * ones. Each `queryKey` here omits its `filters` argument, so it resolves to
 * the bare namespace prefix (`catalogKeys.stockLevels`/`stockKeys.movements`'s
 * docs) and matches every cached query under it, not just one variant's.
 */
export async function invalidateStockAndPurchases(
  queryClient: ReturnType<typeof useQueryClient>,
): Promise<void> {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: catalogKeys.stockLevels() }),
    queryClient.invalidateQueries({ queryKey: stockKeys.low() }),
    queryClient.invalidateQueries({ queryKey: stockKeys.movements() }),
    queryClient.invalidateQueries({ queryKey: purchasesKeys.all }),
    queryClient.invalidateQueries({ queryKey: reportsKeys.all }),
  ]);
}

/** `POST /stock/adjustments`. */
export function useCreateStockAdjustment() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      body,
      idempotencyKey,
    }: {
      body: StockAdjustmentCreate;
      idempotencyKey: string;
    }) => createStockAdjustment(body, idempotencyKey),
    onSuccess: () => invalidateStockAndPurchases(queryClient),
  });
}
