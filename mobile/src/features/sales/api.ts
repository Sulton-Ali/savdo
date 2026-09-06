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

/** A mutable, shared, unpaid order (D-87..D-90, T14) — see this type's own
 * doc comment in `schema.d.ts` for the full shape/reasoning. */
export type SaleDraft = components["schemas"]["SaleDraft"];
export type SaleDraftItem = components["schemas"]["SaleDraftItem"];
export type SaleDraftCreate = components["schemas"]["SaleDraftCreate"];
export type SaleDraftPatch = components["schemas"]["SaleDraftPatch"];
export type SaleDraftComplete = components["schemas"]["SaleDraftComplete"];

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

export interface ListSaleDraftsParams {
  createdBy?: string;
  cursor?: string | null;
}

/** `GET /sales/drafts` — cursor-paginated, newest first; `cashier+` may
 * list every draft in the shop, optionally narrowed to `createdBy` ("mine"
 * toggle) — not scoped to the caller by default (D-87: a draft is shared
 * across staff and devices). */
export async function listSaleDrafts(params: ListSaleDraftsParams): Promise<CursorPage<SaleDraft>> {
  const { data, error } = await api.GET("/sales/drafts", {
    params: {
      query: {
        limit: PAGE_LIMIT,
        cursor: params.cursor ?? undefined,
        createdBy: params.createdBy,
      },
    },
  });
  if (error) {
    throw new SalesApiError(error.error.code);
  }
  return data;
}

/** `GET /sales/drafts/{id}` — requires `cashier+`; any staff may view any
 * draft (D-87). `404 NOT_FOUND` once the draft has been completed or
 * deleted by anyone else. */
export async function getSaleDraft(id: string): Promise<SaleDraft> {
  const { data, error } = await api.GET("/sales/drafts/{id}", {
    params: { path: { id } },
  });
  if (error) {
    throw new SalesApiError(error.error.code);
  }
  return data;
}

/** `POST /sales/drafts` — requires `cashier+`. `items` carries only
 * `variantId`/`qty`, never a price (D-56/D-87). `409
 * DISCOUNT_EXCEEDS_SUBTOTAL` when `discount` exceeds the computed
 * subtotal (D-57). No stock reservation happens here (D-88). */
export async function createSaleDraft(body: SaleDraftCreate): Promise<SaleDraft> {
  const { data, error } = await api.POST("/sales/drafts", { body });
  if (error) {
    throw new SalesApiError(error.error.code, error.error.details as Record<string, unknown>);
  }
  return data;
}

/** `PATCH /sales/drafts/{id}` — requires the draft's own creator or
 * `manager+` (D-89, `403 FORBIDDEN` otherwise). Partial update; `items`,
 * when present, replaces the whole line set; `customerId`/`discountType`/
 * `discountValue`/`discountReason`/`note` are nullable (D-35): explicit
 * `null` clears the field. */
export async function updateSaleDraft(id: string, body: SaleDraftPatch): Promise<SaleDraft> {
  const { data, error } = await api.PATCH("/sales/drafts/{id}", {
    params: { path: { id } },
    body,
  });
  if (error) {
    throw new SalesApiError(error.error.code, error.error.details as Record<string, unknown>);
  }
  return data;
}

/** `DELETE /sales/drafts/{id}` — requires the draft's own creator or
 * `manager+` (D-89, `403 FORBIDDEN` otherwise); hard-deletes, no ledger
 * effect (D-89, drafts never touch stock — D-88). */
export async function deleteSaleDraft(id: string): Promise<void> {
  const { error } = await api.DELETE("/sales/drafts/{id}", {
    params: { path: { id } },
  });
  if (error) {
    throw new SalesApiError(error.error.code);
  }
}

/** `POST /sales/drafts/{id}/complete` — any staff who can create a sale
 * may complete any draft, regardless of who created it (D-96); creates the
 * `Sale` and deletes the draft in one transaction (D-87). `idempotencyKey`
 * follows the exact same lifecycle `createSale`'s doc comment describes
 * (`features/sales/cart.ts`'s `idempotencyKey`/`idempotencyOutcome`): minted
 * once per attempt series, kept on a network error or any other decoded
 * failure, re-minted only for `409 IDEMPOTENCY_KEY_REUSED`. `409
 * STOCK_INSUFFICIENT` when a line can no longer be fulfilled (D-88); `422
 * VALIDATION_FAILED details.fields["items[i].variantId"]` when a line has
 * gone unavailable since it was added (`features/sales/drafts.ts`'s
 * `parseUnavailableLineIndexes` reads this shape). */
export async function completeSaleDraft(
  id: string,
  body: SaleDraftComplete,
  idempotencyKey: string,
): Promise<Sale> {
  const { data, error } = await api.POST("/sales/drafts/{id}/complete", {
    params: { path: { id }, header: { "Idempotency-Key": idempotencyKey } },
    body,
  });
  if (error) {
    throw new SalesApiError(error.error.code, error.error.details as Record<string, unknown>);
  }
  return data;
}
