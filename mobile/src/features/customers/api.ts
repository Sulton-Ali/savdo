import type { components } from "@savdo/api-client";
import type { CursorPage } from "@/features/catalog/api";
import type { SaleSummary } from "@/features/sales/api";
import { api } from "@/lib/api";

export type Customer = components["schemas"]["Customer"];
export type CustomerCreate = components["schemas"]["CustomerCreate"];
export type ErrorCode = components["schemas"]["ErrorCode"];

/** Matches every other collection endpoint's default (`docs/05-API.md` §
 * Conventions). */
const PAGE_LIMIT = 50;

/** Thrown by every function in this module on a non-2xx response. Mirrors
 * `features/sales/api.ts`'s `SalesApiError`/`features/catalog/api.ts`'s
 * `CatalogApiError`. */
export class CustomersApiError extends Error {
  readonly code: ErrorCode;
  readonly details: Record<string, unknown> | undefined;

  constructor(code: ErrorCode, details?: Record<string, unknown>) {
    super(`customers request failed: ${code}`);
    this.name = "CustomersApiError";
    this.code = code;
    this.details = details;
  }
}

/** `GET /customers` — cursor-paginated, `q` searches `fullName` or `phone`.
 * Requires `cashier+` (every staff role). */
export async function searchCustomers(
  q: string,
  cursor: string | null = null,
): Promise<CursorPage<Customer>> {
  const { data, error } = await api.GET("/customers", {
    params: { query: { limit: PAGE_LIMIT, cursor: cursor ?? undefined, q: q || undefined } },
  });
  if (error) {
    throw new CustomersApiError(error.error.code);
  }
  return data;
}

/** `POST /customers` — requires `cashier+`. `409 CONFLICT
 * details.field: "phone"` on a duplicate phone. */
export async function createCustomer(body: CustomerCreate): Promise<Customer> {
  const { data, error } = await api.POST("/customers", { body });
  if (error) {
    throw new CustomersApiError(error.error.code, error.error.details as Record<string, unknown>);
  }
  return data;
}

/** `GET /customers/{id}` — requires `cashier+`. */
export async function getCustomer(id: string): Promise<Customer> {
  const { data, error } = await api.GET("/customers/{id}", {
    params: { path: { id } },
  });
  if (error) {
    throw new CustomersApiError(error.error.code);
  }
  return data;
}

/** `GET /sales?customerId=` — a customer's purchase history (D-63: any
 * cashier may list every sale, not only their own); no cost/margin field
 * ever appears on `SaleSummary` regardless of caller. */
export async function getCustomerSales(
  customerId: string,
  cursor: string | null = null,
): Promise<CursorPage<SaleSummary>> {
  const { data, error } = await api.GET("/sales", {
    params: { query: { customerId, limit: PAGE_LIMIT, cursor: cursor ?? undefined } },
  });
  if (error) {
    throw new CustomersApiError(error.error.code);
  }
  return data;
}
