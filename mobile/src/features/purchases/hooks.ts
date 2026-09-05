import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { invalidateStockAndPurchases } from "@/features/stock/hooks";
import { purchasesKeys } from "@/lib/queryKeys";

import {
  createPurchase,
  getPurchase,
  listAllSuppliers,
  listPurchases,
  type Purchase,
  type PurchaseCreate,
  type PurchaseStatus,
  PurchasesApiError,
  receivePurchase,
} from "./api";

/** Every supplier — a small shop's supplier list is short, same approach as
 * `features/catalog/hooks.ts`'s `useLocations`. */
export function useSuppliers() {
  return useQuery({
    queryKey: purchasesKeys.suppliers(),
    queryFn: listAllSuppliers,
  });
}

/** `GET /purchases`, cursor-paginated, optionally filtered by `status` (the
 * drafts list passes `"draft"`). */
export function usePurchases(status?: PurchaseStatus) {
  return useInfiniteQuery({
    queryKey: purchasesKeys.list({ status }),
    queryFn: ({ pageParam }) => listPurchases({ status, cursor: pageParam }),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.nextCursor,
  });
}

export function usePurchase(id: string | undefined) {
  return useQuery({
    queryKey: purchasesKeys.detail(id ?? ""),
    queryFn: () => getPurchase(id as string),
    enabled: id != null,
  });
}

function invalidatePurchases(queryClient: ReturnType<typeof useQueryClient>) {
  return queryClient.invalidateQueries({ queryKey: purchasesKeys.all });
}

/** `POST /purchases`. No `Idempotency-Key` on this operation (see
 * `features/purchases/api.ts`'s `createPurchase` doc) — the caller (the
 * "Save and receive" screen) guards a duplicate tap by disabling the button
 * while `isPending`. */
export function useCreatePurchase() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: PurchaseCreate) => createPurchase(body),
    onSuccess: () => invalidatePurchases(queryClient),
  });
}

/**
 * `POST /purchases/{id}/receive`. A `409 PURCHASE_ALREADY_RECEIVED` is
 * treated as success for display (task spec: "a replay of receive shows
 * already-received gracefully") — it means the purchase's stock has
 * already been written, whether by this same key's own earlier attempt, a
 * different key from a retry, or someone receiving the same web-created
 * draft first; either way the end state the caller wanted (received, with
 * its items' stock in) already holds, so this fetches the current purchase
 * and resolves with it instead of surfacing an error. Every other error
 * (network failure, `PURCHASE_ALREADY_CANCELLED`, `STOCK_INSUFFICIENT`,
 * ...) still rejects normally.
 */
export function useReceivePurchase() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({
      id,
      idempotencyKey,
    }: {
      id: string;
      idempotencyKey: string;
    }): Promise<Purchase> => {
      try {
        return await receivePurchase(id, idempotencyKey);
      } catch (error) {
        if (error instanceof PurchasesApiError && error.code === "PURCHASE_ALREADY_RECEIVED") {
          return getPurchase(id);
        }
        throw error;
      }
    },
    onSuccess: async (purchase) => {
      queryClient.setQueryData(purchasesKeys.detail(purchase.id), purchase);
      // Receiving writes `purchase_in` stock movements (D-42) — shared with
      // `features/stock/hooks.ts`'s `useCreateStockAdjustment`, which also
      // needs stock levels/low/movements invalidated after its own write.
      await invalidateStockAndPurchases(queryClient);
    },
  });
}
