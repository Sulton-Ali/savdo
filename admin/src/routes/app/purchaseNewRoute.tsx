import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { PurchaseFormPage } from "./PurchaseFormPage";
import { requirePermission } from "./requirePermission";

export const purchaseNewRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/purchases/new",
  beforeLoad: ({ context }) => requirePermission(context.me, "stock.write"),
  component: () => <PurchaseFormPage />,
});
