import type { components } from "@savdo/api-client";

import { api } from "../lib/api";
import { ApiError } from "../lib/errors";
import type { CursorPage } from "../lib/useCursorList";

export type Sale = components["schemas"]["Sale"];
export type SaleCreate = components["schemas"]["SaleCreate"];
export type SaleItemCreate = components["schemas"]["SaleItemCreate"];
export type SaleDiscount = components["schemas"]["SaleDiscount"];
export type SaleSummary = components["schemas"]["SaleSummary"];
export type SaleItem = components["schemas"]["SaleItem"];
export type SaleKind = components["schemas"]["SaleKind"];
export type SaleStatus = components["schemas"]["SaleStatus"];
export type PaymentMethod = components["schemas"]["PaymentMethod"];
export type DiscountType = components["schemas"]["DiscountType"];
export type SaleVoid = components["schemas"]["SaleVoid"];
export type SaleReturnCreate = components["schemas"]["SaleReturnCreate"];
export type SaleReturnItemCreate = components["schemas"]["SaleReturnItemCreate"];

/** `POST /sales` — completes a quick sale in one transaction: line prices
 * always come from the catalogue, never the client (D-56); the manual
 * `discount`, if any, is capped at the subtotal (D-57, `409
 * DISCOUNT_EXCEEDS_SUBTOTAL` otherwise). Requires `cashier+`.
 * `idempotencyKey` should be generated once (`crypto.randomUUID()`) when
 * the cart is created and reused on every retry of the same cart
 * (`docs/05-API.md` § Conventions), so a duplicate click or a retried
 * network failure never completes the sale twice. */
export async function createSale(body: SaleCreate, idempotencyKey: string): Promise<Sale> {
  const { data, error } = await api.POST("/sales", {
    params: { header: { "Idempotency-Key": idempotencyKey } },
    body,
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** Matches every other collection endpoint's default (`docs/05-API.md` §
 * Conventions). */
const PAGE_LIMIT = 50;

export interface SaleListFilters {
  /** Inclusive `YYYY-MM-DD`, shop-timezone calendar day (`docs/05-API.md`). */
  from?: string;
  to?: string;
  locationId?: string;
  cashierId?: string;
  customerId?: string;
  kind?: SaleKind;
  status?: SaleStatus;
}

/** `GET /sales` — cursor-paginated, newest first, requires `cashier+` (a
 * cashier may list every sale, any day, not only their own, D-63). */
export async function fetchSalesPage(
  filters: SaleListFilters,
  cursor: string | null,
): Promise<CursorPage<SaleSummary>> {
  const { data, error } = await api.GET("/sales", {
    params: {
      query: {
        limit: PAGE_LIMIT,
        cursor: cursor ?? undefined,
        from: filters.from,
        to: filters.to,
        locationId: filters.locationId,
        cashierId: filters.cashierId,
        customerId: filters.customerId,
        kind: filters.kind,
        status: filters.status,
      },
    },
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `GET /sales/{id}` — a single sale with its items; `unitCost` on each item
 * is present only for a caller with `cost.read` (manager+, absent — not
 * null — for a cashier, ADR-010, D-63). */
export async function fetchSale(id: string): Promise<Sale> {
  const { data, error } = await api.GET("/sales/{id}", {
    params: { path: { id } },
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `POST /sales/{id}/void` — requires `manager+`. Only on the sale's own
 * calendar day (`409 SALE_VOID_WINDOW_CLOSED`, D-59) and only while it has
 * no return referencing it (`409 SALE_HAS_RETURNS`, D-62); `409
 * SALE_ALREADY_VOIDED` otherwise. */
export async function voidSale(id: string, body: SaleVoid): Promise<Sale> {
  const { data, error } = await api.POST("/sales/{id}/void", {
    params: { path: { id } },
    body,
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `POST /sales/{id}/return` — requires `manager+`. Per line, the returned
 * qty may not exceed sold minus already returned (`409
 * RETURN_EXCEEDS_SOLD details.saleItemId`, D-58). Accepts `Idempotency-Key`
 * (docs/05-API.md § Conventions): a replay with the same key returns the
 * original result; a different body must use a different key or the server
 * answers `409 IDEMPOTENCY_KEY_REUSED`. The response is a new sale of
 * `kind: return`. */
export async function createSaleReturn(
  id: string,
  body: SaleReturnCreate,
  idempotencyKey: string,
): Promise<Sale> {
  const { data, error } = await api.POST("/sales/{id}/return", {
    params: { path: { id }, header: { "Idempotency-Key": idempotencyKey } },
    body,
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}
