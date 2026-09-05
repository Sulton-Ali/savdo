import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { customersKeys } from "@/lib/queryKeys";

import {
  type CustomerCreate,
  createCustomer,
  getCustomer,
  getCustomerSales,
  searchCustomers,
} from "./api";

/** Customers matching `q` (name or phone), cursor-paginated (`GET
 * /customers`). */
export function useCustomersSearch(q: string) {
  return useInfiniteQuery({
    queryKey: customersKeys.list({ q }),
    queryFn: ({ pageParam }) => searchCustomers(q, pageParam),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.nextCursor,
  });
}

/** A single customer (`GET /customers/{id}`). */
export function useCustomer(id: string | undefined) {
  return useQuery({
    queryKey: customersKeys.detail(id ?? ""),
    queryFn: () => getCustomer(id as string),
    enabled: id != null,
  });
}

/** A customer's purchase history (`GET /sales?customerId=`), cursor-
 * paginated, newest first. */
export function useCustomerSales(customerId: string | undefined) {
  return useInfiniteQuery({
    queryKey: [...customersKeys.detail(customerId ?? ""), "sales"] as const,
    queryFn: ({ pageParam }) => getCustomerSales(customerId as string, pageParam),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.nextCursor,
    enabled: customerId != null,
  });
}

/** Creates a customer (`POST /customers`); invalidates the customers list
 * so a freshly created customer appears without a manual refresh. */
export function useCreateCustomer() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: CustomerCreate) => createCustomer(body),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["customers"] });
    },
  });
}
