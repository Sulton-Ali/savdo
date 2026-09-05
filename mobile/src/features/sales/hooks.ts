import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { customersKeys, salesKeys } from "@/lib/queryKeys";

import { createSale, getSale, type ListSalesParams, listSales, type SaleCreate } from "./api";

/** Completes a quick sale (`POST /sales`). Invalidates the sales list on
 * success so a freshly completed sale shows up in `sale/list.tsx`, plus —
 * when the sale carried a `customerId` — that customer's own detail key
 * (`customersKeys.detail`, the prefix `features/customers/hooks.ts`'s
 * `useCustomerSales` builds its `[...detail(id), "sales"]` key from), so
 * their purchase history (`customers/[id].tsx`) shows the new sale without
 * a manual pull-to-refresh (T4 review nit). */
export function useCreateSale() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ body, idempotencyKey }: { body: SaleCreate; idempotencyKey: string }) =>
      createSale(body, idempotencyKey),
    onSuccess: (_sale, variables) => {
      queryClient.invalidateQueries({ queryKey: ["sales"] });
      if (variables.body.customerId) {
        queryClient.invalidateQueries({
          queryKey: customersKeys.detail(variables.body.customerId),
        });
      }
    },
  });
}

/** Today's (or any `from`/`to` range's) sales, cursor-paginated, newest
 * first (`GET /sales`). */
export function useSales(params: Omit<ListSalesParams, "cursor">) {
  return useInfiniteQuery({
    queryKey: salesKeys.list(params),
    queryFn: ({ pageParam }) => listSales({ ...params, cursor: pageParam }),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.nextCursor,
  });
}

/** A single sale with its items (`GET /sales/{id}`), read-only. */
export function useSale(id: string | undefined) {
  return useQuery({
    queryKey: salesKeys.detail(id ?? ""),
    queryFn: () => getSale(id as string),
    enabled: id != null,
  });
}
