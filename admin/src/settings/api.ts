import type { components } from "@savdo/api-client";

import { api } from "../lib/api";
import { ApiError } from "../lib/errors";

export type Shop = components["schemas"]["Shop"];
export type ShopPatch = components["schemas"]["ShopPatch"];

/** `GET /shop` — the current shop's settings. */
export async function fetchShop(): Promise<Shop> {
  const { data, error } = await api.GET("/shop");
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `PATCH /shop` — partial update (name, timezone, defaultLocale,
 * allowNegativeStock, updateCostOnPurchase). Owner only; `slug` and
 * `currency` are not patchable (shown read-only in the UI). */
export async function updateShop(body: ShopPatch): Promise<Shop> {
  const { data, error } = await api.PATCH("/shop", { body });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}
