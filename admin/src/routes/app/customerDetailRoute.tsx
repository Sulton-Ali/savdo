import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { CustomerDetailPage } from "./CustomerDetailPage";

/** No `beforeLoad` guard — every role, including cashier, may read a
 * customer and their purchase history (`docs/04-DATA-MODEL.md` § 7,
 * D-63). */
export const customerDetailRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/customers/$id",
  component: CustomerDetailRouteComponent,
});

function CustomerDetailRouteComponent() {
  const { id } = customerDetailRoute.useParams();
  return <CustomerDetailPage customerId={id} />;
}
