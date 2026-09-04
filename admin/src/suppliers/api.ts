import type { components } from "@savdo/api-client";

import { api } from "../lib/api";
import { ApiError } from "../lib/errors";
import type { CursorPage } from "../lib/useCursorList";

export type Supplier = components["schemas"]["Supplier"];
export type SupplierCreate = components["schemas"]["SupplierCreate"];
export type SupplierPatch = components["schemas"]["SupplierPatch"];

/** Matches every other collection endpoint's default (`docs/05-API.md` §
 * Conventions). */
const PAGE_LIMIT = 50;

export interface SupplierListFilters {
  q?: string;
}

/** `GET /suppliers` — cursor-paginated, requires `suppliers.manage`
 * (manager+). */
export async function fetchSuppliersPage(
  filters: SupplierListFilters,
  cursor: string | null,
): Promise<CursorPage<Supplier>> {
  const { data, error } = await api.GET("/suppliers", {
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

/** `POST /suppliers` — requires `suppliers.manage`. `409 CONFLICT
 * details.field: "name"` on a duplicate name. */
export async function createSupplier(body: SupplierCreate): Promise<Supplier> {
  const { data, error } = await api.POST("/suppliers", { body });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `PATCH /suppliers/{id}` — partial update; `contactName`, `phone`,
 * `telegramUsername` and `note` are nullable (D-35): explicit `null` clears
 * the field. Requires `suppliers.manage`. */
export async function updateSupplier(id: string, body: SupplierPatch): Promise<Supplier> {
  const { data, error } = await api.PATCH("/suppliers/{id}", {
    params: { path: { id } },
    body,
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `DELETE /suppliers/{id}` — soft delete. Requires `suppliers.manage`. */
export async function deleteSupplier(id: string): Promise<void> {
  const { error } = await api.DELETE("/suppliers/{id}", {
    params: { path: { id } },
  });
  if (error) {
    throw new ApiError(error);
  }
}
