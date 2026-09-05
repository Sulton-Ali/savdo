import type { components } from "@savdo/api-client";

import { api } from "@/lib/api";
import type { CursorPage } from "@/features/catalog/api";

export type StockLevel = components["schemas"]["StockLevel"];
export type StockMovement = components["schemas"]["StockMovement"];
export type StockMovementKind = components["schemas"]["StockMovementKind"];
export type AdjustmentReason = components["schemas"]["AdjustmentReason"];
export type StockAdjustmentCreate = components["schemas"]["StockAdjustmentCreate"];
export type StockLowItem = components["schemas"]["StockLowItem"];
export type ErrorCode = components["schemas"]["ErrorCode"];

/** Matches every other collection endpoint's default (`docs/05-API.md` §
 * Conventions). */
const PAGE_LIMIT = 50;

/**
 * Thrown by every function in this module on a non-2xx response. Carries
 * the machine-readable `code` and `details` (ADR-013) — `details.available`
 * on a `409 STOCK_INSUFFICIENT` is read by the adjustment screen to show
 * "only N available". Mirrors `admin/src/lib/errors.ts`'s `ApiError` and
 * `features/catalog/api.ts`'s `CatalogApiError` (which has no `details`
 * because nothing in that module needs one).
 */
export class StockApiError extends Error {
  readonly code: ErrorCode;
  readonly details: Record<string, unknown>;

  constructor(code: ErrorCode, details?: Record<string, unknown>) {
    super(`stock request failed: ${code}`);
    this.name = "StockApiError";
    this.code = code;
    this.details = details ?? {};
  }
}

export interface ListStockLevelsParams {
  variantId?: string;
  productId?: string;
  locationId?: string;
  cursor?: string | null;
}

/** `GET /stock/levels` — any authenticated role, cashier included (D-40). No
 * cost anywhere on `StockLevel`. */
export async function listStockLevels({
  variantId,
  productId,
  locationId,
  cursor,
}: ListStockLevelsParams): Promise<CursorPage<StockLevel>> {
  const { data, error } = await api.GET("/stock/levels", {
    params: {
      query: { limit: PAGE_LIMIT, cursor: cursor ?? undefined, variantId, productId, locationId },
    },
  });
  if (error) {
    throw new StockApiError(error.error.code, error.error.details);
  }
  return data;
}

export interface ListStockMovementsParams {
  variantId?: string;
  locationId?: string;
  cursor?: string | null;
}

/** `GET /stock/movements` — requires `stock.write` (manager+). Used by the
 * optional variant movements-history screen. */
export async function listStockMovements({
  variantId,
  locationId,
  cursor,
}: ListStockMovementsParams): Promise<CursorPage<StockMovement>> {
  const { data, error } = await api.GET("/stock/movements", {
    params: { query: { limit: PAGE_LIMIT, cursor: cursor ?? undefined, variantId, locationId } },
  });
  if (error) {
    throw new StockApiError(error.error.code, error.error.details);
  }
  return data;
}

/** `GET /stock/low` — requires `stock.write` (manager+; `docs/05-API.md` §
 * Stock lists it as `manager+`, not "every role" — the screen only fetches
 * this for a caller with `stock.write`, see `features/stock/hooks.ts`). */
export async function listLowStock(cursor: string | null): Promise<CursorPage<StockLowItem>> {
  const { data, error } = await api.GET("/stock/low", {
    params: { query: { limit: PAGE_LIMIT, cursor: cursor ?? undefined } },
  });
  if (error) {
    throw new StockApiError(error.error.code, error.error.details);
  }
  return data;
}

/**
 * A client-generated key for an idempotent write's `Idempotency-Key` header
 * (`docs/05-API.md` § Conventions) — the contract only requires an opaque
 * string up to 128 characters, not a UUID (`IdempotencyKey` parameter
 * schema), so this doesn't depend on `crypto.randomUUID()` being available
 * in Hermes (unverified on this RN/Expo version; no such polyfill is in the
 * D-78 dependency list) and falls back to a timestamp plus two random
 * suffixes when it isn't. Duplicated in `features/purchases/api.ts` — this
 * app has no shared `lib` module either file's task scope covers (small,
 * deliberate duplicate, same rationale as `features/catalog/qty.ts`'s
 * `formatQty`).
 */
export function newIdempotencyKey(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}-${Math.random().toString(36).slice(2)}`;
}

/** `POST /stock/adjustments` — requires `stock.write`. `idempotencyKey`
 * should be generated once when the form mounts and reused on every retry
 * of the same submission (`docs/05-API.md` § Conventions). */
export async function createStockAdjustment(
  body: StockAdjustmentCreate,
  idempotencyKey: string,
): Promise<StockMovement> {
  const { data, error } = await api.POST("/stock/adjustments", {
    params: { header: { "Idempotency-Key": idempotencyKey } },
    body,
  });
  if (error) {
    throw new StockApiError(error.error.code, error.error.details);
  }
  return data;
}
