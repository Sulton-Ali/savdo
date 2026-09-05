import { useQuery } from "@tanstack/react-query";

import { reportsKeys } from "@/lib/queryKeys";

import {
  fetchLowStock,
  fetchSalesByProduct,
  fetchSalesSummary,
  type SalesByProductFilters,
  type SalesSummaryFilters,
} from "./api";

/**
 * The sales summary for the home screen's card (deliverable 1). Manager+
 * passes the period's `from`/`to`; a cashier passes `{}` and the server
 * scopes it to their own day regardless (D-55, D-71) — always enabled,
 * since every role can call this endpoint.
 */
export function useSalesSummary(filters: SalesSummaryFilters) {
  return useQuery({
    queryKey: reportsKeys.summary(filters),
    queryFn: () => fetchSalesSummary(filters),
  });
}

/** Top products for the period (`manager+` only) — `enabled` gates the
 * request itself, not just the rendering, so a cashier's client never
 * calls an endpoint it has no permission for. */
export function useSalesByProduct(filters: SalesByProductFilters, enabled: boolean) {
  return useQuery({
    queryKey: reportsKeys.byProduct(filters),
    queryFn: () => fetchSalesByProduct(filters),
    enabled,
  });
}

/** Low-stock count for the home screen's shortcut line (`stock.write`
 * only, D-81) — `enabled` gates the request the same way. */
export function useLowStock(enabled: boolean) {
  return useQuery({
    queryKey: reportsKeys.lowStock(),
    queryFn: fetchLowStock,
    enabled,
  });
}
