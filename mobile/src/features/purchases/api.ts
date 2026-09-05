import type { components } from "@savdo/api-client";

import type { CursorPage } from "@/features/catalog/api";
import { api } from "@/lib/api";

export type Purchase = components["schemas"]["Purchase"];
export type PurchaseCreate = components["schemas"]["PurchaseCreate"];
export type PurchaseItem = components["schemas"]["PurchaseItem"];
export type PurchaseItemCreate = components["schemas"]["PurchaseItemCreate"];
export type PurchaseStatus = components["schemas"]["PurchaseStatus"];
export type Supplier = components["schemas"]["Supplier"];
export type ErrorCode = components["schemas"]["ErrorCode"];

/** Matches every other collection endpoint's default (`docs/05-API.md` §
 * Conventions). */
const PAGE_LIMIT = 50;

/**
 * Thrown by every function in this module on a non-2xx response. Carries
 * the machine-readable `code` and `details` (ADR-013) — e.g.
 * `PURCHASE_ALREADY_RECEIVED`/`PURCHASE_ALREADY_CANCELLED` on a replayed
 * `receivePurchase`. Mirrors `features/stock/api.ts`'s `StockApiError`.
 */
export class PurchasesApiError extends Error {
  readonly code: ErrorCode;
  readonly details: Record<string, unknown>;

  constructor(code: ErrorCode, details?: Record<string, unknown>) {
    super(`purchases request failed: ${code}`);
    this.name = "PurchasesApiError";
    this.code = code;
    this.details = details ?? {};
  }
}

export interface ListSuppliersParams {
  q?: string;
  cursor?: string | null;
}

/**
 * A client-generated key for an idempotent write's `Idempotency-Key` header
 * (`docs/05-API.md` § Conventions) — see `features/stock/api.ts`'s
 * identical helper for why this doesn't call `crypto.randomUUID()`
 * unconditionally. Small, deliberate duplicate (no shared `lib` module
 * either feature's task scope covers).
 */
export function newIdempotencyKey(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}-${Math.random().toString(36).slice(2)}`;
}

/** `GET /suppliers` — requires `suppliers.manage`; granted to exactly the
 * same roles as `stock.write` (owner+manager, `api/internal/auth/permissions.go`),
 * so every screen that calls this is already gated on `stock.write`. */
export async function listSuppliers({
  q,
  cursor,
}: ListSuppliersParams): Promise<CursorPage<Supplier>> {
  const { data, error } = await api.GET("/suppliers", {
    params: { query: { limit: PAGE_LIMIT, cursor: cursor ?? undefined, q: q || undefined } },
  });
  if (error) {
    throw new PurchasesApiError(error.error.code, error.error.details);
  }
  return data;
}

/** Every supplier, not just one page — a small shop has a handful, same
 * approach as `features/catalog/api.ts`'s `listLocations`. Used to fill the
 * supplier picker and to resolve a purchase row's supplier name in the
 * drafts list. */
export async function listAllSuppliers(): Promise<Supplier[]> {
  const all: Supplier[] = [];
  let cursor: string | null = null;
  do {
    const page = await listSuppliers({ cursor });
    all.push(...page.items);
    cursor = page.nextCursor;
  } while (cursor);
  return all;
}

export interface ListPurchasesParams {
  status?: PurchaseStatus;
  cursor?: string | null;
}

/** `GET /purchases` — requires `stock.write`. Cursor-paginated. */
export async function listPurchases({
  status,
  cursor,
}: ListPurchasesParams): Promise<CursorPage<Purchase>> {
  const { data, error } = await api.GET("/purchases", {
    params: { query: { limit: PAGE_LIMIT, cursor: cursor ?? undefined, status } },
  });
  if (error) {
    throw new PurchasesApiError(error.error.code, error.error.details);
  }
  return data;
}

/** `GET /purchases/{id}` — a single purchase, including its items. */
export async function getPurchase(id: string): Promise<Purchase> {
  const { data, error } = await api.GET("/purchases/{id}", { params: { path: { id } } });
  if (error) {
    throw new PurchasesApiError(error.error.code, error.error.details);
  }
  return data;
}

/** `POST /purchases` — creates a draft; `number`/`totalCost` are
 * server-computed (D-45, hard rule 8). Requires `stock.write`. No
 * `Idempotency-Key` parameter on this operation (unlike `receivePurchase`)
 * — the generated client's `createPurchase` type has no `header` slot for
 * one at all, matching `admin/src/purchases/api.ts`'s `createPurchase`; a
 * duplicate tap is guarded client-side instead (disabling the submit
 * button while the mutation is in flight), not by a replay key. */
export async function createPurchase(body: PurchaseCreate): Promise<Purchase> {
  const { data, error } = await api.POST("/purchases", { body });
  if (error) {
    throw new PurchasesApiError(error.error.code, error.error.details);
  }
  return data;
}

/** `POST /purchases/{id}/receive` — writes one `purchase_in` stock movement
 * per item and updates each received variant's cost (D-42). Accepts
 * `Idempotency-Key`: a replay with the same key returns the original
 * result, so a caller can treat `409 PURCHASE_ALREADY_RECEIVED` for *this
 * key* the same as success (`features/purchases/hooks.ts`). Requires
 * `stock.write`. */
export async function receivePurchase(id: string, idempotencyKey: string): Promise<Purchase> {
  const { data, error } = await api.POST("/purchases/{id}/receive", {
    params: { path: { id }, header: { "Idempotency-Key": idempotencyKey } },
  });
  if (error) {
    throw new PurchasesApiError(error.error.code, error.error.details);
  }
  return data;
}
