import { createRoute } from "@tanstack/react-router";

import { authenticatedRoute } from "./authenticatedRoute";
import { requirePermission } from "./requirePermission";
import { StockMovementsPage } from "./StockMovementsPage";

export const stockMovementsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/stock/movements",
  beforeLoad: ({ context }) => requirePermission(context.me, "stock.write"),
  component: StockMovementsPage,
});
