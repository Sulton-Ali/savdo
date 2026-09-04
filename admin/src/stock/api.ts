import type { components } from "@savdo/api-client";

import { api } from "../lib/api";
import { ApiError } from "../lib/errors";
import type { CursorPage } from "../lib/useCursorList";
import { fetchLocationsPage, type Location } from "../locations/api";

export type StockLevel = components["schemas"]["StockLevel"];
export type StockMovement = components["schemas"]["StockMovement"];
export type StockMovementKind = components["schemas"]["StockMovementKind"];
export type AdjustmentReason = components["schemas"]["AdjustmentReason"];
export type StockAdjustmentCreate = components["schemas"]["StockAdjustmentCreate"];
export type StockTransferCreate = components["schemas"]["StockTransferCreate"];
export type StockLowItem = components["schemas"]["StockLowItem"];

/** Matches every other collection endpoint's default (`docs/05-API.md` §
 * Conventions). */
const PAGE_LIMIT = 50;

export interface StockLevelFilters {
  productId?: string;
  variantId?: string;
  locationId?: string;
}

/** `GET /stock/levels` — any authenticated role, cashier included (D-40). No
 * cost anywhere on `StockLevel` — it carries only `variantId`, `productId`,
 * `locationId` and `qty`; product/variant names come from the catalogue
 * endpoints, joined client-side (`StockLevelsPage`). */
export async function fetchStockLevelsPage(
  filters: StockLevelFilters,
  cursor: string | null,
): Promise<CursorPage<StockLevel>> {
  const { data, error } = await api.GET("/stock/levels", {
    params: {
      query: {
        limit: PAGE_LIMIT,
        cursor: cursor ?? undefined,
        productId: filters.productId,
        variantId: filters.variantId,
        locationId: filters.locationId,
      },
    },
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

export interface StockMovementFilters {
  variantId?: string;
  locationId?: string;
  kind?: StockMovementKind;
  from?: string;
  to?: string;
}

/** `GET /stock/movements` — requires `manager+` (`stock.write`). */
export async function fetchStockMovementsPage(
  filters: StockMovementFilters,
  cursor: string | null,
): Promise<CursorPage<StockMovement>> {
  const { data, error } = await api.GET("/stock/movements", {
    params: {
      query: {
        limit: PAGE_LIMIT,
        cursor: cursor ?? undefined,
        variantId: filters.variantId,
        locationId: filters.locationId,
        kind: filters.kind,
        from: filters.from,
        to: filters.to,
      },
    },
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `POST /stock/adjustments` — requires `manager+`. `idempotencyKey` should
 * be generated once (`crypto.randomUUID()`) when the drawer opens and
 * reused on every retry of the same submission (`docs/05-API.md` §
 * Conventions). */
export async function createStockAdjustment(
  body: StockAdjustmentCreate,
  idempotencyKey: string,
): Promise<StockMovement> {
  const { data, error } = await api.POST("/stock/adjustments", {
    params: { header: { "Idempotency-Key": idempotencyKey } },
    body,
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `POST /stock/transfers` — requires `manager+`. No `Idempotency-Key`
 * parameter on this operation (unlike adjustments — see the contract). */
export async function createStockTransfer(body: StockTransferCreate): Promise<StockMovement[]> {
  const { data, error } = await api.POST("/stock/transfers", { body });
  if (error) {
    throw new ApiError(error);
  }
  return data.items;
}

/** `GET /stock/low` — requires `manager+`. */
export async function fetchLowStockPage(cursor: string | null): Promise<CursorPage<StockLowItem>> {
  const { data, error } = await api.GET("/stock/low", {
    params: { query: { limit: PAGE_LIMIT, cursor: cursor ?? undefined } },
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** Every location, not just one page — the stock levels grid needs the full
 * set to build one column per location. Shops have a handful of locations
 * (stores/warehouses), so looping `GET /locations` to exhaustion here is
 * simpler than teaching the grid to grow columns as "Load more" pages in
 * (`locations/api.ts`'s `fetchLocationsPage` stays a plain cursor page —
 * every other caller uses it that way). */
export async function fetchAllLocations(): Promise<Location[]> {
  const all: Location[] = [];
  let cursor: string | null = null;
  do {
    const page: CursorPage<Location> = await fetchLocationsPage(cursor);
    all.push(...page.items);
    cursor = page.nextCursor;
  } while (cursor);
  return all;
}

/** Sums decimal quantity strings (`ADR-007`) for the levels grid's total
 * column, formatted back to the same 3-decimal-place wire precision
 * (`quantity NUMERIC(12,3)`, `docs/04-DATA-MODEL.md` § 8). */
export function sumQty(values: string[]): string {
  const total = values.reduce((sum, value) => sum + Number(value), 0);
  return total.toFixed(3);
}

/** Trims a decimal quantity string's trailing zeros for display (`"3.000"`
 * -> `"3"`, `"1.500"` -> `"1.5"`), falling back to `"0"` when absent. */
export function formatQty(value: string | undefined): string {
  if (value == null) {
    return "0";
  }
  const num = Number(value);
  if (!Number.isFinite(num)) {
    return value;
  }
  return String(num);
}
