import type { components } from "@savdo/api-client";

import { api } from "../lib/api";
import { ApiError } from "../lib/errors";

export type Sale = components["schemas"]["Sale"];
export type SaleCreate = components["schemas"]["SaleCreate"];
export type SaleItemCreate = components["schemas"]["SaleItemCreate"];
export type SaleDiscount = components["schemas"]["SaleDiscount"];
export type PaymentMethod = components["schemas"]["PaymentMethod"];
export type DiscountType = components["schemas"]["DiscountType"];

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
