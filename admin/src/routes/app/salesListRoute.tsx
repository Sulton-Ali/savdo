import { createRoute } from "@tanstack/react-router";
import dayjs from "dayjs";
import customParseFormat from "dayjs/plugin/customParseFormat";

import type { SaleKind, SaleStatus } from "../../sales/api";
import { authenticatedRoute } from "./authenticatedRoute";
import { SALE_KINDS, SALE_STATUSES, SalesListPage } from "./SalesListPage";

// Strict `YYYY-MM-DD` parsing for `dateOnlyOrUndefined` below — mirrors
// `stockMovementsRoute` (without this plugin `dayjs("2026-01-32")` falls
// back to a lenient native `Date` parse that accepts out-of-range
// days/months instead of rejecting them).
dayjs.extend(customParseFormat);

const DATE_ONLY_RE = /^\d{4}-\d{2}-\d{2}$/;

/** `/sales` filter state, kept in the route's search params so reload and
 * browser back restore it (D-124). `cashierId` is an owner-only filter
 * (`GET /staff` is owner-only, `docs/05-API.md`) — a non-owner's stale or
 * hand-crafted `?cashierId=` still parses here, but `SalesListPage` never
 * forwards it to the API for a non-owner caller. */
export interface SalesListSearch {
  from?: string;
  to?: string;
  locationId?: string;
  kind?: SaleKind;
  status?: SaleStatus;
  cashierId?: string;
  customerId?: string;
}

function stringOrUndefined(value: unknown): string | undefined {
  return typeof value === "string" && value.length > 0 ? value : undefined;
}

/** Accepts only a real calendar date in `YYYY-MM-DD` shape — the regex
 * rejects malformed input cheaply, `dayjs(..., true)` (strict parsing)
 * catches shape-valid but nonexistent dates like `2026-01-32`. */
function dateOnlyOrUndefined(value: unknown): string | undefined {
  return typeof value === "string" &&
    DATE_ONLY_RE.test(value) &&
    dayjs(value, "YYYY-MM-DD", true).isValid()
    ? value
    : undefined;
}

function kindOrUndefined(value: unknown): SaleKind | undefined {
  return typeof value === "string" && (SALE_KINDS as string[]).includes(value)
    ? (value as SaleKind)
    : undefined;
}

function statusOrUndefined(value: unknown): SaleStatus | undefined {
  return typeof value === "string" && (SALE_STATUSES as string[]).includes(value)
    ? (value as SaleStatus)
    : undefined;
}

/** Exported (not just inlined into `createRoute`) so the "invalid values
 * dropped" behaviour is directly unit-testable without going through a
 * `Router` instance — see `__tests__/salesListRoute.test.tsx`. */
export function validateSalesListSearch(search: Record<string, unknown>): SalesListSearch {
  return {
    from: dateOnlyOrUndefined(search.from),
    to: dateOnlyOrUndefined(search.to),
    locationId: stringOrUndefined(search.locationId),
    kind: kindOrUndefined(search.kind),
    status: statusOrUndefined(search.status),
    cashierId: stringOrUndefined(search.cashierId),
    customerId: stringOrUndefined(search.customerId),
  };
}

/** No `beforeLoad` guard — every role, including cashier, may list every
 * sale (`docs/04-DATA-MODEL.md` § 7, D-63). Named `salesListRoute` (not
 * `salesRoute`) so it doesn't collide with the quick-sale create route,
 * shipped separately. */
export const salesListRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/sales",
  validateSearch: validateSalesListSearch,
  component: SalesListRouteComponent,
});

function SalesListRouteComponent() {
  const search = salesListRoute.useSearch();
  const navigate = salesListRoute.useNavigate();
  return (
    <SalesListPage
      search={search}
      // `replace: true` — every call here is a filter tweak or Reset, not a
      // page-to-page move; pushing a history entry per tweak would make
      // browser Back undo filters one at a time instead of leaving the
      // page. The URL still carries the full filter state for reload/
      // share/forward-back between pages.
      onSearchChange={(next) => navigate({ search: next, replace: true })}
    />
  );
}
