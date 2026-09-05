import type { components } from "@savdo/api-client";
import type { CursorPage } from "@/features/catalog/api";
import { api } from "@/lib/api";

export type Sale = components["schemas"]["Sale"];
export type SaleSummary = components["schemas"]["SaleSummary"];
export type SaleCreate = components["schemas"]["SaleCreate"];
export type SaleItemCreate = components["schemas"]["SaleItemCreate"];
export type SaleDiscount = components["schemas"]["SaleDiscount"];
export type SaleItem = components["schemas"]["SaleItem"];
export type PaymentMethod = components["schemas"]["PaymentMethod"];
export type DiscountType = components["schemas"]["DiscountType"];
export type ErrorCode = components["schemas"]["ErrorCode"];

/** Matches every other collection endpoint's default (`docs/05-API.md` §
 * Conventions). */
const PAGE_LIMIT = 50;

/**
 * Thrown by every function in this module on a non-2xx response. Carries
 * only the machine-readable `code` (ADR-013) plus whatever `details` the
 * error came with (e.g. `STOCK_INSUFFICIENT`'s `variantId`/`available`) —
 * a screen translates the code to a display sentence, never shows a raw
 * API message. Mirrors `features/catalog/api.ts`'s `CatalogApiError`.
 */
export class SalesApiError extends Error {
  readonly code: ErrorCode;
  readonly details: Record<string, unknown> | undefined;

  constructor(code: ErrorCode, details?: Record<string, unknown>) {
    super(`sales request failed: ${code}`);
    this.name = "SalesApiError";
    this.code = code;
    this.details = details;
  }
}

/** `POST /sales` — completes a quick sale in one transaction; line prices
 * always come from the catalogue, never the client (D-56); a manual
 * `discount`, if any, is capped at the subtotal (D-57, `409
 * DISCOUNT_EXCEEDS_SUBTOTAL` otherwise). `idempotencyKey` must be the
 * cart's own key (`features/sales/cart.ts`) so a retry of the same cart
 * never completes the sale twice (docs/05-API.md § Conventions). */
export async function createSale(body: SaleCreate, idempotencyKey: string): Promise<Sale> {
  const { data, error } = await api.POST("/sales", {
    params: { header: { "Idempotency-Key": idempotencyKey } },
    body,
  });
  if (error) {
    throw new SalesApiError(error.error.code, error.error.details as Record<string, unknown>);
  }
  return data;
}

export interface ListSalesParams {
  from?: string;
  to?: string;
  customerId?: string;
  cursor?: string | null;
}

/** `GET /sales` — cursor-paginated, newest first. Requires `cashier+`; a
 * cashier may list every sale for the whole shop and any day, not only
 * their own (D-63) — this module never filters by `cashierId` itself. */
export async function listSales(params: ListSalesParams): Promise<CursorPage<SaleSummary>> {
  const { data, error } = await api.GET("/sales", {
    params: {
      query: {
        limit: PAGE_LIMIT,
        cursor: params.cursor ?? undefined,
        from: params.from,
        to: params.to,
        customerId: params.customerId,
      },
    },
  });
  if (error) {
    throw new SalesApiError(error.error.code);
  }
  return data;
}

/** `GET /sales/{id}` — a single sale with its items; `unitCost` on each
 * item is present only for a caller with `cost.read` (manager+), absent
 * (not null) for a cashier (ADR-010, D-63) — this module never reads it. */
export async function getSale(id: string): Promise<Sale> {
  const { data, error } = await api.GET("/sales/{id}", {
    params: { path: { id } },
  });
  if (error) {
    throw new SalesApiError(error.error.code);
  }
  return data;
}
