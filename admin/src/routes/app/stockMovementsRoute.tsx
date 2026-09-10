import { createRoute } from "@tanstack/react-router";
import dayjs from "dayjs";
import customParseFormat from "dayjs/plugin/customParseFormat";

import type { StockMovementKind } from "../../stock/api";
import { authenticatedRoute } from "./authenticatedRoute";
import { requirePermission } from "./requirePermission";
import { MOVEMENT_KINDS, StockMovementsPage } from "./StockMovementsPage";

// Strict `YYYY-MM-DD` parsing for `dateOnlyOrUndefined` below — without this
// plugin `dayjs("2026-01-32")` falls back to a lenient native `Date` parse
// that accepts out-of-range days/months instead of rejecting them.
dayjs.extend(customParseFormat);

const DATE_ONLY_RE = /^\d{4}-\d{2}-\d{2}$/;

/** `/stock/movements` filter state, kept in the route's search params so
 * reload and browser back restore it (D-124). Only the final `variantId` is
 * a search param, not the product it belongs to — there is no
 * variant-to-product lookup endpoint, so `StockVariantPicker` cannot
 * re-resolve its product select from a bare `variantId` on reload; the
 * filter itself still applies correctly, but the picker's product field
 * starts empty until a product is searched again (a known gap, out of this
 * task's scope — flagged in the task report, not fixed here). */
export interface StockMovementsSearch {
  variantId?: string;
  locationId?: string;
  kind?: StockMovementKind;
  from?: string;
  to?: string;
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

function kindOrUndefined(value: unknown): StockMovementKind | undefined {
  return typeof value === "string" && (MOVEMENT_KINDS as string[]).includes(value)
    ? (value as StockMovementKind)
    : undefined;
}

/** Exported (not just inlined into `createRoute`) so the "invalid values
 * dropped" behaviour is directly unit-testable without going through a
 * `Router` instance — see `__tests__/stockMovementsRoute.test.tsx`. */
export function validateStockMovementsSearch(
  search: Record<string, unknown>,
): StockMovementsSearch {
  return {
    variantId: stringOrUndefined(search.variantId),
    locationId: stringOrUndefined(search.locationId),
    kind: kindOrUndefined(search.kind),
    from: dateOnlyOrUndefined(search.from),
    to: dateOnlyOrUndefined(search.to),
  };
}

export const stockMovementsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/stock/movements",
  beforeLoad: ({ context }) => requirePermission(context.me, "stock.write"),
  validateSearch: validateStockMovementsSearch,
  component: StockMovementsRouteComponent,
});

function StockMovementsRouteComponent() {
  const search = stockMovementsRoute.useSearch();
  const navigate = stockMovementsRoute.useNavigate();
  return (
    <StockMovementsPage search={search} onSearchChange={(next) => navigate({ search: next })} />
  );
}
