import type { components } from "@savdo/api-client";

import { api } from "../lib/api";
import { ApiError } from "../lib/errors";
import type { CursorPage } from "../lib/useCursorList";

export type Purchase = components["schemas"]["Purchase"];
export type PurchaseCreate = components["schemas"]["PurchaseCreate"];
export type PurchasePatch = components["schemas"]["PurchasePatch"];
export type PurchaseItem = components["schemas"]["PurchaseItem"];
export type PurchaseItemCreate = components["schemas"]["PurchaseItemCreate"];
export type PurchaseStatus = components["schemas"]["PurchaseStatus"];

/** Matches every other collection endpoint's default (`docs/05-API.md` §
 * Conventions). */
const PAGE_LIMIT = 50;

export interface PurchaseListFilters {
  status?: PurchaseStatus;
  supplierId?: string;
}

/** `GET /purchases` — cursor-paginated, requires `stock.write` (manager+). */
export async function fetchPurchasesPage(
  filters: PurchaseListFilters,
  cursor: string | null,
): Promise<CursorPage<Purchase>> {
  const { data, error } = await api.GET("/purchases", {
    params: {
      query: {
        limit: PAGE_LIMIT,
        cursor: cursor ?? undefined,
        status: filters.status,
        supplierId: filters.supplierId,
      },
    },
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `GET /purchases/{id}` — a single purchase, including its items. */
export async function fetchPurchase(id: string): Promise<Purchase> {
  const { data, error } = await api.GET("/purchases/{id}", {
    params: { path: { id } },
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `POST /purchases` — creates a draft; `number` and `totalCost` are
 * server-computed and never accepted here (D-45, hard rule 8). Requires
 * `stock.write`. */
export async function createPurchase(body: PurchaseCreate): Promise<Purchase> {
  const { data, error } = await api.POST("/purchases", { body });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `PATCH /purchases/{id}` — only while `status: draft` (`409
 * PURCHASE_NOT_DRAFT` otherwise). `supplierInvoiceNo`/`note` are nullable
 * (D-35); `items`, when provided, replaces the full item list. Requires
 * `stock.write`. */
export async function updatePurchase(id: string, body: PurchasePatch): Promise<Purchase> {
  const { data, error } = await api.PATCH("/purchases/{id}", {
    params: { path: { id } },
    body,
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `POST /purchases/{id}/receive` — writes one `purchase_in` stock movement
 * per item and sets each received variant's cost to that line's `unitCost`
 * (D-42). Accepts `Idempotency-Key`: a replay with the same key returns the
 * original result. Requires `stock.write`. */
export async function receivePurchase(id: string, idempotencyKey: string): Promise<Purchase> {
  const { data, error } = await api.POST("/purchases/{id}/receive", {
    params: { path: { id }, header: { "Idempotency-Key": idempotencyKey } },
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `POST /purchases/{id}/cancel` — a plain status change for a `draft`
 * purchase; for a `received` purchase writes reversing stock movements,
 * which fail with `409 STOCK_INSUFFICIENT` if the stock was already sold or
 * moved below what the reversal needs (D-41). Requires `stock.write`. */
export async function cancelPurchase(id: string): Promise<Purchase> {
  const { data, error } = await api.POST("/purchases/{id}/cancel", {
    params: { path: { id } },
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}
