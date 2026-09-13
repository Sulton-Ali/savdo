import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { requirePermission } from "./requirePermission";
import { SuppliersPage } from "./SuppliersPage";

const MAX_Q_LENGTH = 200;

/** `/suppliers` filter state, kept in the route's search params so reload
 * and browser back restore it (D-124). */
export interface SuppliersSearch {
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
 * `Router` instance — see `__tests__/suppliersRoute.test.tsx`. */
export function validateSuppliersSearch(search: Record<string, unknown>): SuppliersSearch {
  return { q: qOrUndefined(search.q) };
}

export const suppliersRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/suppliers",
  beforeLoad: ({ context }) => requirePermission(context.me, "suppliers.manage"),
  validateSearch: validateSuppliersSearch,
  component: SuppliersRouteComponent,
});

function SuppliersRouteComponent() {
  const search = suppliersRoute.useSearch();
  const navigate = suppliersRoute.useNavigate();
  return (
    <SuppliersPage
      search={search}
      // `replace: true` — a search-box tweak or Reset, not a page-to-page
      // move; pushing a history entry per keystroke would make browser Back
      // undo the search one character at a time instead of leaving the page.
      onSearchChange={(next) => navigate({ search: next, replace: true })}
    />
  );
}
