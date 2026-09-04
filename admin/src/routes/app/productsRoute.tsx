import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { ProductsListPage } from "./ProductsListPage";

/** No `beforeLoad` guard — every role, including cashier, reads the product
 * list (role-shaped fields like `costPrice` stay hidden server-side and by
 * `ProductsListPage` checking their presence, ADR-010). */
export const productsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/products",
  component: ProductsListPage,
});
