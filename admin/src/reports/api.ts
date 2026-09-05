import type { components } from "@savdo/api-client";

import { api } from "../lib/api";
import { ApiError } from "../lib/errors";
import type { CursorPage } from "../lib/useCursorList";

export type SalesSummaryReport = components["schemas"]["SalesSummaryReport"];
export type SalesByProductRow = components["schemas"]["SalesByProductRow"];

/** Matches every other collection endpoint's default (`docs/05-API.md` §
 * Conventions). */
const PAGE_LIMIT = 50;

export interface SalesReportFilters {
  /** `YYYY-MM-DD`, inclusive. Required for manager+; ignored (send
   * `undefined`) for a cashier's "my day" view (D-55). */
  from?: string;
  /** `YYYY-MM-DD`, inclusive. See `from`. */
  to?: string;
  locationId?: string;
}

/**
 * `GET /reports/sales/summary` — `cashier+`. For manager+, pass `from`/`to`
 * (and optionally `locationId`); the response then carries `cost`/`margin`
 * (ADR-010). For a cashier, call with no filters — the server ignores
 * anything sent and always scopes to that cashier's own day, echoing the
 * effective `from`/`to`/`cashierId` it used (D-55).
 */
export async function fetchSalesSummaryReport(
  filters: SalesReportFilters = {},
): Promise<SalesSummaryReport> {
  const { data, error } = await api.GET("/reports/sales/summary", {
    params: {
      query: {
        from: filters.from,
        to: filters.to,
        locationId: filters.locationId,
      },
    },
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}

/**
 * `GET /reports/sales/by-product` — `manager+` only. Sorted by `revenue`
 * descending, server-side (never re-sorted client-side). `cost`/`margin`
 * per row are present for manager+ (ADR-010).
 */
export async function fetchSalesByProductPage(
  filters: Required<Pick<SalesReportFilters, "from" | "to">> &
    Pick<SalesReportFilters, "locationId">,
  cursor: string | null,
): Promise<CursorPage<SalesByProductRow>> {
  const { data, error } = await api.GET("/reports/sales/by-product", {
    params: {
      query: {
        from: filters.from,
        to: filters.to,
        locationId: filters.locationId,
        limit: PAGE_LIMIT,
        cursor: cursor ?? undefined,
      },
    },
  });
  if (error) {
    throw new ApiError(error);
  }
  return data;
}
