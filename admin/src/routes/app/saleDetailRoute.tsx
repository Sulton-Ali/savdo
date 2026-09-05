import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { SaleDetailPage } from "./SaleDetailPage";

/** No `beforeLoad` guard — every role, including cashier, may read a sale
 * (`docs/04-DATA-MODEL.md` § 7, D-63). Void/return stay gated inside the
 * page by `sales.void`. */
export const saleDetailRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/sales/$id",
  component: SaleDetailRouteComponent,
});

function SaleDetailRouteComponent() {
  const { id } = saleDetailRoute.useParams();
  return <SaleDetailPage saleId={id} />;
}
