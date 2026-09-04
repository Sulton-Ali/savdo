import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { PurchasesListPage } from "./PurchasesListPage";
import { requirePermission } from "./requirePermission";

export const purchasesRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/purchases",
  beforeLoad: ({ context }) => requirePermission(context.me, "stock.write"),
  component: PurchasesListPage,
});
