import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { ProductsListPage } from "./ProductsListPage";

const MAX_Q_LENGTH = 200;

/** `/products` filter state, kept in the route's search params so reload
 * and browser back restore it (D-124). `includeInactive` only has an
 * effect for a user with `catalog.write` — `ProductsListPage` never
 * forwards it to the API otherwise, even if present in the URL. */
export interface ProductsSearch {
  q?: string;
  categoryId?: string;
  includeInactive?: boolean;
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

function stringOrUndefined(value: unknown): string | undefined {
  return typeof value === "string" && value.length > 0 ? value : undefined;
}

/** Accepts a real boolean or its `"true"`/`"false"` string form (how it
 * round-trips through a URL query param); anything else is dropped. */
function booleanOrUndefined(value: unknown): boolean | undefined {
  if (typeof value === "boolean") {
    return value;
  }
  if (value === "true") {
    return true;
  }
  if (value === "false") {
    return false;
  }
  return undefined;
}

/** Exported (not just inlined into `createRoute`) so the "invalid values
 * dropped" behaviour is directly unit-testable without going through a
 * `Router` instance — see `__tests__/productsRoute.test.tsx`. */
export function validateProductsSearch(search: Record<string, unknown>): ProductsSearch {
  return {
    q: qOrUndefined(search.q),
    categoryId: stringOrUndefined(search.categoryId),
    includeInactive: booleanOrUndefined(search.includeInactive),
  };
}

/** No `beforeLoad` guard — every role, including cashier, reads the product
 * list (role-shaped fields like `costPrice` stay hidden server-side and by
 * `ProductsListPage` checking their presence, ADR-010). */
export const productsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/products",
  validateSearch: validateProductsSearch,
  component: ProductsRouteComponent,
});

function ProductsRouteComponent() {
  const search = productsRoute.useSearch();
  const navigate = productsRoute.useNavigate();
  return (
    <ProductsListPage
      search={search}
      // `replace: true` — every call here is a filter tweak or Reset, not a
      // page-to-page move; pushing a history entry per tweak would make
      // browser Back undo filters one at a time instead of leaving the page.
      onSearchChange={(next) => navigate({ search: next, replace: true })}
    />
  );
}
