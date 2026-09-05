import type { components } from "@savdo/api-client";

import { api } from "@/lib/api";

export type SalesSummaryReport = components["schemas"]["SalesSummaryReport"];
export type SalesByProductRow = components["schemas"]["SalesByProductRow"];
export type StockLowItem = components["schemas"]["StockLowItem"];
export type ErrorCode = components["schemas"]["ErrorCode"];

/** Matches every other collection endpoint's default (`docs/05-API.md` §
 * Conventions). */
const PAGE_LIMIT = 50;

export interface CursorPage<T> {
  items: T[];
  nextCursor: string | null;
}

/**
 * Thrown by every function in this module on a non-2xx response. Carries
 * only the machine-readable `code` (ADR-013) — callers translate it to a
 * display sentence via `@savdo/i18n`, never show a raw API message.
 * Mirrors `features/catalog/api.ts`'s `CatalogApiError`.
 */
export class ReportsApiError extends Error {
  readonly code: ErrorCode;

  constructor(code: ErrorCode) {
    super(`reports request failed: ${code}`);
    this.name = "ReportsApiError";
    this.code = code;
  }
}

export interface SalesSummaryFilters {
  /** `YYYY-MM-DD`, inclusive. Manager+ only — a cashier's own-day view
   * calls this with no filters at all (see below). */
  from?: string;
  to?: string;
}

/**
 * `GET /reports/sales/summary` — `cashier+`. For manager+, pass `from`/
 * `to`; the response then carries `cost`/`margin` (ADR-010). For a
 * cashier, call with no filters — the server ignores anything sent and
 * always scopes to that cashier's own day, echoing the effective
 * `from`/`to`/`cashierId` it used, and nets that cashier's own refunds
 * into `netRevenue` (D-55, D-71). Mirrors
 * `admin/src/reports/api.ts`'s `fetchSalesSummaryReport`.
 */
export async function fetchSalesSummary(
  filters: SalesSummaryFilters = {},
): Promise<SalesSummaryReport> {
  const { data, error } = await api.GET("/reports/sales/summary", {
    params: { query: { from: filters.from, to: filters.to } },
  });
  if (error) {
    throw new ReportsApiError(error.error.code);
  }
  return data;
}

export interface SalesByProductFilters {
  from: string;
  to: string;
}

/**
 * `GET /reports/sales/by-product` — `manager+` only. Sorted by `revenue`
 * descending, server-side. First page only: the home screen's "top
 * products" card shows the first 3 rows, well within one page.
 * `cost`/`margin` per row are present for manager+ (ADR-010).
 */
export async function fetchSalesByProduct(
  filters: SalesByProductFilters,
): Promise<CursorPage<SalesByProductRow>> {
  const { data, error } = await api.GET("/reports/sales/by-product", {
    params: {
      query: { from: filters.from, to: filters.to, limit: PAGE_LIMIT },
    },
  });
  if (error) {
    throw new ReportsApiError(error.error.code);
  }
  return data;
}

/**
 * `GET /stock/low` — requires `stock.write` (manager+, D-81). First page
 * only: the home screen shows a count with a "+" suffix when
 * `nextCursor` is non-null, same as
 * `admin/src/routes/app/ReportsPage.tsx`'s `LowStockCard`.
 */
export async function fetchLowStock(): Promise<CursorPage<StockLowItem>> {
  const { data, error } = await api.GET("/stock/low", {
    params: { query: { limit: PAGE_LIMIT } },
  });
  if (error) {
    throw new ReportsApiError(error.error.code);
  }
  return data;
}
