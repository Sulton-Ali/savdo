import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { CustomersPage } from "./CustomersPage";

const MAX_Q_LENGTH = 200;

/** `/customers` filter state, kept in the route's search params so reload
 * and browser back restore it (D-124). */
export interface CustomersSearch {
  q?: string;
}

/** A non-empty trimmed string, capped at `MAX_Q_LENGTH` (truncated rather
 * than dropped — a long paste is still a usable prefix to search on); an
 * empty or whitespace-only value (or a non-string) is dropped. */
function qOrUndefined(value: unknown): string | undefined {
  if (typeof value !== "string") {
    return undefined;
  }
  const trimmed = value.trim();
  if (trimmed.length === 0) {
    return undefined;
  }
  return trimmed.length > MAX_Q_LENGTH ? trimmed.slice(0, MAX_Q_LENGTH) : trimmed;
}

/** Exported (not just inlined into `createRoute`) so the "invalid values
 * dropped" behaviour is directly unit-testable without going through a
 * `Router` instance — see `__tests__/customersRoute.test.tsx`. */
export function validateCustomersSearch(search: Record<string, unknown>): CustomersSearch {
  return { q: qOrUndefined(search.q) };
}

/** No `beforeLoad` guard — every role, including cashier, may list and
 * create customers (`docs/04-DATA-MODEL.md` § 7). Edit/delete controls stay
 * gated inside the page by `customers.write`. */
export const customersRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/customers",
  validateSearch: validateCustomersSearch,
  component: CustomersRouteComponent,
});

function CustomersRouteComponent() {
  const search = customersRoute.useSearch();
  const navigate = customersRoute.useNavigate();
  return (
    <CustomersPage
      search={search}
      // `replace: true` — a search-box tweak or Reset, not a page-to-page
      // move; pushing a history entry per keystroke would make browser Back
      // undo the search one character at a time instead of leaving the page.
      onSearchChange={(next) => navigate({ search: next, replace: true })}
    />
  );
}
