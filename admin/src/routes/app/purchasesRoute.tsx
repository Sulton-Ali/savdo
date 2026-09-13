import { createRoute } from "@tanstack/react-router";

import type { PurchaseStatus } from "../../purchases/api";
import { authenticatedRoute } from "./authenticatedRoute";
import { PURCHASE_STATUSES, PurchasesListPage } from "./PurchasesListPage";
import { requirePermission } from "./requirePermission";

/** `/purchases` filter state, kept in the route's search params so reload
 * and browser back restore it (D-124). */
export interface PurchasesSearch {
  status?: PurchaseStatus;
  supplierId?: string;
}

function stringOrUndefined(value: unknown): string | undefined {
  return typeof value === "string" && value.length > 0 ? value : undefined;
}

function statusOrUndefined(value: unknown): PurchaseStatus | undefined {
  return typeof value === "string" && (PURCHASE_STATUSES as string[]).includes(value)
    ? (value as PurchaseStatus)
    : undefined;
}

/** Exported (not just inlined into `createRoute`) so the "invalid values
 * dropped" behaviour is directly unit-testable without going through a
 * `Router` instance — see `__tests__/purchasesRoute.test.tsx`. */
export function validatePurchasesSearch(search: Record<string, unknown>): PurchasesSearch {
  return {
    status: statusOrUndefined(search.status),
    supplierId: stringOrUndefined(search.supplierId),
  };
}

export const purchasesRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/purchases",
  beforeLoad: ({ context }) => requirePermission(context.me, "stock.write"),
  validateSearch: validatePurchasesSearch,
  component: PurchasesRouteComponent,
});

function PurchasesRouteComponent() {
  const search = purchasesRoute.useSearch();
  const navigate = purchasesRoute.useNavigate();
  return (
    <PurchasesListPage
      search={search}
      // `replace: true` — every call here is a filter tweak or Reset, not a
      // page-to-page move; pushing a history entry per tweak would make
      // browser Back undo filters one at a time instead of leaving the
      // page.
      onSearchChange={(next) => navigate({ search: next, replace: true })}
    />
  );
}
