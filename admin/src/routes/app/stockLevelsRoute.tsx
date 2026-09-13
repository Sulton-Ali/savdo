import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { StockLevelsPage } from "./StockLevelsPage";

/** `/stock` filter state, kept in the route's search params so reload and
 * browser back restore it (D-124). `variantId` is intentionally not a
 * filter here (out of scope for this task) — only the product and
 * location. */
export interface StockLevelsSearch {
  productId?: string;
  locationId?: string;
}

function stringOrUndefined(value: unknown): string | undefined {
  return typeof value === "string" && value.length > 0 ? value : undefined;
}

/** Exported (not just inlined into `createRoute`) so the "invalid values
 * dropped" behaviour is directly unit-testable without going through a
 * `Router` instance — see `__tests__/stockLevelsRoute.test.tsx`. */
export function validateStockLevelsSearch(search: Record<string, unknown>): StockLevelsSearch {
  return {
    productId: stringOrUndefined(search.productId),
    locationId: stringOrUndefined(search.locationId),
  };
}

/** No `beforeLoad` guard — every role, including cashier, sees stock
 * quantities (D-40). Write actions (adjust/transfer) stay gated inside the
 * page by `stock.write`. */
export const stockLevelsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/stock",
  validateSearch: validateStockLevelsSearch,
  component: StockLevelsRouteComponent,
});

function StockLevelsRouteComponent() {
  const search = stockLevelsRoute.useSearch();
  const navigate = stockLevelsRoute.useNavigate();
  return (
    <StockLevelsPage
      search={search}
      // `replace: true` — every call here is a filter tweak or Reset, not a
      // page-to-page move; pushing a history entry per tweak would make
      // browser Back undo filters one at a time instead of leaving the page.
      onSearchChange={(next) => navigate({ search: next, replace: true })}
    />
  );
}
