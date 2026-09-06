import type { components } from "@savdo/api-client";

import { api } from "../lib/api";
import { ApiError } from "../lib/errors";
import type { CursorPage } from "../lib/useCursorList";
import type { Sale } from "./api";

export type SaleDraft = components["schemas"]["SaleDraft"];
export type SaleDraftItem = components["schemas"]["SaleDraftItem"];
export type SaleDraftPatch = components["schemas"]["SaleDraftPatch"];
export type SaleDraftComplete = components["schemas"]["SaleDraftComplete"];

/** Matches every other collection endpoint's default (`docs/05-API.md` §
 * Conventions). */
const PAGE_LIMIT = 50;

export interface SaleDraftListFilters {
  /** Restricts the list to one creator (the "mine" filter) — `undefined`
   * lists every draft in the shop (D-87: drafts are shared). */
  createdBy?: string;
}

/** `GET /sales/drafts` — cursor-paginated, newest first, requires
 * `cashier+`: any staff who may create a sale can list every draft, shared
 * across devices (D-87). */
export async function fetchSaleDraftsPage(
  filters: SaleDraftListFilters,
  cursor: string | null,
): Promise<CursorPage<SaleDraft>> {
  const { data, error } = await api.GET("/sales/drafts", {
    params: {
      query: {
        limit: PAGE_LIMIT,
        cursor: cursor ?? undefined,
        createdBy: filters.createdBy,
      },
    },
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `GET /sales/drafts/{id}` — requires `cashier+`. Prices and totals are
 * computed server-side from the catalogue's current state on every read
 * (D-67, D-87), never stored. */
export async function fetchSaleDraft(id: string): Promise<SaleDraft> {
  const { data, error } = await api.GET("/sales/drafts/{id}", {
    params: { path: { id } },
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `PATCH /sales/drafts/{id}` — requires `cashier+`, and either the draft's
 * own creator or `manager+` (D-89). `items` is out of scope for this page —
 * line editing is not built here — so this only ever sends `note`,
 * `discountType`/`discountValue` and `customerId`. */
export async function updateSaleDraft(id: string, body: SaleDraftPatch): Promise<SaleDraft> {
  const { data, error } = await api.PATCH("/sales/drafts/{id}", {
    params: { path: { id } },
    body,
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/** `DELETE /sales/drafts/{id}` — requires `cashier+`, and either the
 * draft's own creator or `manager+` (D-89). Hard delete, no ledger effect. */
export async function deleteSaleDraft(id: string): Promise<void> {
  const { error } = await api.DELETE("/sales/drafts/{id}", {
    params: { path: { id } },
  });
  if (error) {
    throw new ApiError(error);
  }
}

/** `POST /sales/drafts/{id}/complete` — requires `cashier+`: any staff who
 * may create a sale, regardless of the draft's own creator (D-96). Accepts
 * `Idempotency-Key` with the same optional, API-wide semantics as
 * `POST /sales` (D-97): a replay with the same key returns the original
 * result; a different body under the same key answers `409
 * IDEMPOTENCY_KEY_REUSED`. The response is the newly completed `Sale`. */
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
    throw new ApiError(error);
  }
  return data;
}
