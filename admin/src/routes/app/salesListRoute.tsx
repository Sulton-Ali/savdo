import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { SalesListPage } from "./SalesListPage";

/** No `beforeLoad` guard — every role, including cashier, may list every
 * sale (`docs/04-DATA-MODEL.md` § 7, D-63). Named `salesListRoute` (not
 * `salesRoute`) so it doesn't collide with the quick-sale create route,
 * shipped separately. */
export const salesListRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/sales",
  component: SalesListPage,
});
