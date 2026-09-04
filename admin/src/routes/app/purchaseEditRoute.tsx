import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { PurchaseFormPage } from "./PurchaseFormPage";
import { requirePermission } from "./requirePermission";

export const purchaseEditRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/purchases/$id",
  beforeLoad: ({ context }) => requirePermission(context.me, "stock.write"),
  component: PurchaseEditRouteComponent,
});

function PurchaseEditRouteComponent() {
  const { id } = purchaseEditRoute.useParams();
  return <PurchaseFormPage purchaseId={id} />;
}
