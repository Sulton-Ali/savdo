import type { components } from "@savdo/api-client";

import { api } from "../lib/api";
import { ApiError } from "../lib/errors";
import type { CursorPage } from "../lib/useCursorList";

export type Customer = components["schemas"]["Customer"];
export type CustomerCreate = components["schemas"]["CustomerCreate"];
export type CustomerPatch = components["schemas"]["CustomerPatch"];
export type SaleSummary = components["schemas"]["SaleSummary"];

/** Matches every other collection endpoint's default (`docs/05-API.md` §
 * Conventions). */
const PAGE_LIMIT = 50;

export interface CustomerListFilters {
  q?: string;
}

/** `GET /customers` — cursor-paginated, requires `cashier+` (every staff
 * role, `docs/04-DATA-MODEL.md` § 7). */
export async function fetchCustomersPage(
  filters: CustomerListFilters,
  cursor: string | null,
): Promise<CursorPage<Customer>> {
  const { data, error } = await api.GET("/customers", {
    params: {
      query: {
        limit: PAGE_LIMIT,
        cursor: cursor ?? undefined,
        q: filters.q || undefined,
      },
    },
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `POST /customers` — requires `cashier+`. `409 CONFLICT details.field:
 * "phone"` on a duplicate phone. */
export async function createCustomer(body: CustomerCreate): Promise<Customer> {
  const { data, error } = await api.POST("/customers", { body });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `GET /customers/{id}` — requires `cashier+`. */
export async function fetchCustomer(id: string): Promise<Customer> {
  const { data, error } = await api.GET("/customers/{id}", {
    params: { path: { id } },
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `PATCH /customers/{id}` — partial update; `phone`, `telegramUsername` and
 * `note` are nullable (D-35): explicit `null` clears the field. `tags`
 * replaces the full array when provided. Requires `manager+` (`customers.write`). */
export async function updateCustomer(id: string, body: CustomerPatch): Promise<Customer> {
  const { data, error } = await api.PATCH("/customers/{id}", {
    params: { path: { id } },
    body,
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `DELETE /customers/{id}` — soft delete. Requires `manager+`
 * (`customers.write`). */
export async function deleteCustomer(id: string): Promise<void> {
  const { error } = await api.DELETE("/customers/{id}", {
    params: { path: { id } },
  });
  if (error) {
    throw new ApiError(error);
  }
}

/** `GET /sales?customerId=` — a customer's purchase history, requires
 * `cashier+` (D-63: any cashier may list every sale, not only their own).
 * `unitCost`/margin never appear on `SaleSummary` regardless of caller. */
export async function fetchCustomerSalesPage(
  customerId: string,
  cursor: string | null,
): Promise<CursorPage<SaleSummary>> {
  const { data, error } = await api.GET("/sales", {
    params: {
      query: {
        customerId,
        limit: PAGE_LIMIT,
        cursor: cursor ?? undefined,
      },
    },
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}
