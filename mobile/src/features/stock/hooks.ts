import { useInfiniteQuery, useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";

import { getProduct } from "@/features/catalog/api";
import { catalogKeys, stockKeys } from "@/lib/queryKeys";

import {
  createStockAdjustment,
  listLowStock,
  listStockLevels,
  listStockMovements,
  type StockAdjustmentCreate,
  type StockLowItem,
} from "./api";

/** A variant's stock levels across every location it has a `stock_levels`
 * row for (deliverable 3's variant movements screen also wants this to show
 * "current qty" alongside the ledger). Not cursor-paginated for the caller —
 * a variant has at most a handful of locations. */
export function useVariantStockLevels(variantId: string | undefined) {
  return useQuery({
    queryKey: stockKeys.levels({ variantId }),
    queryFn: async () => {
      const page = await listStockLevels({ variantId });
      return page.items;
    },
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
    const productsById = new Map(
      productIds.map((id, index) => [id, productQueries[index]?.data]),
    );
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

/** `POST /stock/adjustments` — invalidates every stock view a successful
 * adjustment can change (levels, low stock, and this variant's movement
 * ledger) so the tab reflects it without a manual pull-to-refresh. */
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
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["stock", "levels"] }),
        queryClient.invalidateQueries({ queryKey: ["stock", "low"] }),
        queryClient.invalidateQueries({ queryKey: ["stock", "movements"] }),
      ]);
    },
  });
}
