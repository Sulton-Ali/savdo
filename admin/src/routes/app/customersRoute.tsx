import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { CustomersPage } from "./CustomersPage";

/** No `beforeLoad` guard — every role, including cashier, may list and
 * create customers (`docs/04-DATA-MODEL.md` § 7). Edit/delete controls stay
 * gated inside the page by `customers.write`. */
export const customersRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/customers",
  component: CustomersPage,
});
