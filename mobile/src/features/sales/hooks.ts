import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { customersKeys, draftsKeys, reportsKeys, salesKeys } from "@/lib/queryKeys";

import {
  completeSaleDraft,
  createSale,
  createSaleDraft,
  deleteSaleDraft,
  getSale,
  getSaleDraft,
  type ListSaleDraftsParams,
  type ListSalesParams,
  listSaleDrafts,
  listSales,
  type SaleCreate,
  type SaleDraftComplete,
  type SaleDraftCreate,
  type SaleDraftPatch,
  updateSaleDraft,
} from "./api";

/** Completes a quick sale (`POST /sales`). Invalidates the sales list on
 * success so a freshly completed sale shows up in `sale/list.tsx`, plus —
 * when the sale carried a `customerId` — that customer's own detail key
 * (`customersKeys.detail`, the prefix `features/customers/hooks.ts`'s
 * `useCustomerSales` builds its `[...detail(id), "sales"]` key from), so
 * their purchase history (`customers/[id].tsx`) shows the new sale without
 * a manual pull-to-refresh (T4 review nit). Also invalidates every reports
 * query (`reportsKeys.all`) — a sale changes today's summary and
 * by-product totals, so without this the home screen's "today" card
 * (`app/(app)/index.tsx`) kept showing pre-sale numbers until a manual
 * pull-to-refresh (T11 device-smoke fix). */
export function useCreateSale() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ body, idempotencyKey }: { body: SaleCreate; idempotencyKey: string }) =>
      createSale(body, idempotencyKey),
    onSuccess: (_sale, variables) => {
      queryClient.invalidateQueries({ queryKey: ["sales"] });
      queryClient.invalidateQueries({ queryKey: reportsKeys.all });
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

/** Drafts, cursor-paginated, newest first (`GET /sales/drafts`), optionally
 * narrowed to `createdBy` (the drafts list's "mine" toggle). */
export function useDrafts(params: ListSaleDraftsParams) {
  return useInfiniteQuery({
    queryKey: draftsKeys.list({ createdBy: params.createdBy }),
    queryFn: ({ pageParam }) => listSaleDrafts({ ...params, cursor: pageParam }),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.nextCursor,
  });
}

/** A single draft (`GET /sales/drafts/{id}`), read-only. */
export function useDraft(id: string | undefined) {
  return useQuery({
    queryKey: draftsKeys.detail(id ?? ""),
    queryFn: () => getSaleDraft(id as string),
    enabled: id != null,
  });
}

/** Creates a draft (`POST /sales/drafts`); invalidates every drafts query
 * so it shows up in the list without a manual refresh. */
export function useCreateSaleDraft() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: SaleDraftCreate) => createSaleDraft(body),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: draftsKeys.all });
    },
  });
}

/** Updates a draft (`PATCH /sales/drafts/{id}`, creator or manager+,
 * D-89); invalidates every drafts query — both the list (its row summary
 * changed) and this draft's own detail. */
export function useUpdateSaleDraft() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, body }: { id: string; body: SaleDraftPatch }) => updateSaleDraft(id, body),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: draftsKeys.all });
    },
  });
}

/** Deletes a draft (`DELETE /sales/drafts/{id}`, creator or manager+,
 * D-89); invalidates every drafts query so it disappears from the list. */
export function useDeleteSaleDraft() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => deleteSaleDraft(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: draftsKeys.all });
    },
  });
}

/** Completes a draft (`POST /sales/drafts/{id}/complete`, any staff who can
 * create a sale — D-96); the draft is removed and a `Sale` is created in
 * one transaction, so this invalidates drafts (the completed one is gone),
 * `["sales"]` (the new sale should show up in every sales list) and every
 * report (`reportsKeys.all`, mirrors `useCreateSale`) — plus, when the
 * completed sale carried a customer, that customer's own detail key, same
 * as `useCreateSale`. */
export function useCompleteSaleDraft() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      body,
      idempotencyKey,
    }: {
      id: string;
      body: SaleDraftComplete;
      idempotencyKey: string;
    }) => completeSaleDraft(id, body, idempotencyKey),
    onSuccess: (sale) => {
      queryClient.invalidateQueries({ queryKey: draftsKeys.all });
      queryClient.invalidateQueries({ queryKey: ["sales"] });
      queryClient.invalidateQueries({ queryKey: reportsKeys.all });
      if (sale.customerId) {
        queryClient.invalidateQueries({ queryKey: customersKeys.detail(sale.customerId) });
      }
    },
  });
}
